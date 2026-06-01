package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	performancesvc "github.com/suncrestlabs/nester/apps/api/internal/service/performance"
)

type mockYieldRegistry struct {
	apyByProtocol map[string]uint32
}

func (m mockYieldRegistry) SourceAPYBPS(_ context.Context, protocolID string) (uint32, error) {
	return m.apyByProtocol[protocolID], nil
}

func TestAPYRefresherIntegration_OneTick(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	vaultRepo := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	if err := vaultRepo.ReplaceAllocations(ctx, vaultID, []vault.Allocation{
		{
			ID:          uuid.New(),
			VaultID:     vaultID,
			Protocol:    "aave",
			Amount:      decimal.RequireFromString("600.00"),
			APY:         decimal.RequireFromString("4.00"),
			AllocatedAt: time.Now().UTC(),
		},
		{
			ID:          uuid.New(),
			VaultID:     vaultID,
			Protocol:    "blend",
			Amount:      decimal.RequireFromString("400.00"),
			APY:         decimal.RequireFromString("6.00"),
			AllocatedAt: time.Now().UTC(),
		},
	}); err != nil {
		t.Fatalf("ReplaceAllocations() error = %v", err)
	}

	refresher := performancesvc.NewAPYRefresher(
		performancesvc.APYRefresherConfig{
			Interval:              time.Minute,
			BroadcastThresholdBPS: 50,
			RegistryAddress:       "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM",
		},
		repo,
		vaultRepo,
		mockYieldRegistry{apyByProtocol: map[string]uint32{
			"aave":  500,
			"blend": 700,
		}},
		nil,
	)

	if err := refresher.RefreshOnce(ctx); err != nil {
		t.Fatalf("RefreshOnce() error = %v", err)
	}

	latest, err := repo.LatestForVault(ctx, vaultID)
	if err != nil {
		t.Fatalf("LatestForVault() error = %v", err)
	}
	if len(latest.AllocationBreakdown) != 2 {
		t.Fatalf("expected 2 breakdown entries, got %d", len(latest.AllocationBreakdown))
	}

	bps, ok := refresher.CachedAPYBPS(vaultID)
	if !ok {
		t.Fatal("expected cached APY after refresh")
	}
	if bps != 580 {
		t.Fatalf("cached apy bps = %d, want 580", bps)
	}
}
