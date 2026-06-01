package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// VaultDepositInvoker handles on-chain deposit, withdrawal, and harvest operations.
// Implementations invoke the Soroban vault contract; the noop is used when
// no operator secret is configured.
type VaultDepositInvoker interface {
	DepositToVault(ctx context.Context, contractAddress string, amountStroops int64) error
	WithdrawFromVault(ctx context.Context, contractAddress string, sharesStroops int64, slippageBps int) error
	HarvestVault(ctx context.Context, contractAddress, userAddress string, compound bool) (string, error)
}

// NoopVaultDepositInvoker satisfies VaultDepositInvoker without making any
// on-chain calls. Used when chain integration is not configured.
type NoopVaultDepositInvoker struct{}

func (NoopVaultDepositInvoker) DepositToVault(_ context.Context, _ string, _ int64) error { return nil }
func (NoopVaultDepositInvoker) WithdrawFromVault(_ context.Context, _ string, _ int64, _ int) error {
	return nil
}
func (NoopVaultDepositInvoker) HarvestVault(_ context.Context, _, _ string, _ bool) (string, error) {
	return "", nil
}

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
	repository            vault.Repository
	depositInvoker        VaultDepositInvoker
	defaultHarvestCompound bool
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
	VaultID     uuid.UUID
	UserID      uuid.UUID
	Amount      decimal.Decimal
	TxHash      string
	Fee         decimal.Decimal
	SlippageBps int // optional; 0 uses configured default
}

// ── Constructor ──────────────────────────────────────────────────────────────

func NewVaultService(repository vault.Repository) *VaultService {
	return &VaultService{repository: repository}
}

// SetDepositInvoker wires an optional on-chain invoker into the vault service.
// Call this after NewVaultService when an operator key is available.
func (s *VaultService) SetDepositInvoker(invoker VaultDepositInvoker) {
	s.depositInvoker = invoker
}

// SetHarvestDefaultCompound configures the compound flag when the request omits it.
func (s *VaultService) SetHarvestDefaultCompound(compound bool) {
	s.defaultHarvestCompound = compound
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

func (s *VaultService) RecordDeposit(ctx context.Context, input RecordDepositInput) (vault.Vault, error) {
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

	sharePrice := vault.ComputeSharePrice(existing)
	shares := input.Amount.Div(sharePrice).Round(6)

	if s.depositInvoker != nil {
		stroops := input.Amount.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart()
		if err := s.depositInvoker.DepositToVault(ctx, existing.ContractAddress, stroops); err != nil {
			return vault.Vault{}, fmt.Errorf("on-chain deposit failed: %w", err)
		}
	}

	record := vault.TransactionRecord{
		UserID:               userID,
		Amount:               input.Amount,
		TransactionHash:      input.TxHash,
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
func (s *VaultService) RecordWithdrawal(ctx context.Context, input RecordWithdrawalInput) (vault.Vault, error) {
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
	if existing.CurrentBalance.LessThan(input.Amount) {
		return vault.Vault{}, vault.ErrInsufficientBalance
	}

	userID := input.UserID
	if userID == uuid.Nil {
		userID = existing.UserID
	}

	sharePrice := vault.ComputeSharePrice(existing)
	shares := input.Amount.Div(sharePrice).Round(6)

	if s.depositInvoker != nil {
		stroops := input.Amount.Mul(decimal.NewFromInt(10_000_000)).Round(0).IntPart()
		if err := s.depositInvoker.WithdrawFromVault(ctx, existing.ContractAddress, stroops, input.SlippageBps); err != nil {
			return vault.Vault{}, fmt.Errorf("on-chain withdrawal failed: %w", err)
		}
	}

	record := vault.TransactionRecord{
		UserID:               userID,
		Amount:               input.Amount,
		TransactionHash:      input.TxHash,
		SharesMintedOrBurned: shares,
		SharePriceAtTime:     sharePrice,
		FeeCharged:           input.Fee,
	}
	if err := s.repository.RecordWithdrawal(ctx, input.VaultID, record); err != nil {
		return vault.Vault{}, err
	}

	return s.repository.GetVault(ctx, input.VaultID)
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

const (
	defaultVaultListLimit = 20
	maxVaultListLimit     = 100
)

// ListVaultsInput carries validated pagination params for the public list endpoint.
type ListVaultsInput struct {
	Limit  int
	Offset int
	Status string
}

// ListVaults returns a paginated slice of all non-deleted vaults.
func (s *VaultService) ListVaults(ctx context.Context, input ListVaultsInput) ([]vault.Vault, int, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = defaultVaultListLimit
	}
	if limit > maxVaultListLimit {
		limit = maxVaultListLimit
	}
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	return s.repository.ListVaults(ctx, vault.ListFilter{
		Limit:  limit,
		Offset: offset,
		Status: input.Status,
	})
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

// ── helpers ──────────────────────────────────────────────────────────────────

func decimalScale(value decimal.Decimal) int32 {
	exponent := value.Exponent()
	if exponent >= 0 {
		return 0
	}
	return -exponent
}
