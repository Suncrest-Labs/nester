package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

type sharePriceCache struct {
	mu    sync.RWMutex
	cache map[uuid.UUID]struct {
		data      SharePriceResponse
		timestamp time.Time
	}
	ttl time.Duration
}

func newSharePriceCache() *sharePriceCache {
	return &sharePriceCache{
		cache: make(map[uuid.UUID]struct {
			data      SharePriceResponse
			timestamp time.Time
		}),
		ttl: 30 * time.Second,
	}
}

func (c *sharePriceCache) get(id uuid.UUID) (SharePriceResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[id]
	if !ok || time.Since(entry.timestamp) > c.ttl {
		return SharePriceResponse{}, false
	}
	return entry.data, true
}

func (c *sharePriceCache) set(id uuid.UUID, data SharePriceResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[id] = struct {
		data      SharePriceResponse
		timestamp time.Time
	}{data, time.Now()}
}

func (c *sharePriceCache) invalidate(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, id)
}

type SharePriceResponse struct {
	VaultID         string `json:"vault_id"`
	SharesPerUSDC   string `json:"shares_per_usdc"`
	USDCPerShare    string `json:"usdc_per_share"`
	TotalShares     string `json:"total_shares"`
	TotalAssetsUSDC string `json:"total_assets_usdc"`
	AsOfLedger      int64  `json:"as_of_ledger"`
}

type ConvertRequest struct {
	Shares string
	USDC   string
}

type ConvertResponse struct {
	Shares string `json:"shares"`
	USDC   string `json:"usdc"`
}

// VaultDepositInvoker handles on-chain deposit, withdrawal, and harvest operations.
// Implementations invoke the Soroban vault contract; the noop is used when
// no operator secret is configured.
type VaultDepositInvoker interface {
	// DepositToVault takes an ASSET amount in stroops (1 unit = 10^7).
	DepositToVault(ctx context.Context, contractAddress string, amountStroops int64) error
	// WithdrawFromVault takes a SHARE amount in stroops, NOT assets: the
	// value is forwarded to preview_withdraw_net and to the contract's
	// share-denominated withdraw. Callers holding an asset amount must
	// convert it first — see resolveWithdrawalShares (nester#1151).
	WithdrawFromVault(ctx context.Context, contractAddress string, sharesStroops int64, slippageBps int) (txHash string, err error)
	// PreviewDeposit maps ASSET stroops in to SHARE stroops out.
	PreviewDeposit(ctx context.Context, contractAddress string, amountStroops int64) (int64, error)
	// PreviewWithdraw maps SHARE stroops in to ASSET stroops out.
	PreviewWithdraw(ctx context.Context, contractAddress string, sharesStroops int64) (int64, error)
	HarvestVault(ctx context.Context, contractAddress, userAddress string, compound bool) (string, error)
	// EmergencyWithdrawAll triggers the vault contract's emergency_withdraw_all
	// function, exiting every active position in a single transaction.
	EmergencyWithdrawAll(ctx context.Context, contractAddress string) error
}

// NoopVaultDepositInvoker satisfies VaultDepositInvoker without making any
// on-chain calls. Used when chain integration is not configured.
type NoopVaultDepositInvoker struct{}

func (NoopVaultDepositInvoker) DepositToVault(_ context.Context, _ string, _ int64) error { return nil }
func (NoopVaultDepositInvoker) WithdrawFromVault(_ context.Context, _ string, _ int64, _ int) (string, error) {
	return "", nil
}
func (NoopVaultDepositInvoker) PreviewDeposit(_ context.Context, _ string, _ int64) (int64, error) {
	return 0, nil
}
func (NoopVaultDepositInvoker) PreviewWithdraw(_ context.Context, _ string, _ int64) (int64, error) {
	return 0, nil
}
func (NoopVaultDepositInvoker) HarvestVault(_ context.Context, _, _ string, _ bool) (string, error) {
	return "", nil
}
func (NoopVaultDepositInvoker) EmergencyWithdrawAll(_ context.Context, _ string) error { return nil }

// Default performance fee (10%) applied when estimating harvest proceeds off-chain.
const defaultHarvestPerformanceFeeBPS = 1000

// HarvestResult is returned by POST /api/v1/vaults/{id}/harvest.
type HarvestResult struct {
	GrossYieldUSDC     string `json:"gross_yield_usdc"`
	PerformanceFeeUSDC string `json:"performance_fee_usdc"`
	NetYieldUSDC       string `json:"net_yield_usdc"`
	Compounded         bool   `json:"compounded"`
	NewSharesMinted    string `json:"new_shares_minted,omitempty"`
	TxHash             string `json:"tx_hash,omitempty"`
}

type VaultService struct {
	repository     vault.Repository
	depositInvoker VaultDepositInvoker
	// operatorFundedDeposits gates deposits that would spend the shared
	// operator account. A nil policy refuses them all, which is the correct
	// default: the wallet-signed path does not need it (nester#1152).
	operatorFundedDeposits *OperatorFundedDepositPolicy
	chainVerifier          ChainEventVerifier
	defaultHarvestCompound bool
	yieldRecorder          YieldHarvestRecorder
	goalYieldRouter        GoalYieldRouter
	sharePriceCache        *sharePriceCache
	// metrics is optional. A nil value disables SLI recording and every
	// helper in vault_flow_metrics.go no-ops, so a service constructed
	// without it (tests, tooling) behaves exactly as before. Losing
	// observability must never change the behaviour of the money path.
	metrics *metrics.Metrics
	// moneyPathSwitches gates deposits and withdrawals on the global pause
	// switch (#1120). Optional: a nil gate allows everything, so a service
	// built without one (tests, tooling) behaves as it did before the switch
	// existed. Production wires it in SetMoneyPathSwitches.
	moneyPathSwitches MoneyPathGate
}

// MoneyPathGate reports whether a money-path operation may proceed. Declared
// here rather than taking *MoneyPathSwitchService so tests can substitute a
// gate without a database.
type MoneyPathGate interface {
	EnsureAllowed(ctx context.Context, op moneypath.Operation) error
}

// SetMoneyPathSwitches installs the global pause gate.
func (s *VaultService) SetMoneyPathSwitches(gate MoneyPathGate) {
	s.moneyPathSwitches = gate
}

// ensureMoneyPathAllowed refuses the operation when its global switch is
// engaged. A nil gate allows everything.
func (s *VaultService) ensureMoneyPathAllowed(ctx context.Context, op moneypath.Operation) error {
	if s.moneyPathSwitches == nil {
		return nil
	}
	return s.moneyPathSwitches.EnsureAllowed(ctx, op)
}

// GoalYieldRouter lets VaultService honor a savings goal's per-goal
// auto_compound preference (#task1) when harvesting yield for a vault that
// is linked to a goal.
type GoalYieldRouter interface {
	// GetAutoCompoundForVault returns the auto_compound preference of the
	// goal linked to vaultID. found is false when no goal is linked.
	GetAutoCompoundForVault(ctx context.Context, vaultID uuid.UUID) (goalID uuid.UUID, autoCompound bool, found bool, err error)
	// CreditGoalYieldBalance credits amount to the goal's yield_balance when
	// its yield was harvested without compounding.
	CreditGoalYieldBalance(ctx context.Context, goalID uuid.UUID, amount decimal.Decimal) error
}

