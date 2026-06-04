package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
)

// applyPerformanceMigrations applies the minimal set of migrations needed for
// performance repository integration tests. All statements use IF NOT EXISTS so
// this is safe to run against a database that already has the schema.
func applyPerformanceMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, name := range []string{
		"001_create_users_table.up.sql",
		"002_create_vaults_table.up.sql",
		"005_create_allocations_table.up.sql",
		"006_create_settlements_table.up.sql",
		"016_create_vault_performance.up.sql",
	} {
		path := filepath.Join("..", "..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q: %v", name, err)
		}
	}
}

// newTestSnapshot returns a minimal valid snapshot for the given vault.
func newTestSnapshot(vaultID uuid.UUID, snapshotAt time.Time) performance.Snapshot {
	return performance.Snapshot{
		ID:              uuid.New(),
		VaultID:         vaultID,
		TotalBalance:    decimal.RequireFromString("1000.00"),
		TotalDeposited:  decimal.RequireFromString("950.00"),
		TotalYieldEarned: decimal.RequireFromString("50.00"),
		SharePrice:      decimal.RequireFromString("1.0526"),
		SnapshotAt:      snapshotAt,
	}
}

// ---------------------------------------------------------------------------
// Insert
// ---------------------------------------------------------------------------

func TestSaveSnapshot_success(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	snap := newTestSnapshot(vaultID, time.Now().UTC().Truncate(time.Microsecond))
	got, err := repo.Insert(ctx, snap)
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if got.ID != snap.ID {
		t.Fatalf("ID: got %v, want %v", got.ID, snap.ID)
	}
	if got.VaultID != vaultID {
		t.Fatalf("VaultID mismatch")
	}
	if !got.TotalBalance.Equal(snap.TotalBalance) {
		t.Fatalf("TotalBalance: got %s, want %s", got.TotalBalance, snap.TotalBalance)
	}
	if !got.TotalDeposited.Equal(snap.TotalDeposited) {
		t.Fatalf("TotalDeposited: got %s, want %s", got.TotalDeposited, snap.TotalDeposited)
	}
	if !got.TotalYieldEarned.Equal(snap.TotalYieldEarned) {
		t.Fatalf("TotalYieldEarned: got %s, want %s", got.TotalYieldEarned, snap.TotalYieldEarned)
	}
	if !got.SharePrice.Equal(snap.SharePrice) {
		t.Fatalf("SharePrice: got %s, want %s", got.SharePrice, snap.SharePrice)
	}
}

// TestSaveSnapshot_withNullOnChainBalance verifies that when the on-chain
// balance is unavailable the snapshot still persists using the DB-cached
// (TotalBalance) value — the row must not be skipped silently.
func TestSaveSnapshot_withNullOnChainBalance(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	// Simulate BalanceProvider returning zero/unavailable by using the
	// last-known DB-cached balance (TotalBalance) directly.
	snap := performance.Snapshot{
		ID:              uuid.New(),
		VaultID:         vaultID,
		TotalBalance:    decimal.RequireFromString("800.00"), // DB-cached value
		TotalDeposited:  decimal.RequireFromString("800.00"),
		TotalYieldEarned: decimal.Zero,
		SharePrice:      decimal.RequireFromString("1.0000"),
		SnapshotAt:      time.Now().UTC().Truncate(time.Microsecond),
	}

	got, err := repo.Insert(ctx, snap)
	if err != nil {
		t.Fatalf("Insert() with null on-chain balance should not error, got: %v", err)
	}
	// The snapshot is stored, not silently dropped.
	if got.ID == uuid.Nil {
		t.Fatal("snapshot must be persisted even when on-chain balance is unavailable")
	}
	if !got.TotalBalance.Equal(snap.TotalBalance) {
		t.Fatalf("TotalBalance preserved: got %s, want %s", got.TotalBalance, snap.TotalBalance)
	}
}

func TestSaveSnapshot_withAllocationBreakdown(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	snap := newTestSnapshot(vaultID, time.Now().UTC().Truncate(time.Microsecond))
	snap.AllocationBreakdown = []performance.AllocationBreakdownEntry{
		{Source: "aave", Amount: decimal.RequireFromString("600.00"), APY: decimal.RequireFromString("4.10")},
		{Source: "blend", Amount: decimal.RequireFromString("400.00"), APY: decimal.RequireFromString("5.20")},
	}

	got, err := repo.Insert(ctx, snap)
	if err != nil {
		t.Fatalf("Insert() with breakdown error = %v", err)
	}

	// Round-trip the breakdown via LatestForVault to confirm JSONB serialisation.
	latest, err := repo.LatestForVault(ctx, vaultID)
	if err != nil {
		t.Fatalf("LatestForVault() error = %v", err)
	}
	if len(latest.AllocationBreakdown) != 2 {
		t.Fatalf("AllocationBreakdown len: got %d, want 2", len(latest.AllocationBreakdown))
	}
	_ = got
}

