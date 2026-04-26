// Package performance defines the domain types for vault performance tracking
// — periodic balance/yield snapshots and rolling APY history. The snapshot
// worker writes here; the API surface in handler/performance reads from here.
package performance

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var (
	ErrSnapshotNotFound = errors.New("performance snapshot not found")
)

// Period labels the rolling APY window.
type Period string

const (
	Period7d     Period = "7d"
	Period30d    Period = "30d"
	Period90d    Period = "90d"
	PeriodAll    Period = "all"
)

// AllAPYPeriods is the canonical ordering used when rebuilding apy_history rows.
var AllAPYPeriods = []Period{Period7d, Period30d, Period90d, PeriodAll}

// Days converts a Period to its numeric window in days; PeriodAll returns 0
// (treated as "since vault creation" by the caller).
func (p Period) Days() int {
	switch p {
	case Period7d:
		return 7
	case Period30d:
		return 30
	case Period90d:
		return 90
	default:
		return 0
	}
}

// AllocationBreakdownEntry is the shape of each element in the JSONB column —
// per-source amount and APY at snapshot time.
type AllocationBreakdownEntry struct {
	Source string          `json:"source"`
	Amount decimal.Decimal `json:"amount"`
	APY    decimal.Decimal `json:"apy"`
}

// Snapshot is a row in vault_performance_snapshots.
type Snapshot struct {
	ID                  uuid.UUID                  `json:"id"`
	VaultID             uuid.UUID                  `json:"vault_id"`
	TotalBalance        decimal.Decimal            `json:"total_balance"`
	TotalDeposited      decimal.Decimal            `json:"total_deposited"`
	TotalYieldEarned    decimal.Decimal            `json:"total_yield_earned"`
	SharePrice          decimal.Decimal            `json:"share_price"`
	SnapshotAt          time.Time                  `json:"snapshot_at"`
	AllocationBreakdown []AllocationBreakdownEntry `json:"allocation_breakdown,omitempty"`
}

// APYRecord is a row in apy_history.
type APYRecord struct {
	ID           uuid.UUID       `json:"id"`
	VaultID      uuid.UUID       `json:"vault_id"`
	Period       Period          `json:"period"`
	RealizedAPY  decimal.Decimal `json:"realized_apy"`
	CalculatedAt time.Time       `json:"calculated_at"`
}

// PerformanceSummary is the view returned by GET /vaults/{id}/performance.
type PerformanceSummary struct {
	VaultID          uuid.UUID            `json:"vault_id"`
	CurrentBalance   decimal.Decimal      `json:"current_balance"`
	TotalDeposited   decimal.Decimal      `json:"total_deposited"`
	TotalYieldEarned decimal.Decimal      `json:"total_yield_earned"`
	SharePrice       decimal.Decimal      `json:"share_price"`
	APY              map[Period]float64   `json:"apy"`
	LastSnapshotAt   *time.Time           `json:"last_snapshot_at,omitempty"`
}

// SnapshotRepository abstracts persistence so handlers and the worker can
// share a single source of truth.
type SnapshotRepository interface {
	Insert(ctx context.Context, snapshot Snapshot) (Snapshot, error)
	LatestForVault(ctx context.Context, vaultID uuid.UUID) (Snapshot, error)
	HistoryForVault(ctx context.Context, vaultID uuid.UUID, since time.Time) ([]Snapshot, error)
	FirstAtOrAfter(ctx context.Context, vaultID uuid.UUID, since time.Time) (Snapshot, error)
	UpsertAPY(ctx context.Context, record APYRecord) error
	ListAPY(ctx context.Context, vaultID uuid.UUID) ([]APYRecord, error)
}
