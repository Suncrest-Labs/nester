package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestVaultServiceHarvestZeroYield(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)
	svc.SetHarvestDefaultCompound(true)

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	result, err := svc.HarvestVault(context.Background(), HarvestVaultInput{
		VaultID: created.ID,
		UserID:  userID,
	})
	if err != nil {
		t.Fatalf("HarvestVault() error = %v", err)
	}
	if result.GrossYieldUSDC != "0.000000" {
		t.Fatalf("expected zero gross yield, got %s", result.GrossYieldUSDC)
	}
}

func TestVaultServiceHarvestRecordsTransaction(t *testing.T) {
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

	compound := false
	result, err := svc.HarvestVault(context.Background(), HarvestVaultInput{
		VaultID: created.ID,
		UserID:  userID,
		Compound: &compound,
	})
	if err != nil {
		t.Fatalf("HarvestVault() error = %v", err)
	}
	if result.NetYieldUSDC != "9.000000" {
		t.Fatalf("expected net yield 9.000000, got %s", result.NetYieldUSDC)
	}
	if result.Compounded {
		t.Fatal("expected compounded=false")
	}

	foundHarvest := false
	for _, txn := range repo.transactions {
		if txn.VaultID == created.ID && txn.Type == "harvest" {
			foundHarvest = true
		}
	}
	if !foundHarvest {
		t.Fatal("expected harvest transaction record")
	}
}

func TestVaultServiceHarvestForbidden(t *testing.T) {
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

	_, err = svc.HarvestVault(context.Background(), HarvestVaultInput{
		VaultID: created.ID,
		UserID:  otherID,
	})
	if err == nil {
		t.Fatal("expected error for non-owner harvest")
	}
	if err != vault.ErrVaultForbidden {
		t.Fatalf("HarvestVault() error = %v, want %v", err, vault.ErrVaultForbidden)
	}
}
