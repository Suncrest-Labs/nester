package transaction

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type TransactionType string

const (
	TypeDeposit    TransactionType = "deposit"
	TypeWithdrawal TransactionType = "withdrawal"
	TypeSettlement TransactionType = "settlement"
)

type TransactionStatus string

const (
	StatusPending   TransactionStatus = "pending"
	StatusCompleted TransactionStatus = "completed"
	StatusFailed    TransactionStatus = "failed"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrInvalidTransaction  = errors.New("invalid transaction input")
	ErrInvalidStatus       = errors.New("invalid transaction status")
	ErrInvalidType         = errors.New("invalid transaction type")

	// ErrChainClaimMismatch is returned when a transaction hash is confirmed
	// successful on-chain but its operations do not match what the client
	// claimed: wrong asset, wrong destination, or an amount other than the one
	// actually transferred. A successful transaction is not evidence that it
	// moved the claimed value to the claimed vault (nester#1145).
	ErrChainClaimMismatch = errors.New("on-chain operations do not match the claimed transaction")
)

// Typed failure reasons persisted in Transaction.ErrorReason when a
// confirmation is rejected. They are stable strings: dashboards and support
// tooling match on them, so change them only with a migration of consumers.
const (
	// ReasonNoMatchingOperation: the transaction succeeded but carries no
	// payment operation to (or from) the vault's contract address at all. This
	// is the unrelated-but-successful hash case.
	ReasonNoMatchingOperation = "chain_no_matching_operation"
	// ReasonAmountMismatch: a matching operation exists but moved a different
	// amount than the request claimed.
	ReasonAmountMismatch = "chain_amount_mismatch"
	// ReasonAssetMismatch: a matching operation exists but moved a different
	// asset than the vault's currency.
	ReasonAssetMismatch = "chain_asset_mismatch"
	// ReasonDestinationMismatch: a payment exists for the claimed asset and
	// amount, but not to the vault's contract address.
	ReasonDestinationMismatch = "chain_destination_mismatch"
	// ReasonVaultUnresolvable: the vault (and therefore the expected
	// destination) could not be loaded, so the claim cannot be checked. The
	// transaction is left pending rather than credited.
	ReasonVaultUnresolvable = "chain_vault_unresolvable"
)

type Transaction struct {
	ID          uuid.UUID         `json:"id"`
	VaultID     uuid.UUID         `json:"vault_id"`
	Type        TransactionType   `json:"type"`
	Amount      decimal.Decimal   `json:"amount"`
	Currency    string            `json:"currency"`
	TxHash      string            `json:"tx_hash"`
	Status      TransactionStatus `json:"status"`
	ErrorReason string            `json:"error_reason,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	ConfirmedAt *time.Time        `json:"confirmed_at,omitempty"`
}

// ListFilter drives paginated, filtered transaction listing for a user.
type ListFilter struct {
	UserID  uuid.UUID
	VaultID uuid.UUID // zero value means all vaults
	Type    string    // "deposit" | "withdrawal" | "" for all
	Status  string    // "pending" | "completed" | "failed" | "" for all
	Limit   int
	Offset  int
}

type Repository interface {
	Upsert(ctx context.Context, model Transaction) (Transaction, error)
	GetByHash(ctx context.Context, hash string) (Transaction, error)
	UpdateStatus(ctx context.Context, hash string, status TransactionStatus, confirmedAt *time.Time, errorReason string) (Transaction, error)
	// ListPendingOlderThan returns every transaction still in StatusPending
	// whose created_at is at or before cutoff. The background poller uses it
	// to find transactions that have had time to settle on-chain but were
	// never reconciled (e.g. the client never polled GET /transactions/{hash}).
	ListPendingOlderThan(ctx context.Context, cutoff time.Time) ([]Transaction, error)
	// ListUserTransactions returns paginated transactions scoped to the user,
	// with optional filtering by vault, type, and status.
	ListUserTransactions(ctx context.Context, filter ListFilter) ([]Transaction, int, error)
}
