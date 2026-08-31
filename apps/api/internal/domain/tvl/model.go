package tvl

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var ErrSnapshotNotFound = errors.New("tvl snapshot not found")

// Snapshot is a row in vault_tvl_snapshots.
type Snapshot struct {
	ID               uuid.UUID       `json:"id"`
	VaultID          uuid.UUID       `json:"vault_id"`
	TVLUSDC          decimal.Decimal `json:"tvl_usdc"`
	TotalDepositors  int             `json:"total_depositors"`
	SnapshotAt       time.Time       `json:"snapshot_at"`
}

// VaultTVL is the API response for a single vault.
type VaultTVL struct {
	VaultID         uuid.UUID `json:"vault_id"`
	TVLUSDC         string    `json:"tvl_usdc"`
	TVLUSD          string    `json:"tvl_usd"`
	TotalDepositors int       `json:"total_depositors"`
	LastUpdated     time.Time `json:"last_updated"`
	Change24hPct    string    `json:"24h_change_pct"`
}

// AggregateTVL sums TVL across all active vaults.
type AggregateTVL struct {
	TVLUSDC         string    `json:"tvl_usdc"`
	TVLUSD          string    `json:"tvl_usd"`
	TotalDepositors int       `json:"total_depositors"`
	VaultCount      int       `json:"vault_count"`
	LastUpdated     time.Time `json:"last_updated"`
	Change24hPct    string    `json:"24h_change_pct"`
}

// Repository persists and reads TVL snapshots.
type Repository interface {
	Insert(ctx context.Context, snapshot Snapshot) (Snapshot, error)
	LatestForVault(ctx context.Context, vaultID uuid.UUID) (Snapshot, error)
	LatestAtOrBefore(ctx context.Context, vaultID uuid.UUID, at time.Time) (Snapshot, error)
	LatestPerActiveVault(ctx context.Context) ([]Snapshot, error)
	CountDepositors(ctx context.Context, vaultID uuid.UUID) (int, error)
}

// FormatUSD formats a decimal value as a truncated (floor) 2-decimal USD string.
//
// Unlike decimal.StringFixed(2), which applies banker's rounding, this always
// truncates toward zero so that displayed balances never overstate the true value.
// e.g. FormatUSD(1234.56789) → "1234.56", not "1234.57".
func FormatUSD(d decimal.Decimal) string {
	return d.Truncate(2).StringFixed(2)
}

// FormatUSDC formats a decimal value as a full 6-decimal USDC string, truncated (not rounded).
func FormatUSDC(d decimal.Decimal) string {
	return d.Truncate(6).StringFixed(6)
}

