package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// stubVaultRepository is a minimal implementation of vault.Repository for testing
type stubVaultRepository struct {
	vault vault.Vault
	err   error
}

func (s *stubVaultRepository) CreateVault(_ context.Context, model vault.Vault) (vault.Vault, error) {
	return vault.Vault{}, errors.New("not implemented")
}

func (s *stubVaultRepository) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	return s.vault, s.err
}

func (s *stubVaultRepository) ListUserVaults(_ context.Context, userID uuid.UUID, filter vault.UserListFilter) ([]vault.Vault, int, error) {
	return nil, 0, errors.New("not implemented")
}

func (s *stubVaultRepository) RecordDeposit(_ context.Context, id uuid.UUID, amount decimal.Decimal) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) UpdateVaultBalances(_ context.Context, id uuid.UUID, totalDeposited decimal.Decimal, currentBalance decimal.Decimal) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) ReplaceAllocations(_ context.Context, vaultID uuid.UUID, allocations []vault.Allocation) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) UpdateVault(_ context.Context, id uuid.UUID, contractAddress string, status vault.VaultStatus) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) RecordWithdrawal(_ context.Context, id uuid.UUID, amount decimal.Decimal) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) RecordHarvest(_ context.Context, input vault.HarvestRecordInput) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) SoftDeleteVault(_ context.Context, id uuid.UUID) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) ListDeposits(_ context.Context, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, errors.New("not implemented")
}

func (s *stubVaultRepository) ListVaults(_ context.Context, _ vault.ListFilter) ([]vault.Vault, int, error) {
	return nil, 0, errors.New("not implemented")
}

type stubVaultRepositoryWithCount struct {
	vault      vault.Vault
	err        error
	callCount  int
	getVaultFn func(context.Context, uuid.UUID) (vault.Vault, error)
}

func (s *stubVaultRepositoryWithCount) CreateVault(_ context.Context, model vault.Vault) (vault.Vault, error) {
	return vault.Vault{}, errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	if s.getVaultFn != nil {
		return s.getVaultFn(context.Background(), id)
	}
	return s.vault, s.err
}

func (s *stubVaultRepositoryWithCount) ListUserVaults(_ context.Context, userID uuid.UUID, filter vault.UserListFilter) ([]vault.Vault, int, error) {
	return nil, 0, errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) RecordDeposit(_ context.Context, id uuid.UUID, amount decimal.Decimal) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) UpdateVaultBalances(_ context.Context, id uuid.UUID, totalDeposited decimal.Decimal, currentBalance decimal.Decimal) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) ReplaceAllocations(_ context.Context, vaultID uuid.UUID, allocations []vault.Allocation) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) UpdateVault(_ context.Context, id uuid.UUID, contractAddress string, status vault.VaultStatus) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) RecordWithdrawal(_ context.Context, id uuid.UUID, amount decimal.Decimal) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) RecordHarvest(_ context.Context, input vault.HarvestRecordInput) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) SoftDeleteVault(_ context.Context, id uuid.UUID) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) ListDeposits(_ context.Context, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) ListVaults(_ context.Context, _ vault.ListFilter) ([]vault.Vault, int, error) {
	return nil, 0, errors.New("not implemented")
}

func TestRiskService_SingleProtocolVault_HighRisk(t *testing.T) {
	// Arrange: single protocol vault (should have high concentration risk)
	vaultID := uuid.New()
	userID := uuid.New()
	vault := vault.Vault{
		ID: vaultID,
		UserID: userID,
		TotalDeposited: decimal.NewFromInt(1000),
		CurrentBalance: decimal.NewFromInt(1000),
		Currency: "USD",
		Allocations: []vault.Allocation{
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "Aave",
				Amount:   decimal.NewFromInt(1000),
				APY:      decimal.NewFromFloat(0.05), // 5%
			},
		},
	}
	
	repo := &stubVaultRepository{vault: vault, err: nil}
	service := NewRiskService(repo)
	
	// Act
	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)
	
	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if score == nil {
		t.Fatalf("expected score, got nil")
	}
	
	// Concentration risk should be 1.0 (100%) for single protocol
	if score.ConcentrationRisk != 100.0 {
		t.Errorf("expected concentration risk 100.0 for single protocol, got %.2f", score.ConcentrationRisk)
	}
	
	// Single Aave vault: high concentration but low protocol risk → medium overall
	if score.Tier != "medium" {
		t.Errorf("expected tier 'medium' for score %.2f, got '%s'", score.Overall, score.Tier)
	}
}

