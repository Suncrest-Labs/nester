package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/caps"
)

// TestRecordDepositRejectsOverMainnetTVLCap proves the mainnet TVL cap is
// enforced end-to-end through the real VaultService, not just against a
// mock of VaultTVLCapManager (nester#1376).
func TestRecordDepositRejectsOverMainnetTVLCap(t *testing.T) {
	userID := uuid.New()
	repository := newMemoryVaultRepository(userID)
	svc := NewVaultService(repository)
	svc.SetTVLCapManager(NewMainnetTVLCapManager(true, decimal.RequireFromString("100")))

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CA123",
		Currency:        "usdc",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// First deposit stays under the cap and must succeed.
	if _, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID,
		UserID:  userID,
		Amount:  decimal.RequireFromString("60"),
	}); err != nil {
		t.Fatalf("RecordDeposit() under cap error = %v", err)
	}

	// Second deposit would push the vault's TVL from 60 to 160, over the
	// cap of 100, and must be rejected before crediting anything.
	_, err = svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID,
		UserID:  userID,
		Amount:  decimal.RequireFromString("60"),
	})
	if !errors.Is(err, caps.ErrTVLCapExceeded) {
		t.Fatalf("RecordDeposit() over cap error = %v, want %v", err, caps.ErrTVLCapExceeded)
	}

	// The rejected deposit must not have been credited.
	got, err := repository.GetVault(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !got.CurrentBalance.Equal(decimal.RequireFromString("60")) {
		t.Fatalf("CurrentBalance = %s, want 60 (rejected deposit must not be credited)", got.CurrentBalance)
	}
}

// TestRecordDepositIgnoresTVLCapOffMainnet proves the cap is never enforced
// when the manager is configured for a non-mainnet network, even past the
// configured ceiling.
func TestRecordDepositIgnoresTVLCapOffMainnet(t *testing.T) {
	userID := uuid.New()
	repository := newMemoryVaultRepository(userID)
	svc := NewVaultService(repository)
	svc.SetTVLCapManager(NewMainnetTVLCapManager(false, decimal.RequireFromString("100")))

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CA123",
		Currency:        "usdc",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	if _, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID,
		UserID:  userID,
		Amount:  decimal.RequireFromString("500"),
	}); err != nil {
		t.Fatalf("RecordDeposit() off mainnet error = %v, want nil (cap must not apply off mainnet)", err)
	}
}
