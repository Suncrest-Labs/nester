package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type allocationAdminRepository struct {
	detail admindomain.VaultDetail
}

func (r *allocationAdminRepository) GetVaultHealthDashboard(context.Context) (admindomain.VaultHealthDashboardData, error) {
	return admindomain.VaultHealthDashboardData{}, nil
}
func (r *allocationAdminRepository) ListVaults(context.Context, admindomain.VaultListFilter) ([]admindomain.VaultSummary, int, error) {
	return nil, 0, nil
}
func (r *allocationAdminRepository) GetVaultDetail(context.Context, uuid.UUID) (admindomain.VaultDetail, error) {
	return r.detail, nil
}
func (r *allocationAdminRepository) UpdateVaultStatus(context.Context, uuid.UUID, vault.VaultStatus) (admindomain.VaultDetail, error) {
	return r.detail, nil
}
func (r *allocationAdminRepository) ListSettlements(context.Context, admindomain.SettlementListFilter) ([]admindomain.SettlementSummary, int, error) {
	return nil, 0, nil
}
func (r *allocationAdminRepository) ListUsers(context.Context, admindomain.UserListFilter) ([]admindomain.UserSummary, int, error) {
	return nil, 0, nil
}
func (r *allocationAdminRepository) GetLastEventIndexedAt(context.Context) (*time.Time, error) {
	return nil, nil
}
func (r *allocationAdminRepository) DatabaseHealth(context.Context) (int64, error) {
	return 0, nil
}
func (r *allocationAdminRepository) HasInFlightRebalance(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}
func (r *allocationAdminRepository) CreateVaultRebalance(context.Context, admindomain.VaultRebalanceRecord) (admindomain.VaultRebalanceRecord, error) {
	return admindomain.VaultRebalanceRecord{}, nil
}
func (r *allocationAdminRepository) UpdateVaultRebalance(context.Context, admindomain.VaultRebalanceRecord) (admindomain.VaultRebalanceRecord, error) {
	return admindomain.VaultRebalanceRecord{}, nil
}
func (r *allocationAdminRepository) ListVaultRebalances(context.Context, uuid.UUID) ([]admindomain.VaultRebalanceRecord, error) {
	return nil, nil
}

type allocationVaultRepository struct {
	allocations []vault.Allocation
}

func (r *allocationVaultRepository) CreateVault(context.Context, vault.Vault) (vault.Vault, error) {
	return vault.Vault{}, nil
}
func (r *allocationVaultRepository) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	return vault.Vault{ID: id, Allocations: r.allocations}, nil
}
func (r *allocationVaultRepository) ListUserVaults(context.Context, uuid.UUID, vault.UserListFilter) ([]vault.Vault, int, error) {
	return nil, 0, nil
}
func (r *allocationVaultRepository) ListVaults(context.Context, vault.ListFilter) ([]vault.Vault, int, error) {
	return nil, 0, nil
}
func (r *allocationVaultRepository) RecordDeposit(context.Context, uuid.UUID, vault.TransactionRecord) error {
	return nil
}
func (r *allocationVaultRepository) UpdateVaultBalances(context.Context, uuid.UUID, decimal.Decimal, decimal.Decimal) error {
	return nil
}
func (r *allocationVaultRepository) ReplaceAllocations(_ context.Context, _ uuid.UUID, allocations []vault.Allocation) error {
	r.allocations = append([]vault.Allocation{}, allocations...)
	return nil
}
func (r *allocationVaultRepository) UpdateVault(context.Context, uuid.UUID, string, vault.VaultStatus) error {
	return nil
}
func (r *allocationVaultRepository) RecordWithdrawal(context.Context, uuid.UUID, vault.TransactionRecord) error {
	return nil
}
func (r *allocationVaultRepository) RecordHarvest(context.Context, vault.HarvestRecordInput) error {
	return nil
}
func (r *allocationVaultRepository) SoftDeleteVault(context.Context, uuid.UUID) error {
	return nil
}
func (r *allocationVaultRepository) ListDeposits(context.Context, uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, nil
}
func (r *allocationVaultRepository) ListUserVaultTransactions(context.Context, uuid.UUID, uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, nil
}

type recordingChainInvoker struct {
	weights []AllocationWeightEntry
}

func (r *recordingChainInvoker) PauseVault(context.Context, string) error    { return nil }
func (r *recordingChainInvoker) UnpauseVault(context.Context, string) error  { return nil }
func (r *recordingChainInvoker) RebalanceVault(context.Context, string) (string, error) {
	return "", nil
}
func (r *recordingChainInvoker) SimulateRebalanceVault(context.Context, string) error {
	return nil
}
func (r *recordingChainInvoker) SetAllocationWeights(_ context.Context, _ string, weights []AllocationWeightEntry) error {
	r.weights = append([]AllocationWeightEntry{}, weights...)
	return nil
}

