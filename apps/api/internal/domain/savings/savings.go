package savings

import (
	"context"
	"errors"
	"math"
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

// AdapterState is one registered yield adapter. Value, AbsCap are raw contract
// integers; Name is empty when the adapter address is not in the name map.
type AdapterState struct {
	Address   string
	Name      string
	Value     decimal.Decimal
	AbsCap    decimal.Decimal
	RelCapBps uint32
}

// PendingChange is a queued timelocked governance change. Kind is the
// contract's Change variant name (AddAdapter, RaiseCap, SetTimelock, SetFee,
// BurnReserve).
type PendingChange struct {
	ID   uint32
	Kind string
	ETA  time.Time
}

// ChainState is the live reading of the savings core's views. Amounts and
// SharePrice are the contract's raw integers (share price scaled by 1e7).
type ChainState struct {
	SharePrice    decimal.Decimal
	TotalAssets   decimal.Decimal
	TotalSupply   decimal.Decimal
	Idle          decimal.Decimal
	ReserveShares decimal.Decimal
	FeeBps        uint32
	Timelock      time.Duration
	Adapters      []AdapterState
	Pending       []PendingChange
}

const secondsPerYear = 365 * 24 * time.Hour

// TrailingRate returns the annualized realized rate, in percent (matching the
// apysnapshot APY representation, e.g. 5.35 for 5.35%), computed as
// (p_end/p_start)^(365d/elapsed) - 1 from the oldest snapshot within window
// of now and the newest snapshot. snaps must be ascending by TakenAt. It
// returns nil with fewer than two snapshots in the window, a non-positive
// start price, or less than 24h between them. The exponentiation uses
// float64, so the result is accurate to roughly 12 significant digits.
func TrailingRate(snaps []Snapshot, window time.Duration, now time.Time) *decimal.Decimal {
	cutoff := now.Add(-window)
	var in []Snapshot
	for _, s := range snaps {
		if !s.TakenAt.Before(cutoff) {
			in = append(in, s)
		}
	}
	if len(in) < 2 {
		return nil
	}
	start, end := in[0], in[len(in)-1]
	elapsed := end.TakenAt.Sub(start.TakenAt)
	if elapsed < 24*time.Hour || !start.SharePrice.IsPositive() {
		return nil
	}
	ratio, _ := end.SharePrice.Div(start.SharePrice).Float64()
	rate := math.Pow(ratio, float64(secondsPerYear)/float64(elapsed)) - 1
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return nil
	}
	out := decimal.NewFromFloat(rate * 100).Round(4)
	return &out
}
