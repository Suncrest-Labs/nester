package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

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

func (s *stubVaultRepository) RecordDeposit(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
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

func (s *stubVaultRepository) UpdateHarvestFrequency(_ context.Context, id uuid.UUID, frequency string) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepository) RecordWithdrawal(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
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

func (s *stubVaultRepository) ListUserVaultTransactions(_ context.Context, userID uuid.UUID, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, errors.New("not implemented")
}

func (s *stubVaultRepository) RecordRebalance(_ context.Context, _ vault.RebalanceRecordInput, _, _ vault.TransactionRecord) error {
	return errors.New("not implemented")
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

func (s *stubVaultRepositoryWithCount) RecordDeposit(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
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

func (s *stubVaultRepositoryWithCount) UpdateHarvestFrequency(_ context.Context, id uuid.UUID, frequency string) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) RecordWithdrawal(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
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

func (s *stubVaultRepositoryWithCount) ListUserVaultTransactions(_ context.Context, userID uuid.UUID, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) RecordRebalance(_ context.Context, _ vault.RebalanceRecordInput, _, _ vault.TransactionRecord) error {
	return errors.New("not implemented")
}

func (s *stubVaultRepositoryWithCount) ListVaults(_ context.Context, _ vault.ListFilter) ([]vault.Vault, int, error) {
	return nil, 0, errors.New("not implemented")
}

func TestRiskService_SingleProtocolVault_HighRisk(t *testing.T) {
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
				Protocol: "aave",
				Amount:   decimal.NewFromInt(1000),
				APY:      decimal.NewFromFloat(0.05),
			},
		},
	}

	repo := &stubVaultRepository{vault: vault, err: nil}
	service := NewRiskService(repo, nil)

	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if score == nil {
		t.Fatalf("expected score, got nil")
	}

	var concentrationFactor *RiskFactor
	for i := range score.Factors {
		if score.Factors[i].Name == "concentration" {
			concentrationFactor = &score.Factors[i]
			break
		}
	}

	if concentrationFactor == nil {
		t.Fatalf("expected concentration factor")
	}

	if concentrationFactor.Score != 100.0 {
		t.Errorf("expected concentration risk 100.0 for single protocol, got %.2f", concentrationFactor.Score)
	}

	if score.Tier != "medium" && score.Tier != "high" {
		t.Errorf("expected tier 'medium' or 'high' for score %.2f, got '%s'", score.Overall, score.Tier)
	}

	if len(score.Factors) < 5 {
		t.Errorf("expected at least 5 factors, got %d", len(score.Factors))
	}

	if score.Confidence < 0 || score.Confidence > 1 {
		t.Errorf("expected confidence between 0 and 1, got %.2f", score.Confidence)
	}
}

func TestRiskService_PerfectlyEqualFourWaySplit_LowConcentrationRisk(t *testing.T) {
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
				Protocol: "aave",
				Amount:   decimal.NewFromInt(250),
				APY:      decimal.NewFromFloat(0.05),
			},
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "blend",
				Amount:   decimal.NewFromInt(250),
				APY:      decimal.NewFromFloat(0.06),
			},
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "compound",
				Amount:   decimal.NewFromInt(250),
				APY:      decimal.NewFromFloat(0.04),
			},
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "aave",
				Amount:   decimal.NewFromInt(250),
				APY:      decimal.NewFromFloat(0.05),
			},
		},
	}

	repo := &stubVaultRepository{vault: vault, err: nil}
	service := NewRiskService(repo, nil)

	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if score == nil {
		t.Fatalf("expected score, got nil")
	}

	var concentrationFactor *RiskFactor
	for i := range score.Factors {
		if score.Factors[i].Name == "concentration" {
			concentrationFactor = &score.Factors[i]
			break
		}
	}

	if concentrationFactor == nil {
		t.Fatalf("expected concentration factor")
	}

	expectedConcentration := 25.0
	if concentrationFactor.Score < expectedConcentration-1 || concentrationFactor.Score > expectedConcentration+1 {
		t.Errorf("expected concentration risk ~25.0 for equal 4-way split, got %.2f", concentrationFactor.Score)
	}

	if score.Tier != "low" && score.Tier != "medium" {
		t.Errorf("expected tier 'low' or 'medium', got '%s'", score.Tier)
	}
}