func TestRiskService_PerfectlyEqualFourWaySplit_LowConcentrationRisk(t *testing.T) {
	// Arrange: four equal protocol vaults
	vaultID := uuid.New()
	userID := uuid.New()
	vault := vault.Vault{
		ID: vaultID,
		UserID: userID,
		TotalDeposited: decimal.NewFromInt(1000),
		CurrentBalance: decimal.NewFromInt(1000),
		Currency: "USD",
		Allocations: []vault.Allocation{
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "Aave",
				Amount:   decimal.NewFromInt(250),
				APY:      decimal.NewFromFloat(0.05),
			},
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "Blend",
				Amount:   decimal.NewFromInt(250),
				APY:      decimal.NewFromFloat(0.06),
			},
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "Compound",
				Amount:   decimal.NewFromInt(250),
				APY:      decimal.NewFromFloat(0.04),
			},
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "Aave", // Using Aave again to test with known protocol
				Amount:   decimal.NewFromInt(250),
				APY:      decimal.NewFromFloat(0.05),
			},
		},
	}
	
	repo := &stubVaultRepository{vault: vault, err: nil}
	service := NewRiskService(repo)
	
	// Act
	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)
	
	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if score == nil {
		t.Fatalf("expected score, got nil")
	}
	
	// With 4 equal allocations, HHI = 4 * (0.25^2) = 4 * 0.0625 = 0.25
	// So concentration risk should be 0.25 * 100 = 25.0
	expectedConcentration := 25.0
	if score.ConcentrationRisk < expectedConcentration-1 || score.ConcentrationRisk > expectedConcentration+1 {
		t.Errorf("expected concentration risk ~25.0 for equal 4-way split, got %.2f", score.ConcentrationRisk)
	}
	
	// Should be low or medium tier depending on other risk factors
	if score.Tier != "low" && score.Tier != "medium" {
		t.Errorf("expected tier 'low' or 'medium', got '%s'", score.Tier)
	}
}

func TestRiskService_EmptyVault_ReturnsError(t *testing.T) {
	// Arrange: empty vault (no allocations)
	vaultID := uuid.New()
	userID := uuid.New()
	vault := vault.Vault{
		ID: vaultID,
		UserID: userID,
		TotalDeposited: decimal.NewFromInt(0),
		CurrentBalance: decimal.NewFromInt(0),
		Currency: "USD",
		Allocations: []vault.Allocation{}, // empty
	}
	
	repo := &stubVaultRepository{vault: vault, err: nil}
	service := NewRiskService(repo)
	
	// Act
	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)
	
	// Assert
	if err == nil {
		t.Fatalf("expected error for empty vault, got nil")
	}
	if score != nil {
		t.Fatalf("expected nil score, got %v", score)
	}
	if !errors.Is(err, errors.New("empty vault: no allocations")) {
		// Check if it's our specific error
		if err.Error() != "empty vault: no allocations" {
			t.Fatalf("expected 'empty vault: no allocations' error, got %v", err)
		}
	}
}

func TestRiskService_VaultNotFound_ReturnsError(t *testing.T) {
	// Arrange: vault not found
	vaultID := uuid.New()
	repo := &stubVaultRepository{vault: vault.Vault{}, err: vault.ErrVaultNotFound}
	service := NewRiskService(repo)
	
	// Act
	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)
	
	// Assert
	if err == nil {
		t.Fatalf("expected error for vault not found, got nil")
	}
	if score != nil {
		t.Fatalf("expected nil score, got %v", score)
	}
	if !errors.Is(err, vault.ErrVaultNotFound) {
		t.Fatalf("expected vault not found error, got %v", err)
	}
}

func TestRiskService_Caching(t *testing.T) {
	// Arrange: vault with some allocations
	vaultID := uuid.New()
	userID := uuid.New()
	v := vault.Vault{
		ID: vaultID,
		UserID: userID,
		TotalDeposited: decimal.NewFromInt(1000),
		CurrentBalance: decimal.NewFromInt(1100),
		Currency: "USD",
		Allocations: []vault.Allocation{
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "Aave",
				Amount:   decimal.NewFromInt(500),
				APY:      decimal.NewFromFloat(0.05),
			},
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "Blend",
				Amount:   decimal.NewFromInt(500),
				APY:      decimal.NewFromFloat(0.10),
			},
		},
	}
	
	callCount := 0
	repo := &stubVaultRepositoryWithCount{
		vault: v,
		getVaultFn: func(_ context.Context, id uuid.UUID) (vault.Vault, error) {
			callCount++
			return v, nil
		},
	}
	service := NewRiskService(repo)
	
	// Act
	ctx := context.Background()
	
	// First call
	score1, err1 := service.Score(ctx, vaultID)
	if err1 != nil {
		t.Fatalf("first call failed: %v", err1)
	}
	
	// Second call (should use cache)
	score2, err2 := service.Score(ctx, vaultID)
	if err2 != nil {
		t.Fatalf("second call failed: %v", err2)
	}
	
	// Assert
	if callCount != 1 {
		t.Fatalf("expected GetVault called once due to caching, got %d calls", callCount)
	}
	if score1.Overall != score2.Overall {
		t.Fatalf("cached score should be identical")
	}
}