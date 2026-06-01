package postgres

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	tvlsvc "github.com/suncrestlabs/nester/apps/api/internal/service/tvl"
)

type mockTVLChain struct {
	assets decimal.Decimal
	err    error
}

func (m mockTVLChain) TotalAssets(_ context.Context, _ string) (decimal.Decimal, error) {
	if m.err != nil {
		return decimal.Zero, m.err
	}
	return m.assets, nil
}

func applyTVLMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	applyPerformanceMigrations(t, db)
	path := filepath.Join("..", "..", "..", "migrations", "024_create_vault_tvl_snapshots.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile migration: %v", err)
	}
	if _, err := db.Exec(string(contents)); err != nil {
		t.Fatalf("apply tvl migration: %v", err)
	}
}

func TestTVLTrackerIntegration_OneTick(t *testing.T) {
	db := openIntegrationDB(t)
	applyTVLMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTVLRepository(db)
	vaultRepo := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	now := time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC)
	tracker := tvlsvc.NewTracker(
		repo,
		vaultRepo,
		mockTVLChain{assets: decimal.RequireFromString("1250000.0000000")},
		time.Hour,
	)
	tracker.SetClock(func() time.Time { return now })

	if err := tracker.RefreshAll(ctx); err != nil {
		t.Fatalf("RefreshAll() error = %v", err)
	}

	snap, err := repo.LatestForVault(ctx, vaultID)
	if err != nil {
		t.Fatalf("LatestForVault() error = %v", err)
	}
	if !snap.TVLUSDC.Equal(decimal.RequireFromString("1250000.000000")) {
		t.Fatalf("tvl_usdc = %s, want 1250000.000000", snap.TVLUSDC)
	}
	if !snap.SnapshotAt.Equal(now) {
		t.Fatalf("snapshot_at = %v, want %v", snap.SnapshotAt, now)
	}
}
