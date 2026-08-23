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

func TestVaultServiceUpdateHarvestFrequency(t *testing.T) {
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
	if created.HarvestFrequency != vault.HarvestFrequencyDaily {
		t.Fatalf("new vault harvest frequency = %q, want default %q", created.HarvestFrequency, vault.HarvestFrequencyDaily)
	}

	updated, err := service.UpdateHarvestFrequency(context.Background(), created.ID, userID, "weekly")
	if err != nil {
		t.Fatalf("UpdateHarvestFrequency() error = %v", err)
	}
	if updated.HarvestFrequency != vault.HarvestFrequencyWeekly {
		t.Fatalf("harvest frequency = %q, want %q", updated.HarvestFrequency, vault.HarvestFrequencyWeekly)
	}

	if _, err := service.UpdateHarvestFrequency(context.Background(), created.ID, userID, "monthly"); err != vault.ErrInvalidHarvestFrequency {
		t.Fatalf("invalid frequency err = %v, want ErrInvalidHarvestFrequency", err)
	}

	if _, err := service.UpdateHarvestFrequency(context.Background(), created.ID, uuid.New(), "weekly"); err != vault.ErrVaultForbidden {
		t.Fatalf("non-owner err = %v, want ErrVaultForbidden", err)
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

func TestVaultServiceGetMyPositionWithYield(t *testing.T) {
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

	if _, err := service.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID,
		UserID:  userID,
		Amount:  decimal.RequireFromString("1000"),
	}); err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	if err := repository.UpdateVaultBalances(context.Background(), created.ID,
		decimal.RequireFromString("1000"),
		decimal.RequireFromString("1050"),
	); err != nil {
		t.Fatalf("UpdateVaultBalances() error = %v", err)
	}

	position, err := service.GetMyPosition(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("GetMyPosition() error = %v", err)
	}

	if position.UnrealizedPnLUSDC != "+50.000000" {
		t.Fatalf("expected pnl +50, got %s", position.UnrealizedPnLUSDC)
	}
}

func TestVaultServiceGetMyPositionEmpty(t *testing.T) {
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

	position, err := service.GetMyPosition(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("GetMyPosition() error = %v", err)
	}

	if position.SharesHeld != "0.000000" {
		t.Fatalf("expected zero shares, got %s", position.SharesHeld)
	}
}

func TestVaultServiceEmergencyWithdrawReportsActivePositions(t *testing.T) {
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

	if _, err := service.UpdateAllocations(context.Background(), UpdateAllocationsInput{
		VaultID: created.ID,
		Allocations: []vault.Allocation{
			{Protocol: "AAVE", Amount: decimal.RequireFromString("40"), APY: decimal.RequireFromString("4.5")},
			{Protocol: "Blend", Amount: decimal.RequireFromString("60"), APY: decimal.RequireFromString("6.2")},
		},
	}); err != nil {
		t.Fatalf("UpdateAllocations() error = %v", err)
	}

	result, err := service.EmergencyWithdraw(context.Background(), EmergencyWithdrawInput{VaultID: created.ID})
	if err != nil {
		t.Fatalf("EmergencyWithdraw() error = %v", err)
	}

	if result.VaultID != created.ID {
		t.Fatalf("expected vault id %s, got %s", created.ID, result.VaultID)
	}
	if len(result.Succeeded) != 2 {
		t.Fatalf("expected 2 succeeded positions, got %d", len(result.Succeeded))
	}
	if result.Failed == nil {
		t.Fatalf("expected non-nil failed slice")
	}
}

