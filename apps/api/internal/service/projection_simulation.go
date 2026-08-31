package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/projection"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
)

// This file wires the Monte Carlo savings forecasting engine
// (internal/domain/projection/simulation.go, issue #843) into
// ProjectionService: resolving real historical APY volatility and a user's
// real contribution cadence, deriving a stable seed, caching results, and
// returning a projection.SimulationOutput. See
// internal/domain/projection/README.md for the full write-up of every
// distributional assumption; the constants below are the short version
// colocated with the code that uses them.
const (
	// simulationHistoryWindow bounds how far back we look for historical
	// APY samples when estimating a vault's yield volatility. 90 days is
	// long enough to gather a meaningful sample of daily buckets while
	// staying responsive to a vault's current allocation/regime (a
	// protocol that changed strategy months ago shouldn't still dominate
	// the estimate).
	simulationHistoryWindow = 90 * 24 * time.Hour

	// minHistoricalAPYSamples is the fewest daily-bucketed APY samples
	// we'll trust to compute a standard deviation from. Below this we fall
	// back to defaultAPYStdDevFraction rather than report a std dev
	// computed from (say) two data points.
	minHistoricalAPYSamples = 5

	// defaultAPYStdDevFraction is the yield-volatility prior, expressed as
	// a fraction of the mean APY, used when a vault has no/insufficient
	// historical APY data (new vault, no snapshots yet) or when the caller
	// supplies an explicit APY with no vault at all. 25% is a deliberately
	// wide prior so an unproven vault's band reads as appropriately
	// uncertain rather than falsely tight.
	defaultAPYStdDevFraction = 0.25

	// newUserContributionSkipProbability is the modeled probability that a
	// user with no active savings schedule misses a given month's
	// contribution. There is no first-party historical distribution to
	// draw this from for a brand-new user, so it is a documented prior:
	// 12% sits in the middle of the 10-15% range called out in the issue,
	// based on typical first-90-day skip/lapse rates observed for
	// comparable recurring-consumer-savings products. It is explicit here
	// (and in ContributionSource="default_prior" on the output) rather
	// than silently hardcoded, per the issue's documentation requirement.
	newUserContributionSkipProbability = 0.12
	// newUserContributionVariationPct is the +/- size variation applied to
	// a new user's un-scheduled contribution when it does happen; wider
	// than the scheduled case because there's no commitment device at all.
	newUserContributionVariationPct = 0.20

	// scheduledContributionSkipProbability/VariationPct apply when the
	// user has an active savingsschedule.SavingsSchedule for the goal.
	// Opting into an automated recurring deposit is a real, observable
	// commitment signal, so both the skip probability and the contribution
	// size variation are modeled tighter than the new-user prior — but not
	// zero, since real schedules still miss runs (insufficient balance,
	// cancelled funding source, manual pause).
	scheduledContributionSkipProbability = 0.05
	scheduledContributionVariationPct    = 0.10
)

// SimulationCacheTTL is the caching window (issue #843's determinism +
// caching requirement): a request with identical stable inputs (same
// derived seed, see projection.DeriveSeed) is served from cache rather than
// recomputed for this long. Long enough to absorb bursty repeat calls (e.g.
// a savings-calculator UI re-fetching as a user adjusts an unrelated field)
// while short enough that an updated vault APY snapshot or savings schedule
// change is reflected within a few minutes. This is a small in-process TTL
// cache (see simulationCache below) rather than Redis: RunMonteCarloSimulation
// is cheap (low single-digit milliseconds at DefaultPathCount), so a
// distributed cache would add operational cost without a latency win; the
// seed-keyed design means swapping in a Redis-backed cache later (the
// project already depends on github.com/redis/go-redis/v9) would not change
// this method's public behavior.
const SimulationCacheTTL = 5 * time.Minute

type simCacheEntry struct {
	output  projection.SimulationOutput
	expires time.Time
}

// simulationCache is a small, process-local, TTL-based cache keyed on the
// Monte Carlo seed. Safe for concurrent use.
type simulationCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[int64]simCacheEntry
}

func newSimulationCache(ttl time.Duration) *simulationCache {
	return &simulationCache{ttl: ttl, items: make(map[int64]simCacheEntry)}
}

func (c *simulationCache) get(seed int64) (projection.SimulationOutput, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[seed]
	if !ok || time.Now().After(e.expires) {
		return projection.SimulationOutput{}, false
	}
	return e.output, true
}

