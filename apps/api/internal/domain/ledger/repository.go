package ledger

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// Repository is the persistence contract for the ledger.
// The Post methods must be atomic and support an external transaction handle.
type Repository interface {
	// Account management
	CreateAccount(ctx context.Context, acc Account) (Account, error)
	GetAccount(ctx context.Context, id uuid.UUID) (Account, error)
	GetOrCreateAccount(ctx context.Context, accountType string, vaultID *uuid.UUID, userID *uuid.UUID, adapterName *string, assetCode string) (Account, error)
	// Same but inside an existing sql.Tx
	GetOrCreateAccountTx(ctx context.Context, tx *sql.Tx, accountType string, vaultID *uuid.UUID, userID *uuid.UUID, adapterName *string, assetCode string) (Account, error)

	// Posting — the core double-entry write path
	// PostEntries posts balanced entries in its own DB transaction.
	PostEntries(ctx context.Context, entries []Entry) error
	// PostEntriesTx posts balanced entries using an existing transaction handle.
	// This is required so callers (vault, harvest, rebalance) can post within their
	// own domain transaction and never diverge.
	PostEntriesTx(ctx context.Context, tx *sql.Tx, entries []Entry) error

	// Balance reads
	GetBalance(ctx context.Context, accountID uuid.UUID) (Balance, error)
	GetBalancesByVault(ctx context.Context, vaultID uuid.UUID) (map[string]int64, error)
	GetUserVaultBalance(ctx context.Context, userID, vaultID uuid.UUID) (int64, error)
	GetVaultPoolBalance(ctx context.Context, vaultID uuid.UUID) (int64, error)
	SumUserPositionBalances(ctx context.Context, vaultID uuid.UUID) (int64, error)

	// Invariant checks
	// SumAllEntries returns sum of all entry amounts — must be zero if books balanced.
	SumAllEntries(ctx context.Context) (int64, error)
	// RecomputeBalances recomputes balances from raw entries and returns mismatches vs cached ledger_balances.
	RecomputeBalances(ctx context.Context) ([]BalanceMismatch, error)

	// Reconciliation records
	CreateReconciliationRecord(ctx context.Context, rec ReconciliationRecord) error
	ListReconciliationRecords(ctx context.Context, vaultID uuid.UUID, limit int) ([]ReconciliationRecord, error)

	// For property testing
	ListAllBalances(ctx context.Context) ([]Balance, error)
}

// BalanceVerifier is a separate interface for the periodic safety-net job.
type BalanceVerifier interface {
	// Verify recomputes from raw entries and asserts equality, returning mismatches.
	Verify(ctx context.Context) ([]BalanceMismatch, error)
}

// ChainReader abstracts the on-chain read needed for reconciliation.
type ChainReader interface {
	// ReadVaultBalance returns on-chain vault balance in stroops (int64)
	ReadVaultBalance(ctx context.Context, contractAddress string) (int64, error)
	// ReadTotalSharesTimesPrice returns total_shares * share_price in stroops for comparison with sum user positions.
	// If not available, return 0 and the reconciler will skip that check.
	ReadTotalSharesTimesPrice(ctx context.Context, contractAddress string) (int64, error)
}

// ReconciliationJobConfig controls the reconciliation job.
type ReconciliationConfig struct {
	Enabled           bool
	Interval          time.Duration
	ToleranceStroops  int64 // max allowed drift before alert
}
