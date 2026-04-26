package offramp

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// SettlementStatus represents the lifecycle state of a settlement.
type SettlementStatus string

const (
	StatusInitiated        SettlementStatus = "initiated"
	StatusLiquidityMatched SettlementStatus = "liquidity_matched"
	StatusFiatDispatched   SettlementStatus = "fiat_dispatched"
	StatusConfirmed        SettlementStatus = "confirmed"
	StatusFailed           SettlementStatus = "failed"
)

// validTransitions defines the allowed forward transitions in the settlement
// state machine. Terminal states (confirmed, failed) have no outbound edges.
var validTransitions = map[SettlementStatus][]SettlementStatus{
	StatusInitiated:        {StatusLiquidityMatched, StatusFailed},
	StatusLiquidityMatched: {StatusFiatDispatched, StatusFailed},
	StatusFiatDispatched:   {StatusConfirmed, StatusFailed},
	StatusConfirmed:        {},
	StatusFailed:           {},
}

var (
	ErrSettlementNotFound    = errors.New("settlement not found")
	ErrUserNotFound          = errors.New("user not found")
	ErrVaultNotFound         = errors.New("vault not found")
	ErrInvalidSettlement     = errors.New("invalid settlement input")
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrInvalidStatus         = errors.New("invalid settlement status")
	ErrInvalidTransition     = errors.New("invalid status transition")
	ErrInvalidPrecision      = errors.New("decimal precision exceeds supported scale")
	ErrForbidden             = errors.New("caller does not own this settlement")
)

const MaxAmountScale = int32(8)

// Destination holds the fiat delivery target — either a bank account or a
// mobile-money wallet.
type Destination struct {
	Type          string `json:"type"`           // "bank_transfer" | "mobile_money"
	Provider      string `json:"provider"`       // "mpesa" | "mtn_momo" | "bank"
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	BankCode      string `json:"bank_code,omitempty"` // required for bank transfers
}

// Settlement is the central domain entity for an offramp operation.
type Settlement struct {
	ID           uuid.UUID        `json:"id"`
	UserID       uuid.UUID        `json:"user_id"`
	VaultID      uuid.UUID        `json:"vault_id"`
	Amount       decimal.Decimal  `json:"amount"`
	Currency     string           `json:"currency"`
	FiatCurrency string           `json:"fiat_currency"`
	FiatAmount   decimal.Decimal  `json:"fiat_amount"`
	ExchangeRate decimal.Decimal  `json:"exchange_rate"`
	Destination  Destination      `json:"destination"`
	Status       SettlementStatus `json:"status"`
	RetryCount   int              `json:"retry_count"`
	ErrorMessage string           `json:"error_message,omitempty"`
	Notes        string           `json:"notes,omitempty"`
	EstimatedFee *decimal.Decimal `json:"estimated_fee,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty"`
}

// CanTransitionTo reports whether transitioning from the current status to
// next is a valid move in the state machine.
func (s *Settlement) CanTransitionTo(next SettlementStatus) bool {
	allowed, ok := validTransitions[s.Status]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == next {
			return true
		}
	}
	return false
}

// ParseStatus parses and validates a raw status string.
func ParseStatus(raw string) (SettlementStatus, error) {
	s := SettlementStatus(raw)
	if _, ok := validTransitions[s]; ok {
		return s, nil
	}
	return "", ErrInvalidStatus
}

// Repository is the persistence contract for settlements.
type Repository interface {
	Create(ctx context.Context, model Settlement) (Settlement, error)
	GetByID(ctx context.Context, id uuid.UUID) (Settlement, error)
	GetByUserID(ctx context.Context, userID uuid.UUID, statusFilter SettlementStatus) ([]Settlement, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status SettlementStatus, completedAt *time.Time) error
}