func TestAdminServiceCreateAllocationUpdatesChainAndDatabase(t *testing.T) {
	vaultID := uuid.New()
	existingID := uuid.New()
	now := time.Now().UTC()

	adminRepo := &allocationAdminRepository{
		detail: admindomain.VaultDetail{
			VaultSummary: admindomain.VaultSummary{
				ID:             vaultID,
				CurrentBalance: decimal.Zero,
			},
			Allocations: []vault.Allocation{
				{ID: existingID, VaultID: vaultID, Protocol: "aave", Amount: decimal.RequireFromString("60"), APY: decimal.RequireFromString("4"), AllocatedAt: now},
			},
		},
	}
	vaultRepo := &allocationVaultRepository{
		allocations: adminRepo.detail.Allocations,
	}
	chain := &recordingChainInvoker{}

	svc := NewAdminService(adminRepo, vaultRepo, chain, "", "", "CSTRATEGY001", 5)

	created, err := svc.CreateAllocation(context.Background(), CreateAllocationInput{
		VaultID:  vaultID,
		Protocol: "compound",
		Weight:   decimal.RequireFromString("40"),
		APY:      decimal.RequireFromString("5"),
	})
	if err != nil {
		t.Fatalf("CreateAllocation() error = %v", err)
	}
	if created.Protocol != "compound" {
		t.Fatalf("protocol = %q, want compound", created.Protocol)
	}
	if len(vaultRepo.allocations) != 2 {
		t.Fatalf("stored allocations = %d, want 2", len(vaultRepo.allocations))
	}
	if len(chain.weights) != 2 {
		t.Fatalf("chain weights = %d, want 2", len(chain.weights))
	}
	if chain.weights[1].WeightBps != 4000 {
		t.Fatalf("compound weight bps = %d, want 4000", chain.weights[1].WeightBps)
	}
}

func TestAdminServiceCreateAllocationRejectsDuplicateProtocol(t *testing.T) {
	vaultID := uuid.New()
	adminRepo := &allocationAdminRepository{
		detail: admindomain.VaultDetail{
			VaultSummary: admindomain.VaultSummary{ID: vaultID},
			Allocations: []vault.Allocation{
				{Protocol: "aave", Amount: decimal.RequireFromString("100"), APY: decimal.RequireFromString("4")},
			},
		},
	}
	svc := NewAdminService(adminRepo, &allocationVaultRepository{}, NoopVaultChainInvoker{}, "", "", "", 5)

	_, err := svc.CreateAllocation(context.Background(), CreateAllocationInput{
		VaultID:  vaultID,
		Protocol: "aave",
		Weight:   decimal.RequireFromString("0"),
		APY:      decimal.RequireFromString("4"),
	})
	if err != vault.ErrDuplicateProtocol {
		t.Fatalf("CreateAllocation() error = %v, want %v", err, vault.ErrDuplicateProtocol)
	}
}

func TestAdminServiceDeleteAllocationRejectsNonZeroBalance(t *testing.T) {
	vaultID := uuid.New()
	allocationID := uuid.New()
	adminRepo := &allocationAdminRepository{
		detail: admindomain.VaultDetail{
			VaultSummary: admindomain.VaultSummary{
				ID:             vaultID,
				CurrentBalance: decimal.RequireFromString("1000"),
			},
			Allocations: []vault.Allocation{
				{ID: allocationID, Protocol: "aave", Amount: decimal.RequireFromString("100"), APY: decimal.RequireFromString("4")},
			},
		},
	}
	svc := NewAdminService(adminRepo, &allocationVaultRepository{}, NoopVaultChainInvoker{}, "", "", "", 5)

	err := svc.DeleteAllocation(context.Background(), DeleteAllocationInput{
		VaultID:      vaultID,
		AllocationID: allocationID,
	})
	if err != vault.ErrAllocationHasBalance {
		t.Fatalf("DeleteAllocation() error = %v, want %v", err, vault.ErrAllocationHasBalance)
	}
}

func TestAdminServiceUpdateAllocationValidatesWeightSum(t *testing.T) {
	vaultID := uuid.New()
	allocationID := uuid.New()
	adminRepo := &allocationAdminRepository{
		detail: admindomain.VaultDetail{
			VaultSummary: admindomain.VaultSummary{ID: vaultID},
			Allocations: []vault.Allocation{
				{ID: allocationID, Protocol: "aave", Amount: decimal.RequireFromString("60"), APY: decimal.RequireFromString("4")},
				{ID: uuid.New(), Protocol: "compound", Amount: decimal.RequireFromString("40"), APY: decimal.RequireFromString("5")},
			},
		},
	}
	svc := NewAdminService(adminRepo, &allocationVaultRepository{}, NoopVaultChainInvoker{}, "", "", "", 5)

	weight := decimal.RequireFromString("70")
	_, err := svc.UpdateAllocation(context.Background(), UpdateAllocationInput{
		VaultID:      vaultID,
		AllocationID: allocationID,
		Weight:       &weight,
	})
	if err != vault.ErrInvalidAllocation {
		t.Fatalf("UpdateAllocation() error = %v, want %v", err, vault.ErrInvalidAllocation)
	}
}
