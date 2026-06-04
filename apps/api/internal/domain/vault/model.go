package vault

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type VaultStatus string

const (
	StatusActive VaultStatus = "active"
	StatusPaused VaultStatus = "paused"
	StatusClosed VaultStatus = "closed"
)

var (
	ErrVaultNotFound        = errors.New("vault not found")
	ErrUserNotFound         = errors.New("user not found")
	ErrInvalidVault         = errors.New("invalid vault input")
	ErrInvalidAmount        = errors.New("amount must be greater than zero")
	ErrInvalidAllocation    = errors.New("invalid allocation input")
	ErrInvalidPrecision     = errors.New("decimal precision exceeds supported scale")
	ErrInvalidTransition    = errors.New("invalid vault status transition")
	ErrVaultClosed          = errors.New("vault is closed")
	ErrVaultNotActive       = errors.New("vault is not active")
	ErrInsufficientBalance  = errors.New("vault balance must be zero before closing")
	ErrVaultForbidden       = errors.New("vault does not belong to caller")
	ErrAllocationNotFound   = errors.New("allocation not found")
	ErrAllocationHasBalance = errors.New("allocation has non-zero balance; set force=true to remove")
	ErrDuplicateProtocol    = errors.New("protocol already allocated")
)

const (
	MaxAmountScale = int32(8)
	MaxAPYScale    = int32(4)
)

type Vault struct {
	ID              uuid.UUID       `json:"id"`
	UserID          uuid.UUID       `json:"user_id"`
	ContractAddress string          `json:"contract_address"`
	TotalDeposited  decimal.Decimal `json:"total_deposited"`
	CurrentBalance  decimal.Decimal `json:"current_balance"`
	Currency        string          `json:"currency"`
	Status          VaultStatus     `json:"status"`
	YieldEarned     decimal.Decimal `json:"yield_earned"`
	FeesPaid        decimal.Decimal `json:"fees_paid"`
	LastSyncedAt    *time.Time      `json:"last_synced_at,omitempty"`
	DeletedAt       *time.Time      `json:"deleted_at,omitempty"`
	Allocations     []Allocation    `json:"allocations,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type ProjectionPoint struct {
	Date    time.Time       `json:"date"`
	Balance decimal.Decimal `json:"balance"`
}

type Projection struct {
	VaultID    uuid.UUID         `json:"vault_id"`
	Currency   string            `json:"currency"`
	CurrentAPY float64           `json:"current_apy"`
	Timeline   []ProjectionPoint `json:"timeline"`
}

type Allocation struct {
	ID          uuid.UUID       `json:"id"`
	VaultID     uuid.UUID       `json:"vault_id"`
	Protocol    string          `json:"protocol"`
	Amount      decimal.Decimal `json:"amount"`
	APY         decimal.Decimal `json:"apy"`
	Status      string          `json:"status"`
	AllocatedAt time.Time       `json:"allocated_at"`
	UpdatedAt   *time.Time      `json:"updated_at,omitempty"`
}

// VaultTransaction represents a single deposit or withdrawal event recorded in
// the vault_transactions table.
// HarvestRecordInput captures ledger updates after a successful harvest.
type HarvestRecordInput struct {
	VaultID              uuid.UUID
	UserID               uuid.UUID
	NetYield             decimal.Decimal
	PerformanceFee       decimal.Decimal
	Compounded           bool
	NewSharesMinted      *decimal.Decimal
	TransactionHash      string
}

type VaultTransaction struct {
	ID                   uuid.UUID        `json:"id"`
	VaultID              uuid.UUID        `json:"vault_id"`
	UserID               *uuid.UUID       `json:"user_id,omitempty"`
	Type                 string           `json:"type"` // "deposit" | "withdrawal" | "harvest"
	Amount               decimal.Decimal  `json:"amount"`
	TransactionHash      string           `json:"transaction_hash,omitempty"`
	SharesMintedOrBurned *decimal.Decimal `json:"shares_minted_or_burned,omitempty"`
	SharePriceAtTime     *decimal.Decimal `json:"share_price_at_time,omitempty"`
	FeeCharged           *decimal.Decimal `json:"fee_charged,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
}

type Repository interface {
	CreateVault(ctx context.Context, model Vault) (Vault, error)
	GetVault(ctx context.Context, id uuid.UUID) (Vault, error)
	ListUserVaults(ctx context.Context, userID uuid.UUID, filter UserListFilter) ([]Vault, int, error)
	ListVaults(ctx context.Context, filter ListFilter) ([]Vault, int, error)
	RecordDeposit(ctx context.Context, vaultID uuid.UUID, record TransactionRecord) error
	UpdateVaultBalances(ctx context.Context, id uuid.UUID, totalDeposited decimal.Decimal, currentBalance decimal.Decimal) error
	ReplaceAllocations(ctx context.Context, vaultID uuid.UUID, allocations []Allocation) error
	UpdateVault(ctx context.Context, id uuid.UUID, contractAddress string, status VaultStatus) error
	RecordWithdrawal(ctx context.Context, vaultID uuid.UUID, record TransactionRecord) error
	RecordHarvest(ctx context.Context, input HarvestRecordInput) error
	SoftDeleteVault(ctx context.Context, id uuid.UUID) error
	ListDeposits(ctx context.Context, vaultID uuid.UUID) ([]VaultTransaction, error)
	ListUserVaultTransactions(ctx context.Context, userID uuid.UUID, vaultID uuid.UUID) ([]VaultTransaction, error)
}

// CanTransitionTo reports whether moving from the receiver status to next is a
// valid state machine move.
//
//	active  → paused | closed
//	paused  → active | closed
//	closed  → (none — terminal)
func (s VaultStatus) CanTransitionTo(next VaultStatus) bool {
	switch s {
	case StatusActive:
		return next == StatusPaused || next == StatusClosed
	case StatusPaused:
		return next == StatusActive || next == StatusClosed
	default:
		return false
	}
}

func ParseStatus(value string) (VaultStatus, error) {
	switch VaultStatus(strings.ToLower(strings.TrimSpace(value))) {
	case StatusActive:
		return StatusActive, nil
	case StatusPaused:
		return StatusPaused, nil
	case StatusClosed:
		return StatusClosed, nil
	default:
		return "", ErrInvalidVault
	}
}
