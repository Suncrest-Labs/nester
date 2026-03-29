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

type CreateVaultInput struct {
	UserID          uuid.UUID
	ContractAddress string
	Currency        string
	Status          string
}

type RecordDepositInput struct {
	VaultID uuid.UUID
	Amount  decimal.Decimal
}

type UpdateAllocationsInput struct {
	VaultID     uuid.UUID
	Allocations []vault.Allocation
}

func NewVaultService(repository vault.Repository) *VaultService {
	return &VaultService{repository: repository}
}

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

func decimalScale(value decimal.Decimal) int32 {
	exponent := value.Exponent()
	if exponent >= 0 {
		return 0
	}
	return -exponent
}