func TestVaultServiceEmergencyWithdrawRejectsNilVault(t *testing.T) {
	service := NewVaultService(newMemoryVaultRepository())

	_, err := service.EmergencyWithdraw(context.Background(), EmergencyWithdrawInput{VaultID: uuid.Nil})
	if err != vault.ErrInvalidVault {
		t.Fatalf("expected ErrInvalidVault, got %v", err)
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

	if model.HarvestFrequency == "" {
		model.HarvestFrequency = vault.DefaultHarvestFrequency
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

func (r *memoryVaultRepository) RecordDeposit(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if record.Amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	model.TotalDeposited = model.TotalDeposited.Add(record.Amount)
	model.CurrentBalance = model.CurrentBalance.Add(record.Amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneVault(model)

	userID := record.UserID
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:                   uuid.New(),
		VaultID:              id,
		UserID:               &userID,
		Type:                 "deposit",
		Amount:               record.Amount,
		TransactionHash:      record.TransactionHash,
		SharesMintedOrBurned: &record.SharesMintedOrBurned,
		SharePriceAtTime:     &record.SharePriceAtTime,
		CreatedAt:            time.Now().UTC(),
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

func (r *memoryVaultRepository) UpdateHarvestFrequency(_ context.Context, id uuid.UUID, frequency string) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.HarvestFrequency = frequency
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

func (r *memoryVaultRepository) RecordWithdrawal(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if record.Amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	model.CurrentBalance = model.CurrentBalance.Sub(record.Amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneVault(model)

	userID := record.UserID
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:                   uuid.New(),
		VaultID:              id,
		UserID:               &userID,
		Type:                 "withdrawal",
		Amount:               record.Amount,
		TransactionHash:      record.TransactionHash,
		SharesMintedOrBurned: &record.SharesMintedOrBurned,
		SharePriceAtTime:     &record.SharePriceAtTime,
		CreatedAt:            time.Now().UTC(),
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

func (r *memoryVaultRepository) RecordRebalance(_ context.Context, input vault.RebalanceRecordInput, withdrawRecord, depositRecord vault.TransactionRecord) error {
	model, ok := r.vaults[input.VaultID]
	if !ok {
		return vault.ErrVaultNotFound
	}

	// Apply withdrawal
	model.CurrentBalance = model.CurrentBalance.Sub(withdrawRecord.Amount)
	// Apply deposit
	model.CurrentBalance = model.CurrentBalance.Add(depositRecord.Amount)
	model.TotalDeposited = model.TotalDeposited.Add(depositRecord.Amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[input.VaultID] = cloneVault(model)

	// Add withdrawal transaction
	withdrawUserID := withdrawRecord.UserID
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:                   uuid.New(),
		VaultID:              input.VaultID,
		UserID:               &withdrawUserID,
		Type:                 "withdrawal",
		Amount:               withdrawRecord.Amount,
		TransactionHash:      withdrawRecord.TransactionHash,
		SharesMintedOrBurned: &withdrawRecord.SharesMintedOrBurned,
		SharePriceAtTime:     &withdrawRecord.SharePriceAtTime,
		CreatedAt:            time.Now().UTC(),
	})

	// Add deposit transaction
	depositUserID := depositRecord.UserID
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:                   uuid.New(),
		VaultID:              input.VaultID,
		UserID:               &depositUserID,
		Type:                 "deposit",
		Amount:               depositRecord.Amount,
		TransactionHash:      depositRecord.TransactionHash,
		SharesMintedOrBurned: &depositRecord.SharesMintedOrBurned,
		SharePriceAtTime:     &depositRecord.SharePriceAtTime,
		CreatedAt:            time.Now().UTC(),
	})

	// Add rebalance transaction
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:              uuid.New(),
		VaultID:         input.VaultID,
		UserID:          &input.UserID,
		Type:            "rebalance",
		Amount:          input.Amount,
		TransactionHash: input.TransactionHash,
		CreatedAt:       time.Now().UTC(),
	})

	return nil
}

func (r *memoryVaultRepository) ListUserVaultTransactions(_ context.Context, userID uuid.UUID, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	result := make([]vault.VaultTransaction, 0)
	for _, txn := range r.transactions {
		if txn.VaultID == vaultID && txn.UserID != nil && *txn.UserID == userID {
			result = append(result, txn)
		}
	}
	return result, nil
}

func cloneVault(model vault.Vault) vault.Vault {
	model.Allocations = append([]vault.Allocation(nil), model.Allocations...)
	return model
}
