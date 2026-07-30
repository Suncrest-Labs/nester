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
	ErrBelowMinDeposit      = errors.New("deposit amount is below the minimum required for this protocol")
	ErrInvalidHarvestFrequency = errors.New("harvest frequency must be 'daily' or 'weekly'")
	// ErrDuplicateTransaction is returned when a deposit/withdrawal insert
	// collides with vault_transactions' UNIQUE transaction_hash index. A
	// caller that generates its own idempotency-bearing hash (e.g. the
	// recurring-deposit job queue handler, #846) can treat this as "already
	// recorded" and safely no-op rather than fail.
	ErrDuplicateTransaction = errors.New("transaction already recorded")
	ErrCapacityExceeded     = errors.New("deposit would exceed vault capacity limit")
)

const (
	MaxAmountScale = int32(8)
	MaxAPYScale    = int32(4)
	// DefaultCapacityWarningThreshold is the percentage of capacity at which
	// warnings are surfaced (80% by default).
	DefaultCapacityWarningThreshold = 80.0
)

// HarvestFrequencyDaily and HarvestFrequencyWeekly are the supported cadences
// for the per-vault harvest engine gate (#940). Smaller vaults default to
// daily; larger vaults may prefer weekly to reduce cumulative gas spend.
const (
	HarvestFrequencyDaily   = "daily"
	HarvestFrequencyWeekly  = "weekly"
	DefaultHarvestFrequency = HarvestFrequencyDaily
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
	// SoftCapacity is an optional maximum deposit limit. When nil, no capacity
	// limit is enforced. When set, deposits that would push CurrentBalance over
	// this limit are rejected with ErrCapacityExceeded.
	SoftCapacity        *decimal.Decimal `json:"soft_capacity,omitempty"`
	// CapacityWarningPct is the percentage threshold at which capacity warnings
	// are surfaced. Defaults to DefaultCapacityWarningThreshold (80%) when nil.
	CapacityWarningPct  *float64         `json:"capacity_warning_pct,omitempty"`
	// HarvestFrequency controls how often the harvest engine will consider this
	// vault for a harvest: "daily" or "weekly". Defaults to
	// DefaultHarvestFrequency when empty.
	HarvestFrequency    string           `json:"harvest_frequency"`
	// LastHarvestedAt is when the vault's yield was last harvested, used by the
	// harvest engine to enforce HarvestFrequency. Nil means never harvested.
	LastHarvestedAt     *time.Time       `json:"last_harvested_at,omitempty"`
	LastSyncedAt        *time.Time       `json:"last_synced_at,omitempty"`
	LastAPYAlertSentAt  *time.Time       `json:"last_apy_alert_sent_at,omitempty"`
	DeletedAt           *time.Time       `json:"deleted_at,omitempty"`
	Allocations         []Allocation     `json:"allocations,omitempty"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
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

// RebalanceRecordInput captures the details of a rebalance transaction
type RebalanceRecordInput struct {
	VaultID              uuid.UUID
	UserID               uuid.UUID
	FromProtocol         string
	ToProtocol           string
	Amount               decimal.Decimal
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
	UpdateHarvestFrequency(ctx context.Context, id uuid.UUID, frequency string) error
	RecordWithdrawal(ctx context.Context, vaultID uuid.UUID, record TransactionRecord) error
	RecordHarvest(ctx context.Context, input HarvestRecordInput) error
	RecordRebalance(ctx context.Context, input RebalanceRecordInput, withdrawRecord, depositRecord TransactionRecord) error
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

// ParseHarvestFrequency validates a harvest frequency string, returning
// ErrInvalidHarvestFrequency for anything other than "daily" or "weekly"
// (case-insensitive, trimmed).
func ParseHarvestFrequency(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case HarvestFrequencyDaily:
		return HarvestFrequencyDaily, nil
	case HarvestFrequencyWeekly:
		return HarvestFrequencyWeekly, nil
	default:
		return "", ErrInvalidHarvestFrequency
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

// CapacityStatus describes the vault's capacity utilization for display and gating.
type CapacityStatus struct {
	// HasLimit is true when the vault has a soft capacity set.
	HasLimit bool `json:"has_limit"`
	// Capacity is the soft cap amount, nil if no limit.
	Capacity *decimal.Decimal `json:"capacity,omitempty"`
	// CurrentBalance is the vault's current balance.
	CurrentBalance decimal.Decimal `json:"current_balance"`
	// UtilizationPct is the percentage of capacity used, nil if no limit.
	UtilizationPct *float64 `json:"utilization_pct,omitempty"`
	// Warning is true when the vault is at or above the warning threshold.
	Warning bool `json:"warning"`
	// WarningThreshold is the percentage at which warnings are shown.
	WarningThreshold float64 `json:"warning_threshold"`
}

// GetCapacityStatus computes the vault's capacity utilization status.
func (v *Vault) GetCapacityStatus() CapacityStatus {
	warningThreshold := DefaultCapacityWarningThreshold
	if v.CapacityWarningPct != nil {
		warningThreshold = *v.CapacityWarningPct
	}

	if v.SoftCapacity == nil {
		return CapacityStatus{
			HasLimit:         false,
			CurrentBalance:   v.CurrentBalance,
			Warning:          false,
			WarningThreshold: warningThreshold,
		}
	}

	utilization := calculateUtilizationPct(v.CurrentBalance, *v.SoftCapacity)
	warning := utilization >= warningThreshold

	return CapacityStatus{
		HasLimit:         true,
		Capacity:         v.SoftCapacity,
		CurrentBalance:   v.CurrentBalance,
		UtilizationPct:   &utilization,
		Warning:          warning,
		WarningThreshold: warningThreshold,
	}
}

// CanAcceptDeposit checks whether a deposit amount would exceed the vault's
// soft capacity. Returns nil if the deposit is allowed, ErrCapacityExceeded otherwise.
func (v *Vault) CanAcceptDeposit(amount decimal.Decimal) error {
	if v.SoftCapacity == nil {
		return nil
	}

	newBalance := v.CurrentBalance.Add(amount)
	if newBalance.GreaterThan(*v.SoftCapacity) {
		return ErrCapacityExceeded
	}

	return nil
}

// calculateUtilizationPct computes the percentage of capacity used.
func calculateUtilizationPct(current, capacity decimal.Decimal) float64 {
	if capacity.IsZero() {
		return 0.0
	}
	pct := current.Div(capacity).Mul(decimal.NewFromInt(100))
	utilization, _ := pct.Float64()
	return utilization
}
