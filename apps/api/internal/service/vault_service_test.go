package service

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)


func TestVaultServiceRecordDepositAndUpdateAllocations(t *testing.T) {
	userID := uuid.New()
	repository := newMemoryVaultRepository(userID)
	service := NewVaultService(repository)

	created, err := service.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CA123",
		Currency:        "usdc",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	updated, err := service.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("125.50"),
	})
	if err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	if !updated.TotalDeposited.Equal(decimal.RequireFromString("125.50")) {
		t.Fatalf("expected deposited amount 125.50, got %s", updated.TotalDeposited)
	}
	if !updated.CurrentBalance.Equal(decimal.RequireFromString("125.50")) {
		t.Fatalf("expected current balance 125.50, got %s", updated.CurrentBalance)
	}

	updated, err = service.UpdateAllocations(context.Background(), UpdateAllocationsInput{
		VaultID: created.ID,
		Allocations: []vault.Allocation{
			{Protocol: "AAVE", Amount: decimal.RequireFromString("40"), APY: decimal.RequireFromString("4.5")},
			{Protocol: "Blend", Amount: decimal.RequireFromString("60"), APY: decimal.RequireFromString("6.2")},
		},
	})
	if err != nil {
		t.Fatalf("UpdateAllocations() error = %v", err)
	}

	if len(updated.Allocations) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(updated.Allocations))
	}

	protocols := []string{updated.Allocations[0].Protocol, updated.Allocations[1].Protocol}
	slices.Sort(protocols)
	if !slices.Equal(protocols, []string{"aave", "blend"}) {
		t.Fatalf("expected normalized protocols, got %v", protocols)
	}
}

func TestVaultServiceCreateVaultReturnsUserNotFound(t *testing.T) {
	service := NewVaultService(newMemoryVaultRepository())

	_, err := service.CreateVault(context.Background(), CreateVaultInput{
		UserID:          uuid.New(),
		ContractAddress: "CA123",
		Currency:        "USDC",
	})
	if err != vault.ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

func TestVaultServiceRejectsExcessiveDecimalScale(t *testing.T) {
	userID := uuid.New()
	repository := newMemoryVaultRepository(userID)
	service := NewVaultService(repository)

	created, err := service.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CA123",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	_, err = service.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("1.123456789"),
	})
	if err != vault.ErrInvalidPrecision {
		t.Fatalf("expected ErrInvalidPrecision, got %v", err)
	}

	_, err = service.UpdateAllocations(context.Background(), UpdateAllocationsInput{
		VaultID: created.ID,
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("1"), APY: decimal.RequireFromString("1.12345")},
		},
	})
	if err != vault.ErrInvalidPrecision {
		t.Fatalf("expected ErrInvalidPrecision for APY scale, got %v", err)
	}
}

type memoryVaultRepository struct {
	users        map[uuid.UUID]struct{}
	vaults       map[uuid.UUID]vault.Vault
	transactions []vault.VaultTransaction
}

func newMemoryVaultRepository(userIDs ...uuid.UUID) *memoryVaultRepository {
	users := make(map[uuid.UUID]struct{}, len(userIDs))
	for _, userID := range userIDs {
		users[userID] = struct{}{}
	}

	return &memoryVaultRepository{
		users:        users,
		vaults:       make(map[uuid.UUID]vault.Vault),
		transactions: make([]vault.VaultTransaction, 0),
	}
}

func (r *memoryVaultRepository) CreateVault(_ context.Context, model vault.Vault) (vault.Vault, error) {
	if _, ok := r.users[model.UserID]; !ok {
		return vault.Vault{}, vault.ErrUserNotFound
	}

	now := time.Now().UTC()
	model.CreatedAt = now
	model.UpdatedAt = now
	model.Allocations = []vault.Allocation{}
	r.vaults[model.ID] = cloneVault(model)
	return cloneVault(model), nil
}

func (r *memoryVaultRepository) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	model, ok := r.vaults[id]
	if !ok {
		return vault.Vault{}, vault.ErrVaultNotFound
	}
	return cloneVault(model), nil
}

