package service

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type VaultService struct {
	repository vault.Repository
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
	Amount  decimal.Decimal
	TxHash  string
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
	Amount  decimal.Decimal
	TxHash  string
}

// ── Constructor ──────────────────────────────────────────────────────────────

func NewVaultService(repository vault.Repository) *VaultService {
	return &VaultService{repository: repository}
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

func (s *VaultService) GetUserVaults(ctx context.Context, userID uuid.UUID) ([]vault.Vault, error) {
	if userID == uuid.Nil {
		return nil, vault.ErrInvalidVault
	}
	vaults, err := s.repository.GetUserVaults(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Sort vaults by APY descending
	for i := 0; i < len(vaults); i++ {
		for j := i + 1; j < len(vaults); j++ {
			// Get max APY for each vault
			maxAPYI := decimal.Zero
			for _, alloc := range vaults[i].Allocations {
				if alloc.APY.GreaterThan(maxAPYI) {
					maxAPYI = alloc.APY
				}
			}
			maxAPYJ := decimal.Zero
			for _, alloc := range vaults[j].Allocations {
				if alloc.APY.GreaterThan(maxAPYJ) {
					maxAPYJ = alloc.APY
				}
			}
			// Swap if j has higher APY
			if maxAPYJ.GreaterThan(maxAPYI) {
				vaults[i], vaults[j] = vaults[j], vaults[i]
			}
		}
	}

	return vaults, nil
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

	if err := s.repository.RecordDeposit(ctx, input.VaultID, input.Amount); err != nil {
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

	if err := s.repository.RecordWithdrawal(ctx, input.VaultID, input.Amount); err != nil {
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

// ── helpers ──────────────────────────────────────────────────────────────────

func decimalScale(value decimal.Decimal) int32 {
	exponent := value.Exponent()
	if exponent >= 0 {
		return 0
	}
	return -exponent
}
