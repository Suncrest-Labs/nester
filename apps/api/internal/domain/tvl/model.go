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
