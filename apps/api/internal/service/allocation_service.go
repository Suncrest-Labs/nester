package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type CreateAllocationInput struct {
	VaultID  uuid.UUID
	Protocol string
	Weight   decimal.Decimal
	APY      decimal.Decimal
}

type UpdateAllocationInput struct {
	VaultID      uuid.UUID
	AllocationID uuid.UUID
	Protocol     *string
	Weight       *decimal.Decimal
	APY          *decimal.Decimal
}

type DeleteAllocationInput struct {
	VaultID      uuid.UUID
	AllocationID uuid.UUID
	Force        bool
}

func (s *AdminService) CreateAllocation(ctx context.Context, input CreateAllocationInput) (vault.Allocation, error) {
	if input.VaultID == uuid.Nil {
		return vault.Allocation{}, ErrInvalidAdminInput
	}

	detail, err := s.repository.GetVaultDetail(ctx, input.VaultID)
	if err != nil {
		return vault.Allocation{}, err
	}

	protocol := strings.ToLower(strings.TrimSpace(input.Protocol))
	if protocol == "" {
		return vault.Allocation{}, vault.ErrInvalidAllocation
	}
	for _, existing := range detail.Allocations {
		if existing.Protocol == protocol {
			return vault.Allocation{}, vault.ErrDuplicateProtocol
		}
	}

	if err := validateAllocationWeight(input.Weight, input.APY, s.minAllocationWeight); err != nil {
		return vault.Allocation{}, err
	}

	now := time.Now().UTC()
	allocation := vault.Allocation{
		ID:          uuid.New(),
		VaultID:     input.VaultID,
		Protocol:    protocol,
		Amount:      input.Weight,
		APY:         input.APY,
		Status:      "active",
		AllocatedAt: now,
	}

	updated := append(append([]vault.Allocation{}, detail.Allocations...), allocation)
	if err := validateAllocationWeightSum(updated); err != nil {
		return vault.Allocation{}, err
	}

	if err := s.syncAllocations(ctx, detail, updated); err != nil {
		return vault.Allocation{}, err
	}

	return allocation, nil
}

func (s *AdminService) UpdateAllocation(ctx context.Context, input UpdateAllocationInput) (vault.Allocation, error) {
	if input.VaultID == uuid.Nil || input.AllocationID == uuid.Nil {
		return vault.Allocation{}, ErrInvalidAdminInput
	}

	detail, err := s.repository.GetVaultDetail(ctx, input.VaultID)
	if err != nil {
		return vault.Allocation{}, err
	}

	updated := make([]vault.Allocation, len(detail.Allocations))
	found := false
	for i, allocation := range detail.Allocations {
		if allocation.ID != input.AllocationID {
			updated[i] = allocation
			continue
		}

		found = true
		next := allocation
		if input.Protocol != nil {
			next.Protocol = strings.ToLower(strings.TrimSpace(*input.Protocol))
			if next.Protocol == "" {
				return vault.Allocation{}, vault.ErrInvalidAllocation
			}
		}
		if input.Weight != nil {
			next.Amount = *input.Weight
		}
		if input.APY != nil {
			next.APY = *input.APY
		}
		now := time.Now().UTC()
		next.UpdatedAt = &now
		updated[i] = next
	}
	if !found {
		return vault.Allocation{}, vault.ErrAllocationNotFound
	}

	if input.Protocol != nil {
		for _, allocation := range updated {
			if allocation.ID != input.AllocationID && allocation.Protocol == *input.Protocol {
				return vault.Allocation{}, vault.ErrDuplicateProtocol
			}
		}
	}

	target := findAllocation(updated, input.AllocationID)
	if err := validateAllocationWeight(target.Amount, target.APY, s.minAllocationWeight); err != nil {
		return vault.Allocation{}, err
	}
	if err := validateAllocationWeightSum(updated); err != nil {
		return vault.Allocation{}, err
	}

	if err := s.syncAllocations(ctx, detail, updated); err != nil {
		return vault.Allocation{}, err
	}

	return target, nil
}

