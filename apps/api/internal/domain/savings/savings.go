package savings

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

var ErrSnapshotNotFound = errors.New("savings snapshot not found")

// Snapshot is a point-in-time reading of the savings vault contract. All
// amounts are the contract's integer (stroop) values.
type Snapshot struct {
	TakenAt       time.Time
	SharePrice    decimal.Decimal
	TotalAssets   decimal.Decimal
	TotalSupply   decimal.Decimal
	ReserveShares decimal.Decimal
}

type SavingsRepository interface {
	Insert(ctx context.Context, s Snapshot) error
	// Since returns snapshots taken at or after t, ascending by TakenAt.
	Since(ctx context.Context, t time.Time) ([]Snapshot, error)
	// Latest returns the most recent snapshot or ErrSnapshotNotFound.
	Latest(ctx context.Context) (Snapshot, error)
}
