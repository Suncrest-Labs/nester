// Package bankaccount defines saved fiat payout accounts for recurring offramps.
package bankaccount

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound            = errors.New("bank account not found")
	ErrForbidden           = errors.New("bank account does not belong to user")
	ErrDuplicateAccount    = errors.New("bank account already saved for this user")
	ErrInvalidInput        = errors.New("invalid bank account input")
	ErrEncryptionUnavailable = errors.New("bank account encryption is not configured")
)

// BankAccount is the persisted saved account (account number stored encrypted).
type BankAccount struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	BankName      string
	BankCode      string
	AccountNumber string
	AccountName   string
	Currency      string
	Country       string
	IsDefault     bool
	VerifiedAt    *time.Time
	CreatedAt     time.Time
}

// PublicView is returned by list/get APIs (never includes full account number).
type PublicView struct {
	ID           uuid.UUID  `json:"id"`
	BankName     string     `json:"bank_name"`
	BankCode     string     `json:"bank_code,omitempty"`
	AccountLast4 string     `json:"account_last4"`
	AccountName  string     `json:"account_name"`
	Currency     string     `json:"currency"`
	Country      string     `json:"country"`
	IsDefault    bool       `json:"is_default"`
	VerifiedAt   *time.Time `json:"verified_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func ToPublicView(a BankAccount) PublicView {
	last4 := a.AccountNumber
	if len(last4) > 4 {
		last4 = last4[len(last4)-4:]
	}
	return PublicView{
		ID:           a.ID,
		BankName:     a.BankName,
		BankCode:     a.BankCode,
		AccountLast4: last4,
		AccountName:  a.AccountName,
		Currency:     a.Currency,
		Country:      a.Country,
		IsDefault:    a.IsDefault,
		VerifiedAt:   a.VerifiedAt,
		CreatedAt:    a.CreatedAt,
	}
}

// AddInput is the data required to save a new bank account.
type AddInput struct {
	BankName      string
	BankCode      string
	AccountNumber string
	AccountName   string
	Currency      string
	Country       string
	SetAsDefault  bool
	VerifiedAt    *time.Time
}

// UpdateInput supports setting default and optional label (bank name override).
type UpdateInput struct {
	SetAsDefault *bool
	BankName     *string
}

// Repository persists encrypted bank accounts.
type Repository interface {
	Create(ctx context.Context, account BankAccount, encryptedNumber []byte, fingerprint string) (BankAccount, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]BankAccount, error)
	GetByID(ctx context.Context, id uuid.UUID) (BankAccount, []byte, error)
	Delete(ctx context.Context, id uuid.UUID) error
	ClearDefaultForCurrency(ctx context.Context, userID uuid.UUID, currency string) error
	SetDefault(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
}
