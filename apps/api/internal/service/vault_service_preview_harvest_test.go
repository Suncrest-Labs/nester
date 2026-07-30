package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestPreviewHarvestNormalCase(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	model := repo.vaults[created.ID]
	model.YieldEarned = decimal.RequireFromString("12.5")
	model.CurrentBalance = decimal.RequireFromString("112.5")
	model.TotalDeposited = decimal.RequireFromString("100")
	repo.vaults[created.ID] = cloneVault(model)

	preview, err := svc.PreviewHarvest(context.Background(), PreviewHarvestInput{
		VaultID:  created.ID,
		UserID:   userID,
		Compound: false,
	})
	if err != nil {
		t.Fatalf("PreviewHarvest() error = %v", err)
	}

	if preview.GrossYieldUSDC != "12.500000" {
		t.Fatalf("gross yield = %s, want 12.500000", preview.GrossYieldUSDC)
	}
	if preview.PerformanceFeeUSDC != "1.250000" {
		t.Fatalf("performance fee = %s, want 1.250000", preview.PerformanceFeeUSDC)
	}
	if preview.NetYieldUSDC != "11.250000" {
		t.Fatalf("net yield = %s, want 11.250000", preview.NetYieldUSDC)
	}
	if preview.PerformanceFeeBPS != 1000 {
		t.Fatalf("performance_fee_bps = %d, want 1000", preview.PerformanceFeeBPS)
	}
	if preview.Impaired {
		t.Fatal("expected impaired=false for profitable vault")
	}
	if preview.EstimatedNewShares != "" {
		t.Fatalf("expected no estimated_new_shares when compound=false, got %s", preview.EstimatedNewShares)
	}
	if preview.VaultID != created.ID.String() {
		t.Fatalf("vault_id = %s, want %s", preview.VaultID, created.ID.String())
	}

	// Verify nothing was written to DB
	fresh, _ := repo.GetVault(context.Background(), created.ID)
	if !fresh.YieldEarned.Equal(decimal.RequireFromString("12.5")) {
		t.Fatal("PreviewHarvest must not modify vault state")
	}
}

func TestPreviewHarvestImpairedCase(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// Current balance < total deposited → impaired
	model := repo.vaults[created.ID]
	model.CurrentBalance = decimal.RequireFromString("90")
	model.TotalDeposited = decimal.RequireFromString("100")
	model.YieldEarned = decimal.Zero
	repo.vaults[created.ID] = cloneVault(model)

	preview, err := svc.PreviewHarvest(context.Background(), PreviewHarvestInput{
		VaultID:  created.ID,
		UserID:   userID,
		Compound: false,
	})
	if err != nil {
		t.Fatalf("PreviewHarvest() error = %v", err)
	}

	if preview.GrossYieldUSDC != "0.000000" {
		t.Fatalf("gross yield = %s, want 0.000000", preview.GrossYieldUSDC)
	}
	if preview.PerformanceFeeUSDC != "0.000000" {
		t.Fatalf("performance fee = %s, want 0.000000", preview.PerformanceFeeUSDC)
	}
	if preview.NetYieldUSDC != "0.000000" {
		t.Fatalf("net yield = %s, want 0.000000", preview.NetYieldUSDC)
	}
	if !preview.Impaired {
		t.Fatal("expected impaired=true when balance < total deposited")
	}
}

func TestPreviewHarvestCompoundCase(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	model := repo.vaults[created.ID]
	model.YieldEarned = decimal.RequireFromString("10")
	model.CurrentBalance = decimal.RequireFromString("110")
	model.TotalDeposited = decimal.RequireFromString("100")
	repo.vaults[created.ID] = cloneVault(model)

	preview, err := svc.PreviewHarvest(context.Background(), PreviewHarvestInput{
		VaultID:  created.ID,
		UserID:   userID,
		Compound: true,
	})
	if err != nil {
		t.Fatalf("PreviewHarvest() error = %v", err)
	}

	if preview.Compounded != true {
		t.Fatal("expected compounded=true")
	}
	if preview.EstimatedNewShares == "" {
		t.Fatal("expected estimated_new_shares when compound=true")
	}
	// net = 10 - 1 = 9; share price = 110/100 = 1.1; shares = 9/1.1 ≈ 8.181818
	shares, _ := decimal.NewFromString(preview.EstimatedNewShares)
	if shares.Cmp(decimal.Zero) <= 0 {
		t.Fatalf("expected positive estimated_new_shares, got %s", preview.EstimatedNewShares)
	}
}

func TestPreviewHarvestForbiddenForOtherUser(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	repo := newMemoryVaultRepository(ownerID, otherID)
	svc := NewVaultService(repo)

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID:          ownerID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	_, err = svc.PreviewHarvest(context.Background(), PreviewHarvestInput{
		VaultID: created.ID,
		UserID:  otherID,
	})
	if err != vault.ErrVaultForbidden {
		t.Fatalf("expected ErrVaultForbidden, got %v", err)
	}
}