// ── Input types ──────────────────────────────────────────────────────────────

type CreateVaultInput struct {
	UserID          uuid.UUID
	ContractAddress string
	Currency        string
	Status          string
}

type RecordDepositInput struct {
	VaultID uuid.UUID
	UserID  uuid.UUID
	Amount  decimal.Decimal
	TxHash  string
	Fee     decimal.Decimal
	// WalletAddress is the caller's Stellar address, used to confirm a
	// verified deposit event was emitted for this account (nester#1075).
	WalletAddress string
}

type UpdateAllocationsInput struct {
	VaultID     uuid.UUID
	Allocations []vault.Allocation
}

type UpdateVaultInput struct {
	VaultID         uuid.UUID
	ContractAddress string // optional — empty string means no change
	Status          string // optional — empty string means no change
}

type CloseVaultInput struct {
	VaultID uuid.UUID
	Force   bool // if true, skip balance > 0 check
}

type RecordWithdrawalInput struct {
	VaultID uuid.UUID
	UserID  uuid.UUID
	Amount  decimal.Decimal
	TxHash  string
	Fee     decimal.Decimal
	// WalletAddress is the caller's Stellar address. When set, a verified
	// chain event must have been emitted for this account; otherwise one user
	// could record another user's real withdrawal against their own vault,
	// since vaults.contract_address is not unique (nester#1076).
	WalletAddress string
	SlippageBps   int // optional; 0 uses configured default
}

type RebalancePositionInput struct {
	VaultID      uuid.UUID
	UserID       uuid.UUID
	FromProtocol string
	ToProtocol   string
	Amount       decimal.Decimal
	Currency     string
	TxHash       string
}

type RebalancePositionResult struct {
	Vault               vault.Vault
	FromProtocolBalance decimal.Decimal
	ToProtocolBalance   decimal.Decimal
}

// ── Constructor ──────────────────────────────────────────────────────────────

func NewVaultService(repository vault.Repository) *VaultService {
	return &VaultService{
		repository:      repository,
		sharePriceCache: newSharePriceCache(),
	}
}

// SetDepositInvoker wires an optional on-chain invoker into the vault service.
// Call this after NewVaultService when an operator key is available.
func (s *VaultService) SetDepositInvoker(invoker VaultDepositInvoker) {
	s.depositInvoker = invoker
}

// SetOperatorFundedDepositPolicy installs the gate for deposits funded from
// the shared operator account. Leaving it unset refuses every such deposit
// (nester#1152).
func (s *VaultService) SetOperatorFundedDepositPolicy(policy *OperatorFundedDepositPolicy) {
	s.operatorFundedDeposits = policy
}

// ChainEventVerifier looks up a transaction hash and returns the matching
// vault contract event. Implemented by stellar.RPCChainEventVerifier; tests
// inject a fake. Shared by deposit and withdrawal recording (nester#1076).
type ChainEventVerifier interface {
	VerifyVaultEvent(ctx context.Context, txHash, contractID, eventType string) (VerifiedVaultEvent, error)
}

// VerifiedVaultEvent is the amount taken from a confirmed on-chain vault event.
type VerifiedVaultEvent struct {
	TxHash     string
	EventType  string
	Amount     decimal.Decimal
	ContractID string
	// Account is the address the event was emitted for. Empty when the
	// contract does not include one in its topics.
	Account string
}

// SetChainEventVerifier wires on-chain event verification used to record
// withdrawals from a confirmed transaction hash rather than the request body.
func (s *VaultService) SetChainEventVerifier(verifier ChainEventVerifier) {
	s.chainVerifier = verifier
}

// SetMetrics wires the SLI recorder for the deposit and withdrawal service
// level indicators (nester#1056). Optional; when unset, recording no-ops.
func (s *VaultService) SetMetrics(m *metrics.Metrics) {
	s.metrics = m
}

// SetHarvestDefaultCompound configures the compound flag when the request omits it.
func (s *VaultService) SetHarvestDefaultCompound(compound bool) {
	s.defaultHarvestCompound = compound
}

// SetYieldHarvestRecorder wires an optional recorder that persists yield harvest
// history entries whenever HarvestVault succeeds.
func (s *VaultService) SetYieldHarvestRecorder(recorder YieldHarvestRecorder) {
	s.yieldRecorder = recorder
}

// SetGoalYieldRouter wires an optional router so HarvestVault can honor a
// linked savings goal's per-goal auto_compound preference (#task1).
func (s *VaultService) SetGoalYieldRouter(router GoalYieldRouter) {
	s.goalYieldRouter = router
}

// ── Existing methods ─────────────────────────────────────────────────────────

func (s *VaultService) CreateVault(ctx context.Context, input CreateVaultInput) (vault.Vault, error) {
	if input.UserID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}
	contractAddress := strings.TrimSpace(input.ContractAddress)
	if contractAddress == "" {
		return vault.Vault{}, vault.ErrInvalidVault
	}
	currency := strings.ToUpper(strings.TrimSpace(input.Currency))
	if currency == "" {
		return vault.Vault{}, vault.ErrInvalidVault
	}
	status := vault.StatusActive
	if s := strings.TrimSpace(input.Status); s != "" {
		parsedStatus, err := vault.ParseStatus(s)
		if err != nil {
			return vault.Vault{}, err
		}
		status = parsedStatus
	}
	now := time.Now()
	model := vault.Vault{
		ID:              uuid.New(),
		UserID:          input.UserID,
		ContractAddress: contractAddress,
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        currency,
		Status:          status,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	// Defensive: ensure all fields are set and normalized
	if model.ID == uuid.Nil || model.UserID == uuid.Nil || model.ContractAddress == "" || model.Currency == "" || model.Status == "" {
		return vault.Vault{}, vault.ErrInvalidVault
	}
	return s.repository.CreateVault(ctx, model)
}

func (s *VaultService) GetVault(ctx context.Context, id uuid.UUID) (vault.Vault, error) {
	if id == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}
	return s.repository.GetVault(ctx, id)
}