func (r *memoryVaultRepository) ListUserVaults(_ context.Context, userID uuid.UUID, filter vault.UserListFilter) ([]vault.Vault, int, error) {
	models := make([]vault.Vault, 0)
	for _, model := range r.vaults {
		if model.UserID != userID {
			continue
		}
		if filter.Status != "" && string(model.Status) != filter.Status {
			continue
		}
		if filter.Currency != "" && model.Currency != filter.Currency {
			continue
		}
		models = append(models, cloneVault(model))
	}
	total := len(models)
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PerPage < 1 {
		filter.PerPage = 20
	}
	start := (filter.Page - 1) * filter.PerPage
	if start >= total {
		return []vault.Vault{}, total, nil
	}
	end := start + filter.PerPage
	if end > total {
		end = total
	}
	return models[start:end], total, nil
}

func (r *memoryVaultRepository) UpdateVaultBalances(_ context.Context, id uuid.UUID, totalDeposited decimal.Decimal, currentBalance decimal.Decimal) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}

	model.TotalDeposited = totalDeposited
	model.CurrentBalance = currentBalance
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneVault(model)
	return nil
}

func (r *memoryVaultRepository) RecordDeposit(_ context.Context, id uuid.UUID, amount decimal.Decimal) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	model.TotalDeposited = model.TotalDeposited.Add(amount)
	model.CurrentBalance = model.CurrentBalance.Add(amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneVault(model)
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:        uuid.New(),
		VaultID:   id,
		Type:      "deposit",
		Amount:    amount,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (r *memoryVaultRepository) ReplaceAllocations(_ context.Context, vaultID uuid.UUID, allocations []vault.Allocation) error {
	model, ok := r.vaults[vaultID]
	if !ok {
		return vault.ErrVaultNotFound
	}

	model.Allocations = append([]vault.Allocation(nil), allocations...)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[vaultID] = cloneVault(model)
	return nil
}

func (r *memoryVaultRepository) UpdateVault(_ context.Context, id uuid.UUID, contractAddress string, status vault.VaultStatus) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.ContractAddress = contractAddress
	model.Status = status
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneVault(model)
	return nil
}

func (r *memoryVaultRepository) RecordHarvest(_ context.Context, input vault.HarvestRecordInput) error {
	model, ok := r.vaults[input.VaultID]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if input.Compounded {
		model.TotalDeposited = model.TotalDeposited.Add(input.NetYield)
		model.CurrentBalance = model.CurrentBalance.Add(input.NetYield)
	} else {
		model.CurrentBalance = model.CurrentBalance.Sub(input.NetYield)
	}
	model.FeesPaid = model.FeesPaid.Add(input.PerformanceFee)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[input.VaultID] = cloneVault(model)
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:        uuid.New(),
		VaultID:   input.VaultID,
		Type:      "harvest",
		Amount:    input.NetYield,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (r *memoryVaultRepository) RecordWithdrawal(_ context.Context, id uuid.UUID, amount decimal.Decimal) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	model.CurrentBalance = model.CurrentBalance.Sub(amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneVault(model)
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:        uuid.New(),
		VaultID:   id,
		Type:      "withdrawal",
		Amount:    amount,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (r *memoryVaultRepository) SoftDeleteVault(_ context.Context, id uuid.UUID) error {
	if _, ok := r.vaults[id]; !ok {
		return vault.ErrVaultNotFound
	}
	delete(r.vaults, id)
	return nil
}

func (r *memoryVaultRepository) ListDeposits(_ context.Context, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	result := make([]vault.VaultTransaction, 0)
	for _, txn := range r.transactions {
		if txn.VaultID == vaultID && txn.Type == "deposit" {
			result = append(result, txn)
		}
	}
	return result, nil
}

func (r *memoryVaultRepository) ListVaults(_ context.Context, filter vault.ListFilter) ([]vault.Vault, int, error) {
	out := make([]vault.Vault, 0)
	for _, v := range r.vaults {
		if filter.Status != "" && string(v.Status) != filter.Status {
			continue
		}
		out = append(out, v)
	}
	total := len(out)
	if filter.Offset < total {
		out = out[filter.Offset:]
	} else {
		out = nil
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, total, nil
}

func cloneVault(model vault.Vault) vault.Vault {
	model.Allocations = append([]vault.Allocation(nil), model.Allocations...)
	return model
}
