package vault

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestBuildUserVaultPositionEmpty(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()

	position := EmptyUserVaultPosition(vaultID, userID)

	if position.TotalDepositedUSDC != "0.000000" {
		t.Fatalf("expected zero deposited, got %s", position.TotalDepositedUSDC)
	}
	if position.UnrealizedPnLUSDC != "+0.000000" {
		t.Fatalf("expected zero pnl, got %s", position.UnrealizedPnLUSDC)
	}
}

func TestBuildUserVaultPositionWithYield(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()
	depositTime := time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)

	shares := decimal.RequireFromString("1000")
	v := Vault{
		ID:             vaultID,
		TotalDeposited: decimal.RequireFromString("1000"),
		CurrentBalance: decimal.RequireFromString("1100"),
	}

	txns := []VaultTransaction{
		{
			VaultID:              vaultID,
			UserID:               &userID,
			Type:                 "deposit",
			Amount:               decimal.RequireFromString("1000"),
			SharesMintedOrBurned: &shares,
			CreatedAt:            depositTime,
		},
	}

	position := BuildUserVaultPosition(v, userID, txns)

	if position.CurrentValueUSDC != "1100.000000" {
		t.Fatalf("expected current value 1100, got %s", position.CurrentValueUSDC)
	}
	if position.UnrealizedPnLUSDC != "+100.000000" {
		t.Fatalf("expected pnl +100, got %s", position.UnrealizedPnLUSDC)
	}
	if position.UnrealizedPnLPct != "+10.00" {
		t.Fatalf("expected pnl pct +10.00, got %s", position.UnrealizedPnLPct)
	}
}

func TestBuildUserVaultPositionAccountsForWithdrawals(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()

	depositShares := decimal.RequireFromString("1000")
	withdrawShares := decimal.RequireFromString("200")

	v := Vault{
		ID:             vaultID,
		TotalDeposited: decimal.RequireFromString("1000"),
		CurrentBalance: decimal.RequireFromString("800"),
	}

	txns := []VaultTransaction{
		{
			VaultID:              vaultID,
			UserID:               &userID,
			Type:                 "deposit",
			Amount:               decimal.RequireFromString("1000"),
			SharesMintedOrBurned: &depositShares,
			CreatedAt:            time.Now().Add(-48 * time.Hour),
		},
		{
			VaultID:              vaultID,
			UserID:               &userID,
			Type:                 "withdrawal",
			Amount:               decimal.RequireFromString("200"),
			SharesMintedOrBurned: &withdrawShares,
			CreatedAt:            time.Now().Add(-24 * time.Hour),
		},
	}

	position := BuildUserVaultPosition(v, userID, txns)

	if position.SharesHeld != "800.000000" {
		t.Fatalf("expected 800 shares held, got %s", position.SharesHeld)
	}
	// current_value = 800 shares * 0.8 price = 640; net invested = 800; pnl = -160
	if position.UnrealizedPnLUSDC != "-160.000000" {
		t.Fatalf("expected pnl -160 after withdrawal, got %s", position.UnrealizedPnLUSDC)
	}
}