func (s *VaultService) ListUserVaults(
	ctx context.Context,
	userID uuid.UUID,
	filter vault.UserListFilter,
) ([]vault.Vault, int, error) {
	if userID == uuid.Nil {
		return nil, 0, vault.ErrInvalidVault
	}
	if filter.Status != "" {
		if _, err := vault.ParseStatus(filter.Status); err != nil {
			return nil, 0, err
		}
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PerPage < 1 {
		filter.PerPage = 20
	}
	return s.repository.ListUserVaults(ctx, userID, filter)
}

func (s *VaultService) RecordDeposit(ctx context.Context, input RecordDepositInput) (_ vault.Vault, err error) {
	// Deposit SLI (nester#1056). Deferred rather than called per return path
	// so that a return added later cannot silently drop the attempt from the
	// denominator and inflate the reported success rate.
	startedAt := time.Now()
	defer func() { recordFlow(s.metrics, metrics.FlowDeposit, startedAt, err) }()

	// Global pause (#1120). Checked before any validation so an engaged
	// switch stops the operation regardless of what was submitted, and
	// before the chain is touched.
	if err := s.ensureMoneyPathAllowed(ctx, moneypath.OperationDeposit); err != nil {
		return vault.Vault{}, err
	}

	if input.VaultID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}
	if input.Amount.Cmp(decimal.Zero) <= 0 {
		return vault.Vault{}, vault.ErrInvalidAmount
	}
	if decimalScale(input.Amount) > vault.MaxAmountScale {
		return vault.Vault{}, vault.ErrInvalidPrecision
	}

	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return vault.Vault{}, err
	}

	userID := input.UserID
	if userID == uuid.Nil {
		userID = existing.UserID
	}

	// A caller-supplied hash asserts the deposit already landed on-chain.
	// Refuse it when nothing can verify that assertion, rather than
	// crediting a balance on the client's word (nester#1075).
	txHash := strings.TrimSpace(input.TxHash)
	if txHash != "" && s.chainVerifier == nil {
		return vault.Vault{}, vault.ErrChainVerificationUnavailable
	}

	if txHash == "" && s.depositInvoker != nil {
		// No tx_hash means nothing was signed by the user, so this deposit
		// would be submitted with the operator as both caller and depositing
		// user — spending platform funds on the caller's behalf. Without a
		// gate that made POST /vaults/{id}/deposit a value transfer bounded
		// only by the operator's balance (nester#1152).
		//
		// The supported path is the wallet-signed one: the client signs its
		// own deposit and supplies the tx_hash, which the chain verifier
		// checks below. Anything else has to be explicitly allowlisted,
		// capped, and audit-logged.
		if err := s.operatorFundedDeposits.Authorize(ctx, input.VaultID, userID, input.Amount); err != nil {
			return vault.Vault{}, err
		}

		stroops := input.Amount.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart()
		if err := s.depositInvoker.DepositToVault(ctx, existing.ContractAddress, stroops); err != nil {
			if strings.Contains(err.Error(), "#21") {
				return vault.Vault{}, vault.ErrBelowMinDeposit
			}
			return vault.Vault{}, fmt.Errorf("on-chain deposit failed: %w", err)
		}
	}

	// Credit the amount the contract actually emitted, never the request
	// body. Without this any authenticated user can inflate their own
	// balance by POSTing an arbitrary figure (nester#1075).
	amount := input.Amount
	if s.chainVerifier != nil {
		if txHash == "" {
			return vault.Vault{}, vault.ErrTxHashRequired
		}
		event, err := s.chainVerifier.VerifyVaultEvent(ctx, txHash, existing.ContractAddress, "deposit")
		if err != nil {
			return vault.Vault{}, err
		}
		caller := strings.TrimSpace(input.WalletAddress)
		if caller != "" && event.Account != "" && !strings.EqualFold(caller, event.Account) {
			return vault.Vault{}, vault.ErrChainEventCallerMismatch
		}
		amount = event.Amount
		if amount.Cmp(decimal.Zero) <= 0 {
			return vault.Vault{}, vault.ErrInvalidAmount
		}
	}

	sharePrice := vault.ComputeSharePrice(existing)
	shares := amount.Div(sharePrice).Round(6)

	record := vault.TransactionRecord{
		UserID:               userID,
		Amount:               amount,
		TransactionHash:      txHash,
		SharesMintedOrBurned: shares,
		SharePriceAtTime:     sharePrice,
		FeeCharged:           input.Fee,
	}
	if err := s.repository.RecordDeposit(ctx, input.VaultID, record); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, input.VaultID)
}

func (s *VaultService) UpdateAllocations(ctx context.Context, input UpdateAllocationsInput) (vault.Vault, error) {
	if input.VaultID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}

	normalized := make([]vault.Allocation, 0, len(input.Allocations))
	now := time.Now().UTC()
	totalAmount := decimal.Zero

	for _, allocation := range input.Allocations {
		if strings.TrimSpace(allocation.Protocol) == "" || allocation.Amount.Cmp(decimal.Zero) < 0 || allocation.APY.Cmp(decimal.Zero) < 0 {
			return vault.Vault{}, vault.ErrInvalidAllocation
		}
		if decimalScale(allocation.Amount) > vault.MaxAmountScale || decimalScale(allocation.APY) > vault.MaxAPYScale {
			return vault.Vault{}, vault.ErrInvalidPrecision
		}

		if allocation.ID == uuid.Nil {
			allocation.ID = uuid.New()
		}
		if allocation.AllocatedAt.IsZero() {
			allocation.AllocatedAt = now
		}

		allocation.Protocol = strings.ToLower(strings.TrimSpace(allocation.Protocol))
		allocation.VaultID = input.VaultID
		normalized = append(normalized, allocation)
		totalAmount = totalAmount.Add(allocation.Amount)
	}

	// Validate that allocation weights sum to exactly 100%
	if !totalAmount.Equal(decimal.RequireFromString("100")) {
		return vault.Vault{}, vault.ErrInvalidAllocation
	}

	if err := s.repository.ReplaceAllocations(ctx, input.VaultID, normalized); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, input.VaultID)
}

// ── New methods ──────────────────────────────────────────────────────────────

// UpdateVault performs a partial update on a vault's contract address and/or
// status. Fields left blank are kept unchanged.
func (s *VaultService) UpdateVault(ctx context.Context, input UpdateVaultInput) (vault.Vault, error) {
	if input.VaultID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}

	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return vault.Vault{}, err
	}

	contractAddress := existing.ContractAddress
	if strings.TrimSpace(input.ContractAddress) != "" {
		contractAddress = strings.TrimSpace(input.ContractAddress)
	}

	newStatus := existing.Status
	if strings.TrimSpace(input.Status) != "" {
		parsed, err := vault.ParseStatus(input.Status)
		if err != nil {
			return vault.Vault{}, err
		}
		if parsed != existing.Status && !existing.Status.CanTransitionTo(parsed) {
			return vault.Vault{}, vault.ErrInvalidTransition
		}
		newStatus = parsed
	}

	if err := s.repository.UpdateVault(ctx, input.VaultID, contractAddress, newStatus); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, input.VaultID)
}

// UpdateHarvestFrequency sets how often the harvest engine considers this
// vault for a harvest ("daily" or "weekly"), letting owners trade off
// harvest latency against cumulative gas spend on a per-vault basis (#940).
func (s *VaultService) UpdateHarvestFrequency(ctx context.Context, vaultID uuid.UUID, userID uuid.UUID, frequency string) (vault.Vault, error) {
	if vaultID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}

	parsed, err := vault.ParseHarvestFrequency(frequency)
	if err != nil {
		return vault.Vault{}, err
	}

	existing, err := s.repository.GetVault(ctx, vaultID)
	if err != nil {
		return vault.Vault{}, err
	}
	if existing.UserID != userID {
		return vault.Vault{}, vault.ErrVaultForbidden
	}

	if err := s.repository.UpdateHarvestFrequency(ctx, vaultID, parsed); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, vaultID)
}