func TestRiskService_EmptyVault_ReturnsError(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()
	vault := vault.Vault{
		ID: vaultID,
		UserID: userID,
		TotalDeposited: decimal.NewFromInt(0),
		CurrentBalance: decimal.NewFromInt(0),
		Currency: "USD",
		Allocations: []vault.Allocation{},
	}

	repo := &stubVaultRepository{vault: vault, err: nil}
	service := NewRiskService(repo, nil)

	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)

	if err == nil {
		t.Fatalf("expected error for empty vault, got nil")
	}
	if score != nil {
		t.Fatalf("expected nil score, got %v", score)
	}
	if err != ErrEmptyVault {
		t.Fatalf("expected ErrEmptyVault, got %v", err)
	}
}

func TestRiskService_VaultNotFound_ReturnsError(t *testing.T) {
	vaultID := uuid.New()
	repo := &stubVaultRepository{vault: vault.Vault{}, err: vault.ErrVaultNotFound}
	service := NewRiskService(repo, nil)

	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)

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
				Protocol: "aave",
				Amount:   decimal.NewFromInt(500),
				APY:      decimal.NewFromFloat(0.05),
			},
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "blend",
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
	service := NewRiskService(repo, nil)

	ctx := context.Background()

	score1, err1 := service.Score(ctx, vaultID)
	if err1 != nil {
		t.Fatalf("first call failed: %v", err1)
	}

	score2, err2 := service.Score(ctx, vaultID)
	if err2 != nil {
		t.Fatalf("second call failed: %v", err2)
	}

	if callCount != 1 {
		t.Fatalf("expected GetVault called once due to caching, got %d calls", callCount)
	}
	if score1.Overall != score2.Overall {
		t.Fatalf("cached score should be identical")
	}
}

func TestRiskService_MissingDataLowersConfidence(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()
	v := vault.Vault{
		ID: vaultID,
		UserID: userID,
		TotalDeposited: decimal.NewFromInt(1000),
		CurrentBalance: decimal.NewFromInt(1000),
		Currency: "USD",
		Allocations: []vault.Allocation{
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "unknown_protocol",
				Amount:   decimal.NewFromInt(1000),
				APY:      decimal.NewFromFloat(0),
			},
		},
	}

	repo := &stubVaultRepository{vault: v, err: nil}
	service := NewRiskService(repo, nil)

	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if score.Confidence >= 0.9 {
		t.Errorf("expected lower confidence for missing data, got %.2f", score.Confidence)
	}

	var unavailableCount int
	for _, factor := range score.Factors {
		if !factor.Available {
			unavailableCount++
		}
	}

	if unavailableCount == 0 {
		t.Errorf("expected some factors to be unavailable for unknown protocol")
	}
}

func TestRiskService_FactorBreakdown(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()
	v := vault.Vault{
		ID: vaultID,
		UserID: userID,
		TotalDeposited: decimal.NewFromInt(1000),
		CurrentBalance: decimal.NewFromInt(1000),
		Currency: "USD",
		Allocations: []vault.Allocation{
			{
				ID:     uuid.New(),
				VaultID: vaultID,
				Protocol: "aave",
				Amount:   decimal.NewFromInt(1000),
				APY:      decimal.NewFromFloat(0.05),
			},
		},
	}

	repo := &stubVaultRepository{vault: v, err: nil}
	service := NewRiskService(repo, nil)

	ctx := context.Background()
	score, err := service.Score(ctx, vaultID)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(score.Factors) < 5 {
		t.Fatalf("expected at least 5 factors, got %d", len(score.Factors))
	}

	for _, factor := range score.Factors {
		if factor.Name == "" {
			t.Errorf("factor name should not be empty")
		}
		if factor.Score < 0 || factor.Score > 100 {
			t.Errorf("factor %s score %.2f out of range [0, 100]", factor.Name, factor.Score)
		}
		if factor.Weight <= 0 || factor.Weight > 1 {
			t.Errorf("factor %s weight %.2f out of range (0, 1]", factor.Name, factor.Weight)
		}
		if factor.Reason == "" {
			t.Errorf("factor %s reason should not be empty", factor.Name)
		}
	}

	var totalWeight float64
	for _, factor := range score.Factors {
		totalWeight += factor.Weight
	}
	if totalWeight < 0.99 || totalWeight > 1.01 {
		t.Errorf("expected total weight ~1.0, got %.2f", totalWeight)
	}
}
