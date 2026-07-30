package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func TestBuildVaultHealthDashboard_AggregatesMultipleVaults(t *testing.T) {
	vaultA := uuid.New()
	vaultB := uuid.New()
	rebalanceAt := mustParseTime(t, "2026-05-26T08:00:00Z")
	apyA := decimal.RequireFromString("8.24")
	apyB := decimal.RequireFromString("12.50")

	data := admindomain.VaultHealthDashboardData{
		TotalTVL:        decimal.RequireFromString("5234891.00"),
		TotalDepositors: 892,
		Vaults: []admindomain.VaultHealthRow{
			{
				ID:                  vaultA,
				Name:                "Conservative",
				TVL:                 decimal.RequireFromString("1234000.00"),
				APY7d:               &apyA,
				Depositors:          234,
				PendingTransactions: 3,
				LastRebalanceAt:     &rebalanceAt,
				Status:              vault.StatusActive,
			},
			{
				ID:                  vaultB,
				Name:                "Growth",
				TVL:                 decimal.RequireFromString("4000891.00"),
				APY7d:               &apyB,
				Depositors:          658,
				PendingTransactions: 0,
				Status:              vault.StatusPaused,
			},
		},
	}

	result := buildVaultHealthDashboard(data)

	if result.TotalTVLUSDC != "5234891.00" {
		t.Fatalf("total_tvl_usdc = %q, want 5234891.00", result.TotalTVLUSDC)
	}
	if result.TotalDepositors != 892 {
		t.Fatalf("total_depositors = %d, want 892", result.TotalDepositors)
	}
	if len(result.Vaults) != 2 {
		t.Fatalf("vault count = %d, want 2", len(result.Vaults))
	}
	if result.Vaults[0].Status != "healthy" {
		t.Fatalf("vault[0].status = %q, want healthy", result.Vaults[0].Status)
	}
	if result.Vaults[1].Status != "paused" {
		t.Fatalf("vault[1].status = %q, want paused", result.Vaults[1].Status)
	}
	if result.Vaults[0].PendingTransactions != 3 {
		t.Fatalf("vault[0].pending_transactions = %d, want 3", result.Vaults[0].PendingTransactions)
	}
	if result.Vaults[0].LastRebalanceAt == nil || *result.Vaults[0].LastRebalanceAt != "2026-05-26T08:00:00Z" {
		t.Fatalf("vault[0].last_rebalance_at = %+v, want 2026-05-26T08:00:00Z", result.Vaults[0].LastRebalanceAt)
	}
	if len(result.SystemAlerts) != 0 {
		t.Fatalf("system_alerts = %+v, want none", result.SystemAlerts)
	}
}

func TestAPYDropAlert_FlagsWarningAboveThreshold(t *testing.T) {
	current := decimal.RequireFromString("6.00")
	previous := decimal.RequireFromString("10.00")

	alert := apyDropAlert("Conservative", &current, &previous)
	if alert == nil {
		t.Fatal("expected APY drop alert, got nil")
	}
	if alert.Severity != "warning" {
		t.Fatalf("severity = %q, want warning", alert.Severity)
	}
	if alert.Message != "Conservative vault APY dropped 40% in 24h" {
		t.Fatalf("message = %q", alert.Message)
	}
}

func TestAPYDropAlert_NoAlertBelowThreshold(t *testing.T) {
	current := decimal.RequireFromString("9.00")
	previous := decimal.RequireFromString("10.00")

	if alert := apyDropAlert("Conservative", &current, &previous); alert != nil {
		t.Fatalf("expected no alert for 10%% drop, got %+v", alert)
	}
}

func TestAPYDropAlert_NoAlertWithoutHistoricalAPY(t *testing.T) {
	current := decimal.RequireFromString("5.00")
	if alert := apyDropAlert("Conservative", &current, nil); alert != nil {
		t.Fatalf("expected no alert without historical APY, got %+v", alert)
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed.UTC()
}