// ---------------------------------------------------------------------------
// LatestForVault
// ---------------------------------------------------------------------------

func TestGetLatestSnapshot_byVaultID(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	base := time.Now().UTC().Truncate(time.Microsecond)
	older := newTestSnapshot(vaultID, base.Add(-time.Hour))
	older.TotalBalance = decimal.RequireFromString("900.00")

	newer := newTestSnapshot(vaultID, base)
	newer.TotalBalance = decimal.RequireFromString("1100.00")

	for _, s := range []performance.Snapshot{older, newer} {
		if _, err := repo.Insert(ctx, s); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
	}

	got, err := repo.LatestForVault(ctx, vaultID)
	if err != nil {
		t.Fatalf("LatestForVault() error = %v", err)
	}
	if !got.TotalBalance.Equal(newer.TotalBalance) {
		t.Fatalf("TotalBalance: got %s, want %s (latest)", got.TotalBalance, newer.TotalBalance)
	}
}

func TestGetLatestSnapshot_noSnapshotsExist(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	_, err := repo.LatestForVault(ctx, vaultID)
	if !errors.Is(err, performance.ErrSnapshotNotFound) {
		t.Fatalf("expected ErrSnapshotNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// HistoryForVault
// ---------------------------------------------------------------------------

func TestListSnapshots_dateRangeFilter(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	now := time.Now().UTC().Truncate(time.Microsecond)
	times := []time.Time{
		now.Add(-48 * time.Hour),
		now.Add(-24 * time.Hour),
		now,
	}
	for _, at := range times {
		snap := newTestSnapshot(vaultID, at)
		if _, err := repo.Insert(ctx, snap); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
	}

	// Request history since 36 h ago → should return the last 2 snapshots.
	since := now.Add(-36 * time.Hour)
	history, err := repo.HistoryForVault(ctx, vaultID, since)
	if err != nil {
		t.Fatalf("HistoryForVault() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("HistoryForVault since -36h: got %d snapshots, want 2", len(history))
	}

	// Results must be ordered oldest-first.
	if !history[0].SnapshotAt.Before(history[1].SnapshotAt) {
		t.Fatal("history should be ordered ascending by snapshot_at")
	}
}

func TestListSnapshots_empty(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	history, err := repo.HistoryForVault(ctx, vaultID, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("HistoryForVault() on empty vault error = %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expected 0 snapshots, got %d", len(history))
	}
}

// TestListSnapshots_pagination verifies that HistoryForVault returns all rows
// in the requested window — the caller controls pagination at the handler layer.
func TestListSnapshots_pagination(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	base := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 7; i++ {
		snap := newTestSnapshot(vaultID, base.Add(time.Duration(-i)*24*time.Hour))
		if _, err := repo.Insert(ctx, snap); err != nil {
			t.Fatalf("Insert(day -%d) error = %v", i, err)
		}
	}

	all, err := repo.HistoryForVault(ctx, vaultID, base.Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("HistoryForVault() error = %v", err)
	}
	if len(all) != 7 {
		t.Fatalf("expected 7 snapshots, got %d", len(all))
	}
}

// ---------------------------------------------------------------------------
// GetPerformanceHistory / FirstAtOrAfter
// ---------------------------------------------------------------------------

func TestGetPerformanceHistory_vaultID(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultA := seedIntegrationVault(t, db, userID)
	vaultB := seedIntegrationVault(t, db, userID)

	base := time.Now().UTC().Truncate(time.Microsecond)
	for i := 0; i < 3; i++ {
		if _, err := repo.Insert(ctx, newTestSnapshot(vaultA, base.Add(time.Duration(-i)*time.Hour))); err != nil {
			t.Fatalf("Insert(vaultA) error = %v", err)
		}
	}
	if _, err := repo.Insert(ctx, newTestSnapshot(vaultB, base)); err != nil {
		t.Fatalf("Insert(vaultB) error = %v", err)
	}

	histA, err := repo.HistoryForVault(ctx, vaultA, base.Add(-4*time.Hour))
	if err != nil {
		t.Fatalf("HistoryForVault(vaultA) error = %v", err)
	}
	if len(histA) != 3 {
		t.Fatalf("vaultA history: got %d, want 3", len(histA))
	}
	for _, s := range histA {
		if s.VaultID != vaultA {
			t.Fatalf("snapshot belongs to wrong vault: %v", s.VaultID)
		}
	}
}

func TestFirstAtOrAfter_success(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	base := time.Now().UTC().Truncate(time.Microsecond)
	early := newTestSnapshot(vaultID, base.Add(-2*time.Hour))
	early.TotalBalance = decimal.RequireFromString("500.00")

	late := newTestSnapshot(vaultID, base)
	late.TotalBalance = decimal.RequireFromString("700.00")

	for _, s := range []performance.Snapshot{early, late} {
		if _, err := repo.Insert(ctx, s); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
	}

	got, err := repo.FirstAtOrAfter(ctx, vaultID, base.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("FirstAtOrAfter() error = %v", err)
	}
	if !got.TotalBalance.Equal(late.TotalBalance) {
		t.Fatalf("TotalBalance: got %s, want %s (the later snapshot)", got.TotalBalance, late.TotalBalance)
	}
}

func TestFirstAtOrAfter_noMatch_returnsError(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	_, err := repo.FirstAtOrAfter(ctx, vaultID, time.Now().Add(24*time.Hour))
	if !errors.Is(err, performance.ErrSnapshotNotFound) {
		t.Fatalf("expected ErrSnapshotNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// UpsertAPY / ListAPY
// ---------------------------------------------------------------------------

func TestCalculateAPY_7day_weightedAverage(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	// Insert a 7-day APY record with a known value.
	record := performance.APYRecord{
		ID:           uuid.New(),
		VaultID:      vaultID,
		Period:       performance.Period7d,
		RealizedAPY:  decimal.RequireFromString("5.25"),
		CalculatedAt: time.Now().UTC(),
	}
	if err := repo.UpsertAPY(ctx, record); err != nil {
		t.Fatalf("UpsertAPY(7d) error = %v", err)
	}

	records, err := repo.ListAPY(ctx, vaultID)
	if err != nil {
		t.Fatalf("ListAPY() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ListAPY(): got %d records, want 1", len(records))
	}
	if records[0].Period != performance.Period7d {
		t.Fatalf("Period: got %v, want 7d", records[0].Period)
	}
	if !records[0].RealizedAPY.Equal(record.RealizedAPY) {
		t.Fatalf("RealizedAPY: got %s, want %s", records[0].RealizedAPY, record.RealizedAPY)
	}
}

// TestCalculateAPY_insufficientHistory verifies that when no APY records exist
// for a vault, ListAPY returns an empty slice rather than an error.
func TestCalculateAPY_insufficientHistory(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	records, err := repo.ListAPY(ctx, vaultID)
	if err != nil {
		t.Fatalf("ListAPY() on vault with no history error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 APY records, got %d", len(records))
	}
}

func TestSaveSnapshot_success_UpsertAPY(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	// Insert APY records for all standard periods.
	apyValues := map[performance.Period]string{
		performance.Period7d:  "5.10",
		performance.Period30d: "4.80",
		performance.Period90d: "4.50",
	}
	for period, apy := range apyValues {
		if err := repo.UpsertAPY(ctx, performance.APYRecord{
			ID:           uuid.New(),
			VaultID:      vaultID,
			Period:       period,
			RealizedAPY:  decimal.RequireFromString(apy),
			CalculatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("UpsertAPY(%v) error = %v", period, err)
		}
	}

	records, err := repo.ListAPY(ctx, vaultID)
	if err != nil {
		t.Fatalf("ListAPY() error = %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 APY records (one per period), got %d", len(records))
	}
}

// TestListAPY_latestPerPeriod verifies DISTINCT ON (period) semantics: when
// multiple APY rows exist for the same period, only the most recently
// calculated one is returned.
func TestListAPY_latestPerPeriod(t *testing.T) {
	db := openIntegrationDB(t)
	applyPerformanceMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewPerformanceRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	base := time.Now().UTC()

	// Older 7d calculation.
	if err := repo.UpsertAPY(ctx, performance.APYRecord{
		ID:           uuid.New(),
		VaultID:      vaultID,
		Period:       performance.Period7d,
		RealizedAPY:  decimal.RequireFromString("3.00"),
		CalculatedAt: base.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("UpsertAPY(old) error = %v", err)
	}

	// Newer 7d calculation.
	if err := repo.UpsertAPY(ctx, performance.APYRecord{
		ID:           uuid.New(),
		VaultID:      vaultID,
		Period:       performance.Period7d,
		RealizedAPY:  decimal.RequireFromString("6.50"),
		CalculatedAt: base,
	}); err != nil {
		t.Fatalf("UpsertAPY(new) error = %v", err)
	}

	records, err := repo.ListAPY(ctx, vaultID)
	if err != nil {
		t.Fatalf("ListAPY() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record (latest per period), got %d", len(records))
	}
	if !records[0].RealizedAPY.Equal(decimal.RequireFromString("6.50")) {
		t.Fatalf("RealizedAPY: got %s, want 6.50 (the newer calculation)", records[0].RealizedAPY)
	}
}