func (c *simulationCache) set(seed int64, out projection.SimulationOutput) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[seed] = simCacheEntry{output: out, expires: time.Now().Add(c.ttl)}
}

// SimulateVaultProjection runs a Monte Carlo savings forecast (issue #843):
// many randomized paths over the horizon varying yield (grounded in a
// vault's real historical APY volatility when VaultID is supplied) and
// contribution behavior (grounded in the user's own active savings schedule
// when GoalID is supplied and has one), reporting a P10/P50/P90 band and,
// when a target amount + deadline are known, the probability of hitting the
// goal in time plus a small deposit/deadline sensitivity grid.
//
// userID scopes goal/schedule resolution: a GoalID that does not belong to
// userID is rejected (savingsgoal.ErrUnauthorized) rather than silently
// leaking another user's target amount or deadline.
func (s *ProjectionService) SimulateVaultProjection(ctx context.Context, userID uuid.UUID, input projection.SimulationInput) (*projection.SimulationOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	currency := "USD"
	var vaultID *uuid.UUID
	expectedAPY := decimal.Zero
	apyStdDev := decimal.Zero
	volatilitySource := "default_prior"
	haveMeanFromHistory := false

	if input.VaultID != nil {
		vaultEntity, err := s.vaultRepo.GetVault(ctx, *input.VaultID)
		if err != nil {
			return nil, fmt.Errorf("failed to get vault: %w", err)
		}
		vaultID = input.VaultID
		currency = vaultEntity.Currency

		since := time.Now().Add(-simulationHistoryWindow)
		if points, histErr := s.performanceRepo.APYHistoryForVault(ctx, *vaultID, since, "daily"); histErr == nil {
			samples := make([]float64, 0, len(points))
			for _, pt := range points {
				// APYHistoryForVault reports APY as a percentage string
				// (e.g. "10.45"); the simulation engine works in decimal
				// fraction terms (0.1045), matching ProjectionInput.APY
				// elsewhere in this service.
				if f, perr := strconv.ParseFloat(pt.APY, 64); perr == nil {
					samples = append(samples, f/100.0)
				}
			}
			if len(samples) >= minHistoricalAPYSamples {
				mean, std := projection.MeanStdDev(samples)
				expectedAPY = decimal.NewFromFloat(mean)
				apyStdDev = decimal.NewFromFloat(std)
				volatilitySource = "historical"
				haveMeanFromHistory = true
			}
		}
		if !haveMeanFromHistory {
			if _, _, avg, statsErr := s.performanceRepo.APYStatsForVault(ctx, *vaultID, since); statsErr == nil && avg.GreaterThan(decimal.Zero) {
				expectedAPY = avg.Div(decimal.NewFromInt(100))
				haveMeanFromHistory = true
			}
		}
	}

	if input.APY != nil {
		// An explicit APY override always wins for the mean, matching
		// CalculateVaultProjection's APYOverride precedence.
		expectedAPY = *input.APY
	} else if !haveMeanFromHistory {
		// No vault, no override, no history: same 5% fallback
		// CalculateVaultProjection uses when historical APY is unavailable.
		expectedAPY = decimal.NewFromFloat(0.05)
	}
	if expectedAPY.LessThanOrEqual(decimal.Zero) {
		return nil, projection.ErrInvalidAPY
	}
	if volatilitySource != "historical" {
		apyStdDev = expectedAPY.Mul(decimal.NewFromFloat(defaultAPYStdDevFraction))
	}

	monthlyContribution := input.MonthlyContribution
	skipProb := newUserContributionSkipProbability
	variationPct := newUserContributionVariationPct
	contributionSource := "default_prior"

	targetAmount := input.TargetAmount
	deadlineMonths := input.DeadlineMonths

	if input.GoalID != nil {
		goal, err := s.savingsGoalRepo.GetByID(ctx, *input.GoalID)
		if err != nil {
			return nil, fmt.Errorf("failed to get savings goal: %w", err)
		}
		if goal.UserID != userID {
			return nil, savingsgoal.ErrUnauthorized
		}
		if targetAmount == nil {
			target := goal.TargetAmount
			targetAmount = &target
		}
		if deadlineMonths == nil {
			if months := monthsUntil(time.Now(), goal.Deadline); months > 0 {
				deadlineMonths = &months
			}
		}
		if vaultID == nil {
			currency = goal.Currency
		}

		if schedule, schedErr := s.savingsScheduleRepo.GetActiveByGoal(ctx, *input.GoalID, userID); schedErr == nil && schedule != nil {
			monthlyContribution = monthlyEquivalent(schedule.Amount, schedule.Frequency)
			skipProb = scheduledContributionSkipProbability
			variationPct = scheduledContributionVariationPct
			contributionSource = "schedule"
		}
	}

	periodMonths := input.PeriodMonths
	if periodMonths <= 0 && deadlineMonths != nil {
		periodMonths = *deadlineMonths
	}
	if periodMonths <= 0 {
		return nil, projection.ErrInvalidPeriod
	}

	seed := projection.DeriveSeed(
		uuidOrEmpty(vaultID),
		uuidOrEmpty(input.GoalID),
		input.InitialDeposit.String(),
		monthlyContribution.String(),
		expectedAPY.String(),
		apyStdDev.String(),
		fmt.Sprintf("%d", periodMonths),
		fmt.Sprintf("%d", input.CompoundFrequency),
		fmt.Sprintf("%.6f", skipProb),
		fmt.Sprintf("%.6f", variationPct),
		decimalOrEmpty(targetAmount),
		intOrEmpty(deadlineMonths),
	)

	if cached, ok := s.simCache.get(seed); ok {
		return &cached, nil
	}

	params := projection.MonteCarloParams{
		InitialDeposit:              input.InitialDeposit,
		MonthlyContribution:         monthlyContribution,
		ExpectedAPY:                 expectedAPY,
		APYStdDev:                   apyStdDev,
		PeriodMonths:                periodMonths,
		CompoundFrequency:           input.CompoundFrequency,
		ContributionSkipProbability: skipProb,
		ContributionVariationPct:    variationPct,
		PathCount:                   input.PathCount,
		Seed:                        seed,
	}
	if targetAmount != nil && deadlineMonths != nil {
		params.TargetAmount = targetAmount
		params.TargetMonth = deadlineMonths
	}

	result := projection.RunMonteCarloSimulation(params)

	var grid []projection.SensitivityGridPoint
	if params.TargetAmount != nil && params.TargetMonth != nil {
		// A small +/-20%/+/-40% deposit grid crossed with a +/-6-month
		// deadline grid — enough to give a caller a "what if I saved more,
		// or gave myself more time" picture without an unbounded/slow grid.
		step := monthlyContribution.Mul(decimal.NewFromFloat(0.2))
		contributionDeltas := []decimal.Decimal{
			step.Mul(decimal.NewFromInt(-2)),
			step.Neg(),
			decimal.Zero,
			step,
			step.Mul(decimal.NewFromInt(2)),
		}
		deadlineDeltas := []int{-6, 0, 6}
		grid = projection.SensitivityGrid(params, contributionDeltas, deadlineDeltas)
	}

	output := projection.SimulationOutput{
		VaultID:                     vaultID,
		Currency:                    currency,
		Input:                       input,
		ExpectedAPY:                 expectedAPY.InexactFloat64(),
		APYStdDev:                   apyStdDev.InexactFloat64(),
		ContributionSkipProbability: skipProb,
		VolatilitySource:            volatilitySource,
		ContributionSource:          contributionSource,
		PathCount:                   result.PathCount,
		Seed:                        seed,
		Timeline:                    result.Timeline,
		FinalBand:                   result.FinalBand,
		GoalSuccess:                 result.GoalSuccess,
		SensitivityGrid:             grid,
		CalculatedAt:                time.Now(),
	}

	s.simCache.set(seed, output)
	return &output, nil
}

// monthsUntil returns the number of whole months (rounded up, floor 1) from
// `from` to a future `to`, or 0 if `to` is not after `from`.
func monthsUntil(from, to time.Time) int {
	if !to.After(from) {
		return 0
	}
	days := to.Sub(from).Hours() / 24
	months := int(math.Ceil(days / 30.44))
	if months < 1 {
		months = 1
	}
	return months
}

// monthlyEquivalent converts a savings schedule's per-occurrence amount into
// a monthly-equivalent contribution for the simulation engine (which steps
// month by month), using average occurrences per month for each frequency.
func monthlyEquivalent(amount decimal.Decimal, freq savingsschedule.Frequency) decimal.Decimal {
	switch freq {
	case savingsschedule.FrequencyWeekly:
		return amount.Mul(decimal.NewFromFloat(52.0 / 12.0))
	case savingsschedule.FrequencyBiweekly:
		return amount.Mul(decimal.NewFromFloat(26.0 / 12.0))
	default: // monthly
		return amount
	}
}

func uuidOrEmpty(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func decimalOrEmpty(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
}

func intOrEmpty(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}