// CloseVault transitions a vault to the closed status. Unless Force is set, it
// rejects vaults that still hold a balance.
func (s *VaultService) CloseVault(ctx context.Context, input CloseVaultInput) (vault.Vault, error) {
	if input.VaultID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}

	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return vault.Vault{}, err
	}

	if existing.Status == vault.StatusClosed {
		return vault.Vault{}, vault.ErrVaultClosed
	}

	if !input.Force && existing.CurrentBalance.GreaterThan(decimal.Zero) {
		return vault.Vault{}, vault.ErrInsufficientBalance
	}

	if err := s.repository.UpdateVault(ctx, input.VaultID, existing.ContractAddress, vault.StatusClosed); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, input.VaultID)
}

// PauseVault transitions an active vault to paused.
func (s *VaultService) PauseVault(ctx context.Context, vaultID uuid.UUID) (vault.Vault, error) {
	if vaultID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}

	existing, err := s.repository.GetVault(ctx, vaultID)
	if err != nil {
		return vault.Vault{}, err
	}

	if existing.Status == vault.StatusClosed {
		return vault.Vault{}, vault.ErrVaultClosed
	}
	if existing.Status != vault.StatusActive {
		return vault.Vault{}, vault.ErrVaultNotActive
	}

	if err := s.repository.UpdateVault(ctx, vaultID, existing.ContractAddress, vault.StatusPaused); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, vaultID)
}

// UnpauseVault transitions a paused vault back to active.
func (s *VaultService) UnpauseVault(ctx context.Context, vaultID uuid.UUID) (vault.Vault, error) {
	if vaultID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}

	existing, err := s.repository.GetVault(ctx, vaultID)
	if err != nil {
		return vault.Vault{}, err
	}

	if existing.Status == vault.StatusClosed {
		return vault.Vault{}, vault.ErrVaultClosed
	}
	if existing.Status != vault.StatusPaused {
		return vault.Vault{}, vault.ErrInvalidTransition
	}

	if err := s.repository.UpdateVault(ctx, vaultID, existing.ContractAddress, vault.StatusActive); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, vaultID)
}

// RecordWithdrawal decrements current_balance and logs the transaction.
func (s *VaultService) RecordWithdrawal(ctx context.Context, input RecordWithdrawalInput) (_ vault.Vault, err error) {
	// Withdrawal SLI (nester#1056). See RecordDeposit for why this is
	// deferred rather than recorded on each return path.
	startedAt := time.Now()
	defer func() { recordFlow(s.metrics, metrics.FlowWithdrawal, startedAt, err) }()

	// Global pause (#1120), independent of the deposit switch: the common
	// incident response is to stop money entering while still letting users
	// take theirs out.
	if err := s.ensureMoneyPathAllowed(ctx, moneypath.OperationWithdrawal); err != nil {
		return vault.Vault{}, err
	}

	if input.VaultID == uuid.Nil {
		return vault.Vault{}, vault.ErrInvalidVault
	}
	if input.Amount.Cmp(decimal.Zero) <= 0 {
		return vault.Vault{}, vault.ErrInvalidAmount
	}
	if decimalScale(input.Amount) > vault.MaxAmountScale {
		return vault.Vault{}, vault.ErrInvalidPrecision
	}

	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return vault.Vault{}, err
	}

	if existing.Status == vault.StatusClosed {
		return vault.Vault{}, vault.ErrVaultClosed
	}
	// Reject before any on-chain submit: a withdrawal that would take the
	// position below zero must not be sent to the contract (nester#1076).
	//
	// This runs unconditionally. Skipping it when the caller supplies a hash
	// made the guard opt-out: any request carrying tx_hash bypassed it, and
	// when no verifier is configured nothing re-checks afterwards, so a
	// forged hash drove current_balance negative.
	//
	// This read is NOT serialised against concurrent withdrawals — it is a
	// fast-fail so we never submit a doomed on-chain call. The authoritative
	// check re-runs inside repository.RecordWithdrawal under a row lock
	// (SELECT ... FOR UPDATE), which is what makes concurrent withdrawals of
	// the same position safe (nester#1084).
	if existing.CurrentBalance.LessThan(input.Amount) {
		return vault.Vault{}, vault.ErrWithdrawalExceedsPosition
	}

	// A caller-supplied hash asserts the withdrawal already landed on-chain.
	// That assertion is only meaningful if something verifies it, so refuse
	// it outright when no verifier is wired rather than trusting the client.
	if strings.TrimSpace(input.TxHash) != "" && s.chainVerifier == nil {
		return vault.Vault{}, vault.ErrChainVerificationUnavailable
	}

	userID := input.UserID
	if userID == uuid.Nil {
		userID = existing.UserID
	}

	txHash := strings.TrimSpace(input.TxHash)
	// Shares actually burned on chain, when this call submitted the
	// transaction itself. Zero means "not known from a submission" and the
	// share figure falls back to the share-price computation below.
	submittedShares := decimal.Zero
	// A client-supplied hash means the withdraw already landed on-chain
	// (wallet-signed). Do not submit again.
	if txHash == "" && s.depositInvoker != nil {
		// WithdrawFromVault's second parameter is SHARES in stroops, not
		// assets: it is forwarded to preview_withdraw_net and to the
		// contract's share-denominated withdraw. Passing asset stroops here
		// burned the wrong number of shares whenever the share price was not
		// exactly 1.0 — taking materially more or less than the user asked
		// for, while the database recorded the requested amount either way
		// (nester#1151).
		sharesStroops, err := s.resolveWithdrawalShares(ctx, existing, input.Amount)
		if err != nil {
			return vault.Vault{}, err
		}

		submitted, err := s.depositInvoker.WithdrawFromVault(ctx, existing.ContractAddress, sharesStroops, input.SlippageBps)
		if err != nil {
			return vault.Vault{}, fmt.Errorf("on-chain withdrawal failed: %w", err)
		}
		txHash = strings.TrimSpace(submitted)
		// The contract burned sharesStroops and returned the corresponding
		// assets. Record what the chain actually did rather than what was
		// requested; when a verifier is configured the block below replaces
		// this with the emitted event, which is better still.
		submittedShares = decimal.NewFromInt(sharesStroops).Div(stroopsPerUnit)
	}

	amount := input.Amount
	if s.chainVerifier != nil {
		if txHash == "" {
			return vault.Vault{}, vault.ErrTxHashRequired
		}
		event, err := s.chainVerifier.VerifyVaultEvent(ctx, txHash, existing.ContractAddress, "withdraw")
		if err != nil {
			return vault.Vault{}, err
		}
		// Confirm the event belongs to the caller before trusting it.
		caller := strings.TrimSpace(input.WalletAddress)
		if caller != "" && event.Account != "" && !strings.EqualFold(caller, event.Account) {
			return vault.Vault{}, vault.ErrChainEventCallerMismatch
		}
		// Record the contract-event amount, never the request body.
		amount = event.Amount
		if amount.Cmp(decimal.Zero) <= 0 {
			return vault.Vault{}, vault.ErrInvalidAmount
		}
		if existing.CurrentBalance.LessThan(amount) {
			return vault.Vault{}, vault.ErrWithdrawalExceedsPosition
		}
	}

	sharePrice := vault.ComputeSharePrice(existing)
	shares := amount.Div(sharePrice).Round(6)
	// Prefer the share count the chain actually burned over one re-derived
	// from the price: the recorded withdrawal must reflect what happened on
	// chain, not what was requested (nester#1151).
	if submittedShares.IsPositive() {
		shares = submittedShares.Round(6)
	}

	record := vault.TransactionRecord{
		UserID:               userID,
		Amount:               amount,
		TransactionHash:      txHash,
		SharesMintedOrBurned: shares,
		SharePriceAtTime:     sharePrice,
		FeeCharged:           input.Fee,
	}
	if err := s.repository.RecordWithdrawal(ctx, input.VaultID, record); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, input.VaultID)
}