func (s *AdminService) DeleteAllocation(ctx context.Context, input DeleteAllocationInput) error {
	if input.VaultID == uuid.Nil || input.AllocationID == uuid.Nil {
		return ErrInvalidAdminInput
	}

	detail, err := s.repository.GetVaultDetail(ctx, input.VaultID)
	if err != nil {
		return err
	}

	var target *vault.Allocation
	remaining := make([]vault.Allocation, 0, len(detail.Allocations))
	for _, allocation := range detail.Allocations {
		if allocation.ID == input.AllocationID {
			copy := allocation
			target = &copy
			continue
		}
		remaining = append(remaining, allocation)
	}
	if target == nil {
		return vault.ErrAllocationNotFound
	}

	deployed := allocationDeployedBalance(detail.CurrentBalance, target.Amount)
	if deployed.GreaterThan(decimal.Zero) && !input.Force {
		return vault.ErrAllocationHasBalance
	}
	if err := validateAllocationWeightSum(remaining); err != nil {
		return err
	}

	return s.syncAllocations(ctx, detail, remaining)
}

func (s *AdminService) syncAllocations(
	ctx context.Context,
	detail admindomain.VaultDetail,
	allocations []vault.Allocation,
) error {
	if err := s.propagateAllocationWeights(ctx, allocations); err != nil {
		return fmt.Errorf("on-chain allocation update failed: %w", err)
	}

	if err := s.vaultRepository.ReplaceAllocations(ctx, detail.ID, allocations); err != nil {
		return err
	}

	return nil
}

func (s *AdminService) propagateAllocationWeights(ctx context.Context, allocations []vault.Allocation) error {
	if s.allocationStrategyAddress == "" {
		return nil
	}

	weights := make([]AllocationWeightEntry, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation.Amount.IsZero() {
			continue
		}
		bps := allocation.Amount.Mul(decimal.NewFromInt(100)).Round(0).IntPart()
		if bps <= 0 {
			return vault.ErrInvalidAllocation
		}
		weights = append(weights, AllocationWeightEntry{
			Protocol:  allocation.Protocol,
			WeightBps: uint32(bps), // #nosec G115 -- validated positive and <= 10000
		})
	}

	return s.chainInvoker.SetAllocationWeights(ctx, s.allocationStrategyAddress, weights)
}

func validateAllocationWeight(weight, apy, minWeight decimal.Decimal) error {
	if weight.Cmp(decimal.Zero) < 0 || apy.Cmp(decimal.Zero) < 0 {
		return vault.ErrInvalidAllocation
	}
	if weight.GreaterThan(decimal.Zero) && weight.LessThan(minWeight) {
		return vault.ErrInvalidAllocation
	}
	if decimalScale(weight) > vault.MaxAmountScale || decimalScale(apy) > vault.MaxAPYScale {
		return vault.ErrInvalidPrecision
	}
	return nil
}

func validateAllocationWeightSum(allocations []vault.Allocation) error {
	total := decimal.Zero
	for _, allocation := range allocations {
		total = total.Add(allocation.Amount)
	}
	if !total.Equal(decimal.RequireFromString("100")) {
		return vault.ErrInvalidAllocation
	}
	return nil
}

func allocationDeployedBalance(vaultBalance, weightPercent decimal.Decimal) decimal.Decimal {
	if vaultBalance.IsZero() || weightPercent.IsZero() {
		return decimal.Zero
	}
	return vaultBalance.Mul(weightPercent).Div(decimal.NewFromInt(100))
}

func findAllocation(allocations []vault.Allocation, id uuid.UUID) vault.Allocation {
	for _, allocation := range allocations {
		if allocation.ID == id {
			return allocation
		}
	}
	return vault.Allocation{}
}
