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
	TypeRebalance  TransactionType = "rebalance"
	TypeYieldEarned TransactionType = "yield_earned"
)

type TransactionStatus string

const (
	StatusPending   TransactionStatus = "pending"
	StatusCompleted TransactionStatus = "completed"
	StatusFailed    TransactionStatus = "failed"
)

var (
	ErrTransactionNotFound = errors.New("transaction not found")
	ErrInvalidTransaction   = errors.New("invalid transaction input")
	ErrInvalidStatus        = errors.New("invalid transaction status")
	ErrInvalidType          = errors.New("invalid transaction type")
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

// ListFilter holds optional query parameters for listing transactions.
type ListFilter struct {
	UserID  string
	Types   []TransactionType
	Status  TransactionStatus
	From    *time.Time
	To      *time.Time
	Cursor  string // opaque base64-encoded "createdAt:id"
	Limit   int    // default 25, max 100
	VaultID string
	Search  string
}

// Page is a generic cursor-based paginated response.
type Page[T any] struct {
	Items          []T             `json:"items"`
	NextCursor     string          `json:"next_cursor,omitempty"`
	PrevCursor     string          `json:"prev_cursor,omitempty"`
	Total          int             `json:"total"`
	TotalDeposited decimal.Decimal `json:"total_deposited"`
	TotalWithdrawn decimal.Decimal `json:"total_withdrawn"`
	TotalYield     decimal.Decimal `json:"total_yield_earned"`
}

type Repository interface {
	Upsert(ctx context.Context, model Transaction) (Transaction, error)
	GetByHash(ctx context.Context, hash string) (Transaction, error)
	UpdateStatus(ctx context.Context, hash string, status TransactionStatus, confirmedAt *time.Time, errorReason string) (Transaction, error)
	ListByUserID(ctx context.Context, filter ListFilter) (Page[Transaction], error)
}