// stroopsPerUnit is the fixed-point scale Soroban uses for asset and share
// amounts: 1 unit = 10^7 stroops.
var stroopsPerUnit = decimal.NewFromInt(10_000_000)

// resolveWithdrawalShares converts a requested ASSET amount into the SHARE
// amount, in stroops, to burn on chain.
//
// The contract's withdraw is share-denominated, so a user asking to take out
// 100 USDC from a vault whose shares are worth 1.25 USDC each must burn 80
// shares, not 100. Passing the asset figure straight through was the bug this
// fixes (nester#1151).
//
// Two steps, because neither alone is sufficient:
//
//  1. Convert at the vault's current share price. This is a local estimate
//     derived from recorded balances.
//  2. Refine it against the contract's own preview_withdraw, which is
//     authoritative and accounts for fees the local price does not model. One
//     correction is applied by scaling the estimate by the ratio of requested
//     to previewed assets; a second preview is not worth the extra round trip,
//     and the slippage guard inside WithdrawFromVault is what ultimately
//     bounds the result.
//
// When the preview is unavailable the share-price estimate stands on its own:
// failing the withdrawal outright would make an unreachable read-only RPC
// block users from their funds, and the on-chain slippage guard still bounds
// what a mis-estimate can cost.
func (s *VaultService) resolveWithdrawalShares(
	ctx context.Context,
	existing vault.Vault,
	assetAmount decimal.Decimal,
) (int64, error) {
	sharePrice := vault.ComputeSharePrice(existing)
	if sharePrice.Sign() <= 0 {
		return 0, vault.ErrInvalidSharePrice
	}

	shares := assetAmount.Div(sharePrice)
	sharesStroops := shares.Mul(stroopsPerUnit).Round(0).IntPart()
	if sharesStroops <= 0 {
		return 0, vault.ErrInvalidAmount
	}

	previewedStroops, err := s.depositInvoker.PreviewWithdraw(ctx, existing.ContractAddress, sharesStroops)
	if err != nil || previewedStroops <= 0 {
		// Preview unavailable (or a noop invoker, which returns zero): the
		// share-price estimate is the best available answer.
		return sharesStroops, nil
	}

	previewedAssets := decimal.NewFromInt(previewedStroops)
	requestedAssets := assetAmount.Mul(stroopsPerUnit)
	corrected := decimal.NewFromInt(sharesStroops).
		Mul(requestedAssets).
		Div(previewedAssets).
		Round(0).
		IntPart()
	if corrected <= 0 {
		return sharesStroops, nil
	}
	return corrected, nil
}

// DeleteVault soft-deletes a vault so it is excluded from future reads.
func (s *VaultService) DeleteVault(ctx context.Context, vaultID uuid.UUID) error {
	if vaultID == uuid.Nil {
		return vault.ErrInvalidVault
	}

	if _, err := s.repository.GetVault(ctx, vaultID); err != nil {
		return err
	}

	return s.repository.SoftDeleteVault(ctx, vaultID)
}

type HarvestVaultInput struct {
	VaultID       uuid.UUID
	UserID        uuid.UUID
	WalletAddress string
	Compound      *bool
}

// HarvestVault claims accrued yield for the vault owner, optionally compounding it.
func (s *VaultService) HarvestVault(ctx context.Context, input HarvestVaultInput) (HarvestResult, error) {
	if input.VaultID == uuid.Nil || input.UserID == uuid.Nil {
		return HarvestResult{}, vault.ErrInvalidVault
	}

	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return HarvestResult{}, err
	}
	if existing.UserID != input.UserID {
		return HarvestResult{}, vault.ErrVaultForbidden
	}
	if existing.Status == vault.StatusClosed {
		return HarvestResult{}, vault.ErrVaultClosed
	}
	if existing.Status != vault.StatusActive {
		return HarvestResult{}, vault.ErrVaultNotActive
	}

	compound := s.defaultHarvestCompound
	var linkedGoalID uuid.UUID
	var hasLinkedGoal bool
	if s.goalYieldRouter != nil {
		goalID, autoCompound, found, err := s.goalYieldRouter.GetAutoCompoundForVault(ctx, input.VaultID)
		if err != nil {
			return HarvestResult{}, err
		}
		if found {
			linkedGoalID = goalID
			hasLinkedGoal = true
			compound = autoCompound
		}
	}
	if input.Compound != nil {
		compound = *input.Compound
	}

	grossYield := harvestableYield(existing)
	if grossYield.Cmp(decimal.Zero) <= 0 {
		return HarvestResult{
			GrossYieldUSDC:     formatUSDCAmount(decimal.Zero),
			PerformanceFeeUSDC: formatUSDCAmount(decimal.Zero),
			NetYieldUSDC:       formatUSDCAmount(decimal.Zero),
			Compounded:         compound,
		}, nil
	}

	performanceFee := grossYield.Mul(decimal.NewFromInt(defaultHarvestPerformanceFeeBPS)).
		Div(decimal.NewFromInt(10_000)).
		Round(vault.MaxAmountScale)
	netYield := grossYield.Sub(performanceFee)

	var txHash string
	if s.depositInvoker != nil {
		userAddress := strings.TrimSpace(input.WalletAddress)
		if userAddress == "" {
			return HarvestResult{}, fmt.Errorf("wallet address required for on-chain harvest")
		}
		txHash, err = s.depositInvoker.HarvestVault(ctx, existing.ContractAddress, userAddress, compound)
		if err != nil {
			return HarvestResult{}, fmt.Errorf("on-chain harvest failed: %w", err)
		}
	}

	var newShares *decimal.Decimal
	newSharesStr := ""
	if compound {
		shares := estimateSharesMinted(existing, netYield)
		newShares = &shares
		newSharesStr = formatUSDCAmount(shares)
	}

	if err := s.repository.RecordHarvest(ctx, vault.HarvestRecordInput{
		VaultID:         input.VaultID,
		UserID:          input.UserID,
		NetYield:        netYield,
		PerformanceFee:  performanceFee,
		Compounded:      compound,
		NewSharesMinted: newShares,
		TransactionHash: txHash,
	}); err != nil {
		return HarvestResult{}, err
	}

	// Yield that was not compounded back into the vault is credited to the
	// linked goal's yield_balance instead (#task1), rather than only leaving
	// the vault's ledger updated.
	if !compound && hasLinkedGoal && s.goalYieldRouter != nil {
		if err := s.goalYieldRouter.CreditGoalYieldBalance(ctx, linkedGoalID, netYield); err != nil {
			return HarvestResult{}, err
		}
	}

	if s.yieldRecorder != nil {
		_ = s.yieldRecorder.RecordHarvest(ctx, YieldHarvestRecord{
			UserID:      input.UserID,
			VaultID:     input.VaultID,
			Amount:      netYield,
			Currency:    existing.Currency,
			HarvestedAt: time.Now().UTC(),
			TxHash:      txHash,
		})
	}

	return HarvestResult{
		GrossYieldUSDC:     formatUSDCAmount(grossYield),
		PerformanceFeeUSDC: formatUSDCAmount(performanceFee),
		NetYieldUSDC:       formatUSDCAmount(netYield),
		Compounded:         compound,
		NewSharesMinted:    newSharesStr,
		TxHash:             txHash,
	}, nil
}

