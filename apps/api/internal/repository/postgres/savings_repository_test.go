package postgres

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savings"
	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

func TestSavingsRepository_InsertAndSince(t *testing.T) {
	db := openIntegrationDB(t)
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "..", "migrations"))
	if _, err := db.Exec(`TRUNCATE TABLE savings_snapshots RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	repo := NewSavingsRepository(db)
	ctx := context.Background()

	if _, err := repo.Latest(ctx); !errors.Is(err, savings.ErrSnapshotNotFound) {
		t.Fatalf("Latest() on empty table error = %v, want ErrSnapshotNotFound", err)
	}

	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	huge := decimal.RequireFromString("99999999999999999999999999999999999999") // 38 digits, numeric(38,0) max
	snaps := []savings.Snapshot{
		{TakenAt: base.Add(2 * time.Hour), SharePrice: decimal.NewFromInt(10_300_000), TotalAssets: huge, TotalSupply: decimal.NewFromInt(3), ReserveShares: decimal.NewFromInt(4)},
		{TakenAt: base, SharePrice: decimal.NewFromInt(10_000_000), TotalAssets: decimal.NewFromInt(1), TotalSupply: decimal.NewFromInt(2), ReserveShares: decimal.Zero},
		{TakenAt: base.Add(time.Hour), SharePrice: decimal.NewFromInt(10_100_000), TotalAssets: decimal.NewFromInt(5), TotalSupply: decimal.NewFromInt(6), ReserveShares: decimal.NewFromInt(7)},
	}
	for _, s := range snaps {
		if err := repo.Insert(ctx, s); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
	}

	got, err := repo.Since(ctx, base.Add(time.Hour))
	if err != nil {
		t.Fatalf("Since() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Since() len = %d, want 2", len(got))
	}
	if !got[0].TakenAt.Equal(base.Add(time.Hour)) || !got[1].TakenAt.Equal(base.Add(2*time.Hour)) {
		t.Fatalf("Since() not ascending by taken_at: %v, %v", got[0].TakenAt, got[1].TakenAt)
	}
	if !got[1].TotalAssets.Equal(huge) {
		t.Fatalf("TotalAssets round-trip: got %s, want %s", got[1].TotalAssets, huge)
	}
	if !got[0].ReserveShares.Equal(decimal.NewFromInt(7)) || !got[0].TotalSupply.Equal(decimal.NewFromInt(6)) {
		t.Fatalf("supply/reserve mismatch: %+v", got[0])
	}

	all, err := repo.Since(ctx, base.Add(-time.Hour))
	if err != nil || len(all) != 3 {
		t.Fatalf("Since(all) = %d, %v; want 3", len(all), err)
	}

	latest, err := repo.Latest(ctx)
	if err != nil {
		t.Fatalf("Latest() error = %v", err)
	}
	if !latest.TakenAt.Equal(base.Add(2*time.Hour)) || !latest.SharePrice.Equal(decimal.NewFromInt(10_300_000)) {
		t.Fatalf("Latest() = %+v", latest)
	}
}
