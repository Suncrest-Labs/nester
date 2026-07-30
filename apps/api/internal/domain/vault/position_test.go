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

func TestBuildUserVaultPositionManySmallDepositsAndHarvestsLimitRoundingDrift(t *testing.T) {
	const operationCount = 5000

	vaultID := uuid.New()
	userID := uuid.New()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	oneMillion := decimal.RequireFromString("1000000")
	depositAmount := decimal.RequireFromString("0.01")
	harvestAmount := decimal.RequireFromString("0.000001")

	v := Vault{
		ID:             vaultID,
		TotalDeposited: oneMillion,
		CurrentBalance: oneMillion,
	}
	initialShares := oneMillion
	txns := []VaultTransaction{{
		VaultID:              vaultID,
		UserID:               &userID,
		Type:                 "deposit",
		Amount:               oneMillion,
		SharesMintedOrBurned: &initialShares,
		CreatedAt:            at,
	}}
	sharesHeld := initialShares

	for i := 1; i <= operationCount; i++ {
		// Apply a small harvest before each deposit and mint shares at the
		// unrounded live price used by the ledger.
		v.CurrentBalance = v.CurrentBalance.Add(harvestAmount)
		sharePrice := v.CurrentBalance.Div(v.TotalDeposited)
		shares := depositAmount.Div(sharePrice)

		at = at.Add(time.Minute)
		txns = append(txns, VaultTransaction{
			VaultID:              vaultID,
			UserID:               &userID,
			Type:                 "deposit",
			Amount:               depositAmount,
			SharesMintedOrBurned: &shares,
			CreatedAt:            at,
		})
		sharesHeld = sharesHeld.Add(shares)
		v.TotalDeposited = v.TotalDeposited.Add(depositAmount)
		v.CurrentBalance = v.CurrentBalance.Add(depositAmount)
	}

	position := BuildUserVaultPosition(v, userID, txns)
	got, err := decimal.NewFromString(position.CurrentValueUSDC)
	if err != nil {
		t.Fatalf("parse current value %q: %v", position.CurrentValueUSDC, err)
	}
	expected := sharesHeld.Mul(v.CurrentBalance.Div(v.TotalDeposited)).Round(positionDecimalPlaces)
	epsilon := decimal.RequireFromString("0.000001")
	if got.Sub(expected).Abs().GreaterThan(epsilon) {
		t.Fatalf("current value drifted: got %s, expected %s, epsilon %s", got, expected, epsilon)
	}
}
