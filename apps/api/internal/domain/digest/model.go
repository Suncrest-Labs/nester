// Package digest supports the periodic financial insight digest (#859).
//
// The Go backend's role is narrow and deliberate: it is the source of truth
// for money movement, so it exposes the *raw ledger ingredients* a digest
// period needs (deposits this/last period, yield harvested this period,
// streak status) via DigestLedgerSource, plus a cache/audit table recording
// what was actually generated and delivered. Deterministic fact assembly
// (period comparisons, goal-progress narration, attention items) and the
// LLM narrative generation both happen in the intelligence service, which
// also pulls goals/vault/performance data from this API's existing
// endpoints — see apps/intelligence/app/services/digest_service.py.
package digest

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Period is the digest cadence: how often a user receives one.
type Period string

const (
	PeriodWeekly  Period = "weekly"
	PeriodMonthly Period = "monthly"
)

// Valid reports whether p is a recognized period.
func (p Period) Valid() bool {
	return p == PeriodWeekly || p == PeriodMonthly
}

// Bounds returns [start, end) for the most recently *completed* period as
// of `now`, plus the start of the period before that — the pair the digest
// needs for a period-over-period comparison. "This month's digest" reports
// on the month that just ended, not the one still in progress, so end is
// always <= now. Weekly periods run Monday-Sunday (ISO week); monthly
// periods run calendar-month.
func Bounds(p Period, now time.Time) (start, end, previousStart time.Time) {
	now = now.UTC()
	switch p {
	case PeriodWeekly:
		// ISO week starts Monday. time.Weekday: Sunday=0..Saturday=6.
		offset := int(now.Weekday())
		if offset == 0 {
			offset = 7 // Sunday -> 7 days since Monday
		}
		currentWeekStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(offset - 1))
		end = currentWeekStart
		start = currentWeekStart.AddDate(0, 0, -7)
		previousStart = start.AddDate(0, 0, -7)
	default: // PeriodMonthly
		currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = currentMonthStart
		start = currentMonthStart.AddDate(0, -1, 0)
		previousStart = start.AddDate(0, -1, 0)
	}
	return start, end, previousStart
}

// YieldHarvestFact is one harvest event within the period, as surfaced to
// the digest (enough to narrate "your locked savings earned X").
type YieldHarvestFact struct {
	VaultID     uuid.UUID       `json:"vault_id"`
	Amount      decimal.Decimal `json:"amount"`
	Currency    string          `json:"currency"`
	HarvestedAt time.Time       `json:"harvested_at"`
}

// StreakFact is the user's savings-streak status as of digest generation.
type StreakFact struct {
	CurrentStreak   int    `json:"current_streak"`
	LongestStreak   int    `json:"longest_streak"`
	LastDepositWeek string `json:"last_deposit_week"`
}

// LedgerSource is the deterministic, Go-computed raw ledger data for one
// user's digest period. It carries no narrative or judgment — every field
// is a number or event pulled directly from the ledger/streak repositories.
// The intelligence service combines this with goals/vault data (fetched
// separately via existing endpoints) to assemble the full fact set.
type LedgerSource struct {
	Period             Period             `json:"period"`
	PeriodStart        time.Time          `json:"period_start"`
	PeriodEnd          time.Time          `json:"period_end"`
	PreviousPeriodStart time.Time         `json:"previous_period_start"`
	Currency           string             `json:"currency"`
	DepositedThisPeriod decimal.Decimal   `json:"deposited_this_period"`
	DepositedLastPeriod decimal.Decimal   `json:"deposited_last_period"`
	YieldHarvests      []YieldHarvestFact `json:"yield_harvests"`
	Streak             StreakFact         `json:"streak"`
}

// CachedDigest is the persisted record of a generated digest: the
// authoritative "one generation per user per period" cache plus a delivery
// audit trail. FactsHash lets the caller detect whether the underlying
// period data changed since generation (e.g. a corrected transaction) and
// a regeneration is actually warranted, rather than trusting a stale cache
// forever.
type CachedDigest struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	Period            Period
	PeriodStart       time.Time
	PeriodEnd         time.Time
	FactsHash         string
	FactsJSON         string
	Narrative         string
	AttentionItemsJSON string
	HonestZeroPeriod  bool
	DeliveredAt       *time.Time
	GeneratedAt       time.Time
}

// Repository is the persistence contract for the digest cache/audit table.
type Repository interface {
	// GetCached returns the cached digest for this user/period/periodStart,
	// or (nil, nil) if none has been generated yet.
	GetCached(ctx context.Context, userID uuid.UUID, period Period, periodStart time.Time) (*CachedDigest, error)
	// Save upserts the generated digest for its (user, period, periodStart).
	Save(ctx context.Context, d CachedDigest) (CachedDigest, error)
	// MarkDelivered records that the digest was handed to the notification
	// dispatcher, so the scheduler does not re-send it on a later tick.
	MarkDelivered(ctx context.Context, id uuid.UUID, deliveredAt time.Time) error
	// GetLatest returns the most recently generated digest for a user
	// regardless of period bounds, for the user-facing "latest digest" card.
	GetLatest(ctx context.Context, userID uuid.UUID) (*CachedDigest, error)
}