func harvestableYield(v vault.Vault) decimal.Decimal {
	if v.YieldEarned.Cmp(decimal.Zero) > 0 {
		return v.YieldEarned
	}
	delta := v.CurrentBalance.Sub(v.TotalDeposited)
	if delta.Cmp(decimal.Zero) > 0 {
		return delta
	}
	return decimal.Zero
}

func estimateSharesMinted(v vault.Vault, netYield decimal.Decimal) decimal.Decimal {
	if netYield.Cmp(decimal.Zero) <= 0 {
		return decimal.Zero
	}
	if v.TotalDeposited.Cmp(decimal.Zero) <= 0 {
		return netYield
	}
	sharePrice := v.CurrentBalance.Div(v.TotalDeposited)
	if sharePrice.Cmp(decimal.Zero) <= 0 {
		return netYield
	}
	return netYield.Div(sharePrice).Round(vault.MaxAmountScale)
}

func formatUSDCAmount(amount decimal.Decimal) string {
	return amount.Round(6).StringFixed(6)
}

// ListDeposits returns the deposit transaction history for a vault.
func (s *VaultService) ListDeposits(ctx context.Context, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	if vaultID == uuid.Nil {
		return nil, vault.ErrInvalidVault
	}

	if _, err := s.repository.GetVault(ctx, vaultID); err != nil {
		return nil, err
	}

	return s.repository.ListDeposits(ctx, vaultID)
}

// GetMyPosition returns the authenticated user's aggregated position in a vault.
func (s *VaultService) GetMyPosition(ctx context.Context, userID uuid.UUID, vaultID uuid.UUID) (vault.UserVaultPosition, error) {
	if userID == uuid.Nil || vaultID == uuid.Nil {
		return vault.UserVaultPosition{}, vault.ErrInvalidVault
	}

	v, err := s.repository.GetVault(ctx, vaultID)
	if err != nil {
		return vault.UserVaultPosition{}, err
	}

	txns, err := s.repository.ListUserVaultTransactions(ctx, userID, vaultID)
	if err != nil {
		return vault.UserVaultPosition{}, err
	}

	return vault.BuildUserVaultPosition(v, userID, txns), nil
}

func (s *VaultService) GetProjection(ctx context.Context, vaultID uuid.UUID) (vault.Projection, error) {
	if vaultID == uuid.Nil {
		return vault.Projection{}, vault.ErrInvalidVault
	}

	v, err := s.repository.GetVault(ctx, vaultID)
	if err != nil {
		return vault.Projection{}, err
	}

	// Calculate weighted average APY
	var totalAmount decimal.Decimal
	var weightedAPY decimal.Decimal
	for _, a := range v.Allocations {
		totalAmount = totalAmount.Add(a.Amount)
		weightedAPY = weightedAPY.Add(a.Amount.Mul(a.APY))
	}

	avgAPY := 0.0
	if !totalAmount.IsZero() {
		avgAPY, _ = weightedAPY.Div(totalAmount).Float64()
	}

	// Project for 365 days
	timeline := make([]vault.ProjectionPoint, 366)
	currentBalance := v.CurrentBalance
	dailyRate := avgAPY / 100 / 365
	now := time.Now().UTC()

	for i := 0; i <= 365; i++ {
		timeline[i] = vault.ProjectionPoint{
			Date:    now.AddDate(0, 0, i),
			Balance: currentBalance,
		}
		// Compound daily: next = current * (1 + rate)
		growth := currentBalance.Mul(decimal.NewFromFloat(dailyRate))
		currentBalance = currentBalance.Add(growth)
	}

	return vault.Projection{
		VaultID:    vaultID,
		Currency:   v.Currency,
		CurrentAPY: avgAPY,
		Timeline:   timeline,
	}, nil
}

// EmergencyWithdrawInput identifies the vault to emergency-exit.
type EmergencyWithdrawInput struct {
	VaultID uuid.UUID
}

// PositionResult is a single position touched by an emergency withdrawal.
type PositionResult struct {
	Protocol string          `json:"protocol"`
	Amount   decimal.Decimal `json:"amount"`
}

// EmergencyWithdrawResult reports the outcome of an emergency exit: which
// positions were successfully withdrawn and which failed.
type EmergencyWithdrawResult struct {
	VaultID   uuid.UUID        `json:"vault_id"`
	Succeeded []PositionResult `json:"succeeded"`
	Failed    []PositionResult `json:"failed"`
}

// EmergencyWithdraw triggers an on-chain emergency exit from all of the vault's
// active positions in a single transaction and returns the per-position
// outcome. Failures are reported alongside successes rather than aborting the
// whole call.
func (s *VaultService) EmergencyWithdraw(ctx context.Context, input EmergencyWithdrawInput) (EmergencyWithdrawResult, error) {
	if input.VaultID == uuid.Nil {
		return EmergencyWithdrawResult{}, vault.ErrInvalidVault
	}

	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return EmergencyWithdrawResult{}, err
	}

	result := EmergencyWithdrawResult{
		VaultID:   input.VaultID,
		Succeeded: []PositionResult{},
		Failed:    []PositionResult{},
	}

	if s.depositInvoker != nil {
		if err := s.depositInvoker.EmergencyWithdrawAll(ctx, existing.ContractAddress); err != nil {
			return EmergencyWithdrawResult{}, fmt.Errorf("on-chain emergency withdraw failed: %w", err)
		}
	}

	for _, allocation := range existing.Allocations {
		if allocation.Amount.Cmp(decimal.Zero) <= 0 {
			continue
		}
		result.Succeeded = append(result.Succeeded, PositionResult{
			Protocol: allocation.Protocol,
			Amount:   allocation.Amount,
		})
	}

	return result, nil
}

