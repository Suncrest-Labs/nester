package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/digest"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/yieldharvest"
)

// DigestPrimaryCurrency is the currency period deposit totals are computed
// in. Nester's savings goals are USDC- or XLM-denominated; USDC is the
// default for new goals and the currency most balances settle in, so it is
// the digest's reporting currency for MVP. Extending to a per-user or
// multi-currency digest is a documented follow-up, not required by #859.
const DigestPrimaryCurrency = "USDC"

// DigestLedgerService assembles the deterministic, Go-computed raw ledger
// ingredients for one user's digest period (#859). It does no narration and
// makes no judgment calls — every value is a direct sum or listing from the
// ledger/streak repositories. The intelligence service combines this with
// goals/vault data (already available via existing endpoints) to assemble
// the full fact set and generate the narrative.
type DigestLedgerService struct {
	goals   savingsgoal.Repository
	harvest yieldharvest.Repository
	streaks savingsstreak.Repository
	clock   func() time.Time
}

func NewDigestLedgerService(
	goals savingsgoal.Repository,
	harvest yieldharvest.Repository,
	streaks savingsstreak.Repository,
) *DigestLedgerService {
	return &DigestLedgerService{
		goals:   goals,
		harvest: harvest,
		streaks: streaks,
		clock:   func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the time source (tests only).
func (s *DigestLedgerService) SetClock(clock func() time.Time) { s.clock = clock }

// Assemble computes the raw ledger source for the most recently completed
// period as of now.
func (s *DigestLedgerService) Assemble(ctx context.Context, userID uuid.UUID, period digest.Period) (digest.LedgerSource, error) {
	if !period.Valid() {
		return digest.LedgerSource{}, fmt.Errorf("digest: invalid period %q", period)
	}

	start, end, previousStart := digest.Bounds(period, s.clock())

	// SumRecentDeposits(since) returns everything deposited from `since` to
	// now, so the "this period" figure is a direct call, and "last period"
	// is derived by subtracting the (overlapping) since-`start` sum from the
	// since-`previousStart` sum — the two ranges are disjoint and adjacent,
	// so the subtraction recovers exactly [previousStart, start).
	thisPeriod, err := s.goals.SumRecentDeposits(ctx, userID, DigestPrimaryCurrency, start)
	if err != nil {
		return digest.LedgerSource{}, fmt.Errorf("digest: sum deposits this period: %w", err)
	}
	sinceLastStart, err := s.goals.SumRecentDeposits(ctx, userID, DigestPrimaryCurrency, previousStart)
	if err != nil {
		return digest.LedgerSource{}, fmt.Errorf("digest: sum deposits last period: %w", err)
	}
	lastPeriod := sinceLastStart.Sub(thisPeriod)

	harvests, err := s.harvest.ListForUser(ctx, yieldharvest.ListFilter{
		UserID: userID,
		Since:  &start,
		Until:  &end,
		Limit:  100,
	})
	if err != nil {
		return digest.LedgerSource{}, fmt.Errorf("digest: list yield harvests: %w", err)
	}
	harvestFacts := make([]digest.YieldHarvestFact, 0, len(harvests))
	for _, h := range harvests {
		harvestFacts = append(harvestFacts, digest.YieldHarvestFact{
			VaultID:     h.VaultID,
			Amount:      h.Amount,
			Currency:    h.Currency,
			HarvestedAt: h.HarvestedAt,
		})
	}

	var streakFact digest.StreakFact
	if s.streaks != nil {
		streak, err := s.streaks.Get(ctx, userID)
		if err != nil {
			return digest.LedgerSource{}, fmt.Errorf("digest: get streak: %w", err)
		}
		if streak != nil {
			streakFact = digest.StreakFact{
				CurrentStreak:   streak.CurrentStreak,
				LongestStreak:   streak.LongestStreak,
				LastDepositWeek: streak.LastDepositWeek,
			}
		}
	}

	return digest.LedgerSource{
		Period:               period,
		PeriodStart:          start,
		PeriodEnd:            end,
		PreviousPeriodStart:  previousStart,
		Currency:             DigestPrimaryCurrency,
		DepositedThisPeriod:  thisPeriod,
		DepositedLastPeriod:  lastPeriod,
		YieldHarvests:        harvestFacts,
		Streak:               streakFact,
	}, nil
}
