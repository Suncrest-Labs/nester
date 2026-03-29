package user

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type KYCStatus string

const (
	KYCStatusPending  KYCStatus = "pending"
	KYCStatusVerified KYCStatus = "verified"
	KYCStatusRejected KYCStatus = "rejected"
)

type User struct {
	ID            uuid.UUID `json:"id"`
	WalletAddress string    `json:"wallet_address"`
	DisplayName   string    `json:"display_name"`
	KYCStatus     KYCStatus `json:"kyc_status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

var (
	ErrUserNotFound      = errors.New("user not found")
	ErrDuplicateWallet   = errors.New("wallet address already registered")
	ErrInvalidWallet     = errors.New("invalid wallet address")
)

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByWalletAddress(ctx context.Context, addr string) (*User, error)
}