func (s *VaultService) RebalancePosition(ctx context.Context, input RebalancePositionInput) (RebalancePositionResult, error) {
	// Global pause (#1120). A rebalance moves funds between protocols and
	// reaches the chain, so an engaged switch has to stop it too — gating
	// only deposits and withdrawals left this path open while the money
	// path was supposed to be halted. Bound to the withdrawal switch
	// because that is the side the funds leave from.
	//
	// EmergencyWithdraw is deliberately left ungated: blocking the
	// emergency exit during an incident is backwards.
	if err := s.ensureMoneyPathAllowed(ctx, moneypath.OperationWithdrawal); err != nil {
		return RebalancePositionResult{}, err
	}

	if input.VaultID == uuid.Nil || input.UserID == uuid.Nil {
		return RebalancePositionResult{}, vault.ErrInvalidVault
	}
	if input.Amount.Cmp(decimal.Zero) <= 0 {
		return RebalancePositionResult{}, vault.ErrInvalidAmount
	}
	if input.FromProtocol == "" || input.ToProtocol == "" {
		return RebalancePositionResult{}, vault.ErrInvalidVault
	}
	if input.FromProtocol == input.ToProtocol {
		return RebalancePositionResult{}, vault.ErrInvalidVault
	}

	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return RebalancePositionResult{}, err
	}
	if existing.UserID != input.UserID {
		return RebalancePositionResult{}, vault.ErrVaultForbidden
	}
	if existing.Status != vault.StatusActive {
		return RebalancePositionResult{}, vault.ErrVaultNotActive
	}
	if existing.CurrentBalance.Cmp(input.Amount) < 0 {
		return RebalancePositionResult{}, vault.ErrInsufficientBalance
	}

	sharePrice := vault.ComputeSharePrice(existing)
	shares := input.Amount.Div(sharePrice).Round(6)

	withdrawRecord := vault.TransactionRecord{
		UserID:               input.UserID,
		Amount:               input.Amount,
		TransactionHash:      input.TxHash,
		SharesMintedOrBurned: shares,
		SharePriceAtTime:     sharePrice,
		FeeCharged:           decimal.Zero,
	}

	depositRecord := vault.TransactionRecord{
		UserID:               input.UserID,
		Amount:               input.Amount,
		TransactionHash:      input.TxHash,
		SharesMintedOrBurned: shares,
		SharePriceAtTime:     sharePrice,
		FeeCharged:           decimal.Zero,
	}

	err = s.repository.RecordRebalance(ctx, vault.RebalanceRecordInput{
		VaultID:         input.VaultID,
		UserID:          input.UserID,
		FromProtocol:    input.FromProtocol,
		ToProtocol:      input.ToProtocol,
		Amount:          input.Amount,
		TransactionHash: input.TxHash,
	}, withdrawRecord, depositRecord)
	if err != nil {
		return RebalancePositionResult{}, err
	}

	updatedVault, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return RebalancePositionResult{}, err
	}

	fromBalance := decimal.Zero
	toBalance := decimal.Zero
	for _, allocation := range updatedVault.Allocations {
		if allocation.Protocol == input.FromProtocol {
			fromBalance = allocation.Amount
		}
		if allocation.Protocol == input.ToProtocol {
			toBalance = allocation.Amount
		}
	}

	return RebalancePositionResult{
		Vault:               updatedVault,
		FromProtocolBalance: fromBalance,
		ToProtocolBalance:   toBalance,
	}, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func decimalScale(value decimal.Decimal) int32 {
	exponent := value.Exponent()
	if exponent >= 0 {
		return 0
	}
	return -exponent
}

// HarvestPreview is returned by GET /api/v1/vaults/{id}/harvest/preview.
// Same shape as HarvestResult but no TxHash — nothing is written to DB or chain.
type HarvestPreview struct {
	VaultID            string `json:"vault_id"`
	GrossYieldUSDC     string `json:"gross_yield_usdc"`
	PerformanceFeeUSDC string `json:"performance_fee_usdc"`
	NetYieldUSDC       string `json:"net_yield_usdc"`
	Compounded         bool   `json:"compounded"`
	EstimatedNewShares string `json:"estimated_new_shares,omitempty"`
	PerformanceFeeBPS  int    `json:"performance_fee_bps"`
	Impaired           bool   `json:"impaired,omitempty"`
}

type PreviewHarvestInput struct {
	VaultID  uuid.UUID
	UserID   uuid.UUID
	Compound bool
}

// PreviewHarvest computes what a harvest would yield without writing anything
// to the DB or submitting any on-chain transaction.
func (s *VaultService) PreviewHarvest(ctx context.Context, input PreviewHarvestInput) (HarvestPreview, error) {
	if input.VaultID == uuid.Nil || input.UserID == uuid.Nil {
		return HarvestPreview{}, vault.ErrInvalidVault
	}

	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return HarvestPreview{}, err
	}
	if existing.UserID != input.UserID {
		return HarvestPreview{}, vault.ErrVaultForbidden
	}

	grossYield := harvestableYield(existing)
	if grossYield.Cmp(decimal.Zero) <= 0 {
		return HarvestPreview{
			VaultID:            existing.ID.String(),
			GrossYieldUSDC:     formatUSDCAmount(decimal.Zero),
			PerformanceFeeUSDC: formatUSDCAmount(decimal.Zero),
			NetYieldUSDC:       formatUSDCAmount(decimal.Zero),
			PerformanceFeeBPS:  defaultHarvestPerformanceFeeBPS,
			Impaired:           existing.CurrentBalance.LessThan(existing.TotalDeposited),
		}, nil
	}

	performanceFee := grossYield.Mul(decimal.NewFromInt(defaultHarvestPerformanceFeeBPS)).
		Div(decimal.NewFromInt(10_000)).
		Round(vault.MaxAmountScale)
	netYield := grossYield.Sub(performanceFee)

	preview := HarvestPreview{
		VaultID:            existing.ID.String(),
		GrossYieldUSDC:     formatUSDCAmount(grossYield),
		PerformanceFeeUSDC: formatUSDCAmount(performanceFee),
		NetYieldUSDC:       formatUSDCAmount(netYield),
		Compounded:         input.Compound,
		PerformanceFeeBPS:  defaultHarvestPerformanceFeeBPS,
	}

	if input.Compound {
		shares := estimateSharesMinted(existing, netYield)
		preview.EstimatedNewShares = formatUSDCAmount(shares)
	}

	return preview, nil
}

