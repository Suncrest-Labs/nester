package vault

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
)

type VaultStatus string

const (
	StatusActive VaultStatus = "active"
	StatusPaused VaultStatus = "paused"
	StatusClosed VaultStatus = "closed"
)

// These are retrofitted onto apperror.AppError (nester#1341, following the
// taxonomy introduced in #1048) rather than plain errors.New values. Each
// stays the exact same error value it always was, so every existing
// errors.Is(err, ErrX) call site across the codebase - including
// vault_handler.go's writeDomainError dispatch - keeps compiling and
// behaving identically; a caller that wants the stable Kind/Code can now
// also errors.As(err, &appErr) instead.
//
// Kind assignments below follow writeDomainError's existing HTTP mapping
// where one already exists (e.g. ErrVaultNotFound -> NotFound,
// ErrInvalidSharePrice -> Internal because that branch is deliberately 500,
// not 400, per its own comment there). Sentinels no handler currently maps
// (ErrInvalidTransition, ErrChainVerificationUnavailable,
// ErrChainEventCallerMismatch, ErrUserCancelled) are assigned by their own
// doc comments' semantics instead.
var (
	ErrVaultNotFound     error = apperror.NewNotFound("VAULT_NOT_FOUND", "vault not found")
	ErrUserNotFound      error = apperror.NewNotFound("VAULT_USER_NOT_FOUND", "user not found")
	ErrInvalidVault      error = apperror.NewValidation("VAULT_INVALID_VAULT", "invalid vault input")
	ErrInvalidAmount     error = apperror.NewValidation("VAULT_INVALID_AMOUNT", "amount must be greater than zero")
	ErrInvalidAllocation error = apperror.NewValidation("VAULT_INVALID_ALLOCATION", "invalid allocation input")
	ErrInvalidPrecision  error = apperror.NewValidation("VAULT_INVALID_PRECISION", "decimal precision exceeds supported scale")
	// ErrInvalidTransition is returned for a status change no state machine
	// transition permits (e.g. closed -> active). Not currently surfaced
	// through a handler's writeDomainError switch, but shaped like every
	// other malformed-request case: the caller asked for something the
	// current state can never allow, so it is Validation, not Conflict.
	ErrInvalidTransition   error = apperror.NewValidation("VAULT_INVALID_TRANSITION", "invalid vault status transition")
	ErrVaultClosed         error = apperror.NewValidation("VAULT_CLOSED", "vault is closed")
	ErrVaultNotActive      error = apperror.NewValidation("VAULT_NOT_ACTIVE", "vault is not active")
	ErrInsufficientBalance error = apperror.NewValidation("VAULT_INSUFFICIENT_BALANCE", "vault balance must be zero before closing")
	// ErrWithdrawalExceedsPosition is returned when a withdraw would take the
	// caller's vault position below zero. Checked before any on-chain submit
	// (nester#1076).
	ErrWithdrawalExceedsPosition error = apperror.NewValidation("VAULT_WITHDRAWAL_EXCEEDS_POSITION", "withdrawal would take the position below zero")
	// ErrTxHashRequired is returned when a withdrawal is recorded without a
	// verified on-chain transaction hash (nester#1076).
	ErrTxHashRequired error = apperror.NewValidation("VAULT_TX_HASH_REQUIRED", "transaction hash is required")
	// ErrUnverifiedChainTx is returned when the supplied hash cannot be
	// reconciled against a matching vault contract event.
	ErrUnverifiedChainTx error = apperror.NewValidation("VAULT_UNVERIFIED_CHAIN_TX", "on-chain transaction could not be verified")
	// ErrChainVerificationUnavailable is returned when a caller supplies a
	// transaction hash but no chain verifier is configured. Accepting the
	// hash unverified would let a forged one move a balance, so the request
	// is refused rather than trusted (nester#1075, nester#1076). Kind is
	// Forbidden rather than UpstreamUnavailable: this is a deliberate policy
	// refusal (no verifier wired), not a transient failure a client should
	// retry after a delay.
	ErrChainVerificationUnavailable error = apperror.NewForbidden("VAULT_CHAIN_VERIFICATION_UNAVAILABLE", "on-chain verification is not configured; transaction hash cannot be accepted")
	// ErrChainEventCallerMismatch is returned when a verified contract event
	// was emitted for a different account than the caller's wallet. Without
	// this check one user can record another user's real withdrawal against
	// their own vault (nester#1076).
	ErrChainEventCallerMismatch error = apperror.NewForbidden("VAULT_CHAIN_EVENT_CALLER_MISMATCH", "on-chain event belongs to a different account")
	// ErrVaultForbidden maps to NotFound's HTTP status (404, not 403) at the
	// handler via writeDomainError's own remap - a non-owner must not be
	// able to tell an existing vault from one that was never there. The Kind
	// here stays Forbidden rather than NotFound because that is what the
	// error actually IS (an owner mismatch); the anti-enumeration remap to a
	// 404-shaped response is the handler's presentation choice, not a fact
	// about the error itself.
	ErrVaultForbidden          error = apperror.NewForbidden("VAULT_FORBIDDEN", "vault does not belong to caller")
	ErrAllocationNotFound      error = apperror.NewNotFound("VAULT_ALLOCATION_NOT_FOUND", "allocation not found")
	ErrAllocationHasBalance    error = apperror.NewConflict("VAULT_ALLOCATION_HAS_BALANCE", "allocation has non-zero balance; set force=true to remove")
	ErrDuplicateProtocol       error = apperror.NewConflict("VAULT_DUPLICATE_PROTOCOL", "protocol already allocated")
	ErrBelowMinDeposit         error = apperror.NewValidation("VAULT_BELOW_MIN_DEPOSIT", "deposit amount is below the minimum required for this protocol")
	ErrInvalidHarvestFrequency error = apperror.NewValidation("VAULT_INVALID_HARVEST_FREQUENCY", "harvest frequency must be 'daily' or 'weekly'")
	// ErrDuplicateTransaction is returned when a deposit/withdrawal insert
	// collides with vault_transactions' UNIQUE transaction_hash index. A
	// caller that generates its own idempotency-bearing hash (e.g. the
	// recurring-deposit job queue handler, #846) can treat this as "already
	// recorded" and safely no-op rather than fail.
	ErrDuplicateTransaction error = apperror.NewConflict("VAULT_DUPLICATE_TRANSACTION", "transaction already recorded")
	// ErrContractAddressRegistered is returned when a vault is created for a
	// contract address another live vault already claims. The database
	// enforces this with the uq_vaults_contract_address_live partial unique
	// index (migration 104); without a distinct error the collision would
	// surface as an opaque 500.
	//
	// It matters beyond tidiness: the event indexer keys balance mutations on
	// contract_address, so a second vault pointing at someone else's contract
	// would have that victim's on-chain deposits credited to it as well
	// (nester#1148).
	ErrContractAddressRegistered error = apperror.NewConflict("VAULT_CONTRACT_ADDRESS_REGISTERED", "a vault is already registered for this contract address")
	// ErrInvalidSharePrice is returned when a vault's share price is zero or
	// negative, which makes an asset/share conversion meaningless. It signals
	// corrupted balances rather than bad user input, so Kind is Internal
	// (writeDomainError deliberately maps this to 500, not 400, for the same
	// reason: the caller cannot fix this by changing the request).
	ErrInvalidSharePrice error = apperror.NewInternal("VAULT_INVALID_SHARE_PRICE", "vault share price is not positive")
	// ErrOperatorFundedDepositRefused is returned when a deposit would have
	// to be funded from the shared operator account and policy forbids it.
	// The caller's remedy is to sign and submit the deposit from their own
	// wallet and supply the resulting tx_hash (nester#1152).
	ErrOperatorFundedDepositRefused error = apperror.NewForbidden("VAULT_OPERATOR_FUNDED_DEPOSIT_REFUSED", "deposits must be signed and funded by your own wallet: submit the transaction and supply its tx_hash")
	ErrCapacityExceeded             error = apperror.NewValidation("VAULT_CAPACITY_EXCEEDED", "deposit would exceed vault capacity limit")
	// ErrUserCancelled is returned when a user declines the wallet signature
	// or abandons an attempt before submission. It exists to keep that case
	// distinguishable from a system fault: the deposit and withdrawal SLIs
	// (nester#1056) exclude cancellations from the denominator, and without a
	// dedicated sentinel a cancellation would be classified as an internal
	// failure and burn the error budget for something the system did right.
	// Kind is Validation: the client's current request cannot proceed as
	// submitted (the signature was never provided), the same shape as every
	// other "try again differently" case, not a server fault.
	ErrUserCancelled error = apperror.NewValidation("VAULT_USER_CANCELLED", "attempt cancelled by user")
	// ErrDepositNotAllowlisted is returned when the mainnet deposit allowlist
	// gate is active and the user's ID has not been granted access (nester#1389).
	// Kind is Forbidden: the service is available, the user is simply not in
	// the current cohort.
	ErrDepositNotAllowlisted error = apperror.NewForbidden("VAULT_DEPOSIT_NOT_ALLOWLISTED", "your account is not yet enabled for deposits; join the waitlist")
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
	SoftCapacity *decimal.Decimal `json:"soft_capacity,omitempty"`
	// CapacityWarningPct is the percentage threshold at which capacity warnings
	// are surfaced. Defaults to DefaultCapacityWarningThreshold (80%) when nil.
	CapacityWarningPct *float64 `json:"capacity_warning_pct,omitempty"`
	// HarvestFrequency controls how often the harvest engine will consider this
	// vault for a harvest: "daily" or "weekly". Defaults to
	// DefaultHarvestFrequency when empty.
	HarvestFrequency string `json:"harvest_frequency"`
	// LastHarvestedAt is when the vault's yield was last harvested, used by the
	// harvest engine to enforce HarvestFrequency. Nil means never harvested.
	LastHarvestedAt    *time.Time   `json:"last_harvested_at,omitempty"`
	LastSyncedAt       *time.Time   `json:"last_synced_at,omitempty"`
	LastAPYAlertSentAt *time.Time   `json:"last_apy_alert_sent_at,omitempty"`
	DeletedAt          *time.Time   `json:"deleted_at,omitempty"`
	Allocations        []Allocation `json:"allocations,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
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
	VaultID         uuid.UUID
	UserID          uuid.UUID
	NetYield        decimal.Decimal
	PerformanceFee  decimal.Decimal
	Compounded      bool
	NewSharesMinted *decimal.Decimal
	TransactionHash string
}

// RebalanceRecordInput captures the details of a rebalance transaction
type RebalanceRecordInput struct {
	VaultID         uuid.UUID
	UserID          uuid.UUID
	FromProtocol    string
	ToProtocol      string
	Amount          decimal.Decimal
	TransactionHash string
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
