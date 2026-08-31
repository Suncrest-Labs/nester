package ledger

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Account types as defined in the issue.
const (
	AccountTypeUserVaultPosition = "user_vault_position"
	AccountTypeVaultAssetPool    = "vault_asset_pool"
	AccountTypeFee               = "fee_account"
	AccountTypePenaltyEscrow     = "penalty_escrow"
	AccountTypeTreasury          = "treasury"
	AccountTypeYieldSource       = "yield_source"
	AccountTypeSystemSuspense    = "system_suspense"
	// External is an optional helper for outside world if needed.
	AccountTypeExternal = "external"
)

var (
	ErrInvalidAccountType = errors.New("invalid ledger account type")
	ErrUnbalanced         = errors.New("ledger entries do not sum to zero")
	ErrTooFewEntries      = errors.New("at least two entries required")
	ErrEmptyTransactionID = errors.New("transaction_id is required")
	ErrZeroAmount         = errors.New("amount must be non-zero")
)

// ValidAccountTypes is the set of allowed account_type values.
var ValidAccountTypes = map[string]bool{
	AccountTypeUserVaultPosition: true,
	AccountTypeVaultAssetPool:    true,
	AccountTypeFee:               true,
	AccountTypePenaltyEscrow:     true,
	AccountTypeTreasury:          true,
	AccountTypeYieldSource:       true,
	AccountTypeSystemSuspense:    true,
	AccountTypeExternal:          true,
}

// Account identifies a ledger account.
// - user_vault_position: a user's position in a vault (vault_id + user_id)
// - vault_asset_pool: the vault's asset pool (vault_id)
// - fee_account: global or per-asset fee account
// - penalty_escrow: escrow for early-withdraw penalties (vault_id)
// - treasury: protocol treasury
// - yield_source: per-adapter yield source (adapter_name)
// - system_suspense: global suspense for balancing
type Account struct {
	ID          uuid.UUID  `json:"id"`
	AccountType string     `json:"account_type"`
	VaultID     *uuid.UUID `json:"vault_id,omitempty"`
	UserID      *uuid.UUID `json:"user_id,omitempty"`
	AdapterName *string    `json:"adapter_name,omitempty"`
	AssetCode   string     `json:"asset_code"` // e.g. USDC
	AssetUnit   string     `json:"asset_unit"` // e.g. stroops — smallest integer unit, never floats
	Description string     `json:"description,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Entry records a single posting. Amount is signed, in the smallest integer unit (stroops).
// Every logical transaction (same transaction_id) must have at least two entries summing to zero.
type Entry struct {
	ID              uuid.UUID `json:"id"`
	TransactionID   uuid.UUID `json:"transaction_id"`
	AccountID       uuid.UUID `json:"account_id"`
	Amount          int64     `json:"amount"` // signed, stroops — never float
	Direction       string    `json:"direction"` // debit or credit, derived from amount sign
	CreatedAt       time.Time `json:"created_at"`
	DomainEventType string    `json:"domain_event_type,omitempty"` // deposit, withdraw, harvest, rebalance
	DomainEventID   string    `json:"domain_event_id,omitempty"`   // reference to domain record
	AssetCode       string    `json:"asset_code"`
	AssetUnit       string    `json:"asset_unit"` // stroops
}

// Balance is the cached materialised total for an account.
type Balance struct {
	AccountID uuid.UUID `json:"account_id"`
	Balance   int64     `json:"balance"` // sum of entries, stroops
	AssetCode string    `json:"asset_code"`
	AssetUnit string    `json:"asset_unit"`
	UpdatedAt time.Time `json:"updated_at"`
	Version   int64     `json:"version"`
}

// ValidateAccountType checks if the account type is known.
func ValidateAccountType(t string) error {
	if !ValidAccountTypes[t] {
		return ErrInvalidAccountType
	}
	return nil
}

// ValidateBalanced enforces the core double-entry invariant: every logical transaction
// must have >=2 entries and sum to exactly zero in integer stroops.
func ValidateBalanced(entries []Entry) error {
	if len(entries) < 2 {
		return ErrTooFewEntries
	}
	// All entries must share the same transaction_id.
	txID := entries[0].TransactionID
	if txID == uuid.Nil {
		return ErrEmptyTransactionID
	}
	var sum int64
	for _, e := range entries {
		if e.TransactionID != txID {
			return errors.New("all entries must share the same transaction_id")
		}
		if e.Amount == 0 {
			return ErrZeroAmount
		}
		if e.AccountID == uuid.Nil {
			return errors.New("account_id is required")
		}
		sum += e.Amount
	}
	if sum != 0 {
		return ErrUnbalanced
	}
	return nil
}

// DirectionFromAmount returns debit for positive amounts, credit for negative.
// This is a convention: positive = increase in our chosen sign model.
// For user_vault_position and vault_asset_pool we treat positive as increase.
func DirectionFromAmount(amount int64) string {
	if amount >= 0 {
		return "debit"
	}
	return "credit"
}

// Stroops conversion helpers.
// 1 USDC = 10^7 stroops (Stellar standard 7 decimals)
const StroopsPerUSDC = 10_000_000

// DecimalStringToStroops converts a decimal string like "125.50" USDC to stroops int64.
// It is the presentation-edge conversion — ledger never stores floats.
func DecimalStringToStroops(s string) (int64, error) {
	// Avoid float; parse manually or via decimal library.
	// We use a simple integer parsing to keep domain pure; caller should use shopspring/decimal
	// and pass int64. This helper is for tests.
	return 0, errors.New("use shopspring/decimal conversion in service layer")
}

// ReconciliationRecord stores a drift check result.
type ReconciliationRecord struct {
	ID                      uuid.UUID `json:"id"`
	VaultID                 uuid.UUID `json:"vault_id"`
	LedgerVaultPoolBalance  int64     `json:"ledger_vault_pool_balance"`
	OnChainBalance          int64     `json:"on_chain_balance"`
	Difference              int64     `json:"difference"`
	Tolerance               int64     `json:"tolerance"`
	Status                  string    `json:"status"` // ok, drift, error
	Details                 string    `json:"details,omitempty"`
	CreatedAt               time.Time `json:"created_at"`
}

// BalanceMismatch is returned by the recompute-and-assert job.
type BalanceMismatch struct {
	AccountID uuid.UUID `json:"account_id"`
	Cached    int64     `json:"cached"`
	Computed  int64     `json:"computed"`
	Difference int64    `json:"difference"`
}