// GetSharePrice returns the current share price information for a vault
func (s *VaultService) GetSharePrice(ctx context.Context, vaultID uuid.UUID) (SharePriceResponse, error) {
	if cached, ok := s.sharePriceCache.get(vaultID); ok {
		return cached, nil
	}

	v, err := s.repository.GetVault(ctx, vaultID)
	if err != nil {
		return SharePriceResponse{}, err
	}

	usdcPerShare := vault.ComputeSharePrice(v)
	var sharesPerUSDC decimal.Decimal
	if usdcPerShare.IsZero() {
		sharesPerUSDC = decimal.NewFromInt(1)
	} else {
		sharesPerUSDC = decimal.NewFromInt(1).Div(usdcPerShare)
	}

	totalShares := v.TotalDeposited
	if !usdcPerShare.IsZero() && !usdcPerShare.Equal(decimal.NewFromInt(1)) {
		totalShares = v.CurrentBalance.Div(usdcPerShare)
	}

	response := SharePriceResponse{
		VaultID:         v.ID.String(),
		SharesPerUSDC:   sharesPerUSDC.Round(6).StringFixed(6),
		USDCPerShare:    usdcPerShare.Round(6).StringFixed(6),
		TotalShares:     totalShares.Round(6).StringFixed(6),
		TotalAssetsUSDC: v.CurrentBalance.Round(6).StringFixed(6),
		AsOfLedger:      0,
	}

	s.sharePriceCache.set(vaultID, response)
	return response, nil
}

// Convert converts between USDC and shares for a vault
func (s *VaultService) Convert(ctx context.Context, vaultID uuid.UUID, req ConvertRequest) (ConvertResponse, error) {
	sharePrice, err := s.GetSharePrice(ctx, vaultID)
	if err != nil {
		return ConvertResponse{}, err
	}

	usdcPerShare, err := decimal.NewFromString(sharePrice.USDCPerShare)
	if err != nil {
		return ConvertResponse{}, err
	}

	var shares, usdc decimal.Decimal
	if req.Shares != "" && req.USDC != "" {
		return ConvertResponse{}, fmt.Errorf("must provide either shares or usdc, not both")
	}
	if req.Shares == "" && req.USDC == "" {
		return ConvertResponse{}, fmt.Errorf("must provide either shares or usdc")
	}

	if req.Shares != "" {
		shares, err = decimal.NewFromString(req.Shares)
		if err != nil {
			return ConvertResponse{}, err
		}
		usdc = shares.Mul(usdcPerShare)
	} else {
		usdc, err = decimal.NewFromString(req.USDC)
		if err != nil {
			return ConvertResponse{}, err
		}
		shares = usdc.Div(usdcPerShare)
	}

	return ConvertResponse{
		Shares: shares.Round(6).StringFixed(6),
		USDC:   usdc.Round(6).StringFixed(6),
	}, nil
}

// InvalidateSharePriceCache invalidates the share price cache for a vault
func (s *VaultService) InvalidateSharePriceCache(vaultID uuid.UUID) {
	s.sharePriceCache.invalidate(vaultID)
}

// ── Preview endpoints ─────────────────────────────────────────────────────────

type PreviewDepositInput struct {
	VaultID uuid.UUID
	Amount  decimal.Decimal
}

type PreviewDepositOutput struct {
	GrossAmount          decimal.Decimal `json:"gross_amount"`
	ManagementFee        decimal.Decimal `json:"management_fee"`
	NetAmount            decimal.Decimal `json:"net_amount"`
	SharesReceived       decimal.Decimal `json:"shares_received"`
	CurrentPricePerShare decimal.Decimal `json:"current_price_per_share"`
}

type PreviewWithdrawInput struct {
	VaultID uuid.UUID
	Shares  decimal.Decimal
}

type PreviewWithdrawOutput struct {
	GrossAmount          decimal.Decimal `json:"gross_amount"`
	ManagementFee        decimal.Decimal `json:"management_fee"`
	NetAmount            decimal.Decimal `json:"net_amount"`
	CurrentPricePerShare decimal.Decimal `json:"current_price_per_share"`
}

func (s *VaultService) PreviewDeposit(ctx context.Context, input PreviewDepositInput) (PreviewDepositOutput, error) {
	if input.VaultID == uuid.Nil {
		return PreviewDepositOutput{}, vault.ErrInvalidVault
	}
	if input.Amount.Cmp(decimal.Zero) <= 0 {
		return PreviewDepositOutput{}, vault.ErrInvalidAmount
	}
	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return PreviewDepositOutput{}, err
	}
	var shares decimal.Decimal
	if s.depositInvoker != nil {
		stroops := input.Amount.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart()
		sharesStroops, err := s.depositInvoker.PreviewDeposit(ctx, existing.ContractAddress, stroops)
		if err != nil {
			return PreviewDepositOutput{}, fmt.Errorf("on-chain preview deposit failed: %w", err)
		}
		shares = decimal.NewFromInt(sharesStroops).Div(decimal.NewFromInt(10_000_000))
	} else {
		shares = input.Amount
	}
	price := decimal.Zero
	if shares.GreaterThan(decimal.Zero) {
		price = input.Amount.DivRound(shares, 6)
	}
	return PreviewDepositOutput{
		GrossAmount:          input.Amount,
		ManagementFee:        decimal.Zero,
		NetAmount:            input.Amount,
		SharesReceived:       shares,
		CurrentPricePerShare: price,
	}, nil
}

func (s *VaultService) PreviewWithdraw(ctx context.Context, input PreviewWithdrawInput) (PreviewWithdrawOutput, error) {
	if input.VaultID == uuid.Nil {
		return PreviewWithdrawOutput{}, vault.ErrInvalidVault
	}
	if input.Shares.Cmp(decimal.Zero) <= 0 {
		return PreviewWithdrawOutput{}, vault.ErrInvalidAmount
	}
	existing, err := s.repository.GetVault(ctx, input.VaultID)
	if err != nil {
		return PreviewWithdrawOutput{}, err
	}
	var grossAmount decimal.Decimal
	if s.depositInvoker != nil {
		sharesStroops := input.Shares.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart()
		amountStroops, err := s.depositInvoker.PreviewWithdraw(ctx, existing.ContractAddress, sharesStroops)
		if err != nil {
			return PreviewWithdrawOutput{}, fmt.Errorf("on-chain preview withdraw failed: %w", err)
		}
		grossAmount = decimal.NewFromInt(amountStroops).Div(decimal.NewFromInt(10_000_000))
	} else {
		grossAmount = input.Shares
	}
	estimatedFee := grossAmount.Mul(decimal.NewFromFloat(0.005)).Round(6)
	netAmount := grossAmount.Sub(estimatedFee)
	price := decimal.Zero
	if input.Shares.GreaterThan(decimal.Zero) {
		price = grossAmount.DivRound(input.Shares, 6)
	}
	return PreviewWithdrawOutput{
		GrossAmount:          grossAmount,
		ManagementFee:        estimatedFee,
		NetAmount:            netAmount,
		CurrentPricePerShare: price,
	}, nil
}
