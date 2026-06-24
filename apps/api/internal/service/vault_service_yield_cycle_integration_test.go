//go:build integration
// +build integration

package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	performancedomain "github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	performancesvc "github.com/suncrestlabs/nester/apps/api/internal/service/performance"
)

// TestVaultServiceFullYieldCycleIntegration proves that deposit, performance
// snapshots, harvest and APY-history all agree end-to-end against a real
// PostgreSQL instance.
//
// Run with:
//
//	go test -tags integration ./apps/api/internal/service/... \
//	  -run TestVaultServiceFullYieldCycleIntegration
//
// Requires TEST_DATABASE_DSN pointing at an empty test database. All DDL is
// applied inline and tables are TRUNCATEd at the top of each run.
func TestVaultServiceFullYieldCycleIntegration(t *testing.T) {
	db := openYieldCycleDB(t)
	applyYieldCycleMigrations(t, db)
	resetYieldCycleTables(t, db)

	ctx := context.Background()
	userID := seedYieldCycleUser(t, db)

	vaultRepo := postgres.NewVaultRepository(db)
	perfRepo := postgres.NewPerformanceRepository(db)
	vaultSvc := NewVaultService(vaultRepo)
	// Noop invoker keeps the test off any Soroban contract call.
	vaultSvc.SetDepositInvoker(NoopVaultDepositInvoker{})
	perfSvc := performancesvc.NewService(perfRepo, vaultRepo)

	// ── 1. Setup: create vault ──────────────────────────────────────────────
	vault, err := vaultSvc.CreateVault(ctx, CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CA-YIELD-CYCLE-001",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}
	if vault.Status != "active" {
		t.Fatalf("vault status: got %q, want %q", vault.Status, "active")
	}

	// ── 2. Deposit 1000 USDC ─────────────────────────────────────────────────
	if _, err := vaultSvc.RecordDeposit(ctx, RecordDepositInput{
		VaultID: vault.ID,
		Amount:  decimal.RequireFromString("1000"),
	}); err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	afterDeposit, err := vaultSvc.GetVault(ctx, vault.ID)
	if err != nil {
		t.Fatalf("GetVault after deposit error = %v", err)
	}
	if !afterDeposit.CurrentBalance.Equal(decimal.RequireFromString("1000")) {
		t.Fatalf("current_balance: got %s, want 1000", afterDeposit.CurrentBalance)
	}
	if !afterDeposit.TotalDeposited.Equal(decimal.RequireFromString("1000")) {
		t.Fatalf("total_deposited: got %s, want 1000", afterDeposit.TotalDeposited)
	}

	// ── 3. Yield accrual: 30 daily snapshots (8% APY target) ────────────────
	// Total accrued over 30 days on 1000 USDC at 8% APR ≈ 6 USDC; bumped to a
	// clean 6.60 USDC so the gross/fees/net numerics are easy to read.
	const seededTotalYield = "6.60000000"
	const dailyInterval = 24 * time.Hour

	deposited := decimal.RequireFromString("1000")
	totalYield := decimal.RequireFromString(seededTotalYield)
	dailyYield := totalYield.Div(decimal.NewFromInt(30)).Round(8)

	// Inject a single fixed clock value into performancesvc.Service so the
	// APYHistoryForVault rolling 30-day window (`since = clock() - 30*24h`)
	// aligns exactly with the snapshot timestamps we insert below. Without
	// this, sub-second drift between time.Now() calls produces 30 stored rows
	// but only 29 daily buckets (date_trunc('day', snapshot_at) collapses a
	// trailing edge case into the prior bucket). With SetClock in place the
	// test is deterministic across held CI runners, suspended VMs, and
	// future additions of work between insert and the APY query.
	testClock := time.Now().UTC().Truncate(time.Microsecond)
	perfSvc.SetClock(func() time.Time { return testClock })
	startAt := testClock.Add(-30 * dailyInterval)

	for day := 0; day < 30; day++ {
		balance := deposited.Add(dailyYield.Mul(decimal.NewFromInt(int64(day) + 1)))
		snapAt := startAt.Add(time.Duration(day) * dailyInterval).Truncate(time.Microsecond)
		snap := performancedomain.Snapshot{
			ID:               uuid.New(),
			VaultID:          vault.ID,
			TotalBalance:     balance,
			TotalDeposited:   deposited,
			TotalYieldEarned: balance.Sub(deposited),
			SharePrice:       balance.Div(deposited).Round(8),
			SnapshotAt:       snapAt,
			AllocationBreakdown: []performancedomain.AllocationBreakdownEntry{
				{Source: "aave", Amount: decimal.RequireFromString("400"), APY: decimal.RequireFromString("4.00")},
				{Source: "blend", Amount: decimal.RequireFromString("600"), APY: decimal.RequireFromString("5.00")},
			},
		}
		if _, err := perfRepo.Insert(ctx, snap); err != nil {
			t.Fatalf("Insert snapshot day %d error = %v", day, err)
		}
	}

	// Pre-assertion: 30 snapshot rows persisted. See the SetClock block above
	// for why this count is non-trivial under clock drift.
	var snapshotCount int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM vault_performance_snapshots WHERE vault_id = $1`,
		vault.ID,
	).Scan(&snapshotCount); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if snapshotCount != 30 {
		t.Fatalf("stored snapshots: got %d, want 30", snapshotCount)
	}

	// Promote the accrued yield to the vault row so HarvestVault picks it up.
	if _, err := db.ExecContext(ctx,
		`UPDATE vaults
		   SET yield_earned = $1,
		       current_balance = $2
		   WHERE id = $3`,
		totalYield.String(),
		deposited.Add(totalYield).String(),
		vault.ID.String(),
	); err != nil {
		t.Fatalf("promote yield_earned: %v", err)
	}

	// ── 4. Harvest (no-op on-chain invoker) ─────────────────────────────────
	compound := false
	// WalletAddress is required by VaultService.HarvestVault whenever a
	// DepositInvoker (including Noop) is wired — the value is passed to the
	// invoker. The Noop invoker ignores it but VaultService still enforces its
	// presence as part of its safety check.
	result, err := vaultSvc.HarvestVault(ctx, HarvestVaultInput{
		VaultID:       vault.ID,
		UserID:        userID,
		WalletAddress: "GWALLET-NOOP-TEST-001",
		Compound:      &compound,
	})
	if err != nil {
		t.Fatalf("HarvestVault() error = %v", err)
	}

	gross := mustDecimal(t, result.GrossYieldUSDC)
	expectedFee := totalYield.Mul(decimal.RequireFromString("0.1")).Round(6)
	expectedNet := totalYield.Sub(expectedFee)
	actualFee := mustDecimal(t, result.PerformanceFeeUSDC)
	actualNet := mustDecimal(t, result.NetYieldUSDC)

	if !gross.Equal(totalYield) {
		t.Fatalf("GrossYieldUSDC: got %s, want %s", result.GrossYieldUSDC, totalYield)
	}
	if !actualFee.Equal(expectedFee) {
		t.Fatalf("PerformanceFeeUSDC: got %s, want %s", result.PerformanceFeeUSDC, expectedFee)
	}
	if !actualNet.Equal(expectedNet) {
		t.Fatalf("NetYieldUSDC: got %s, want %s", result.NetYieldUSDC, expectedNet)
	}
	if result.Compounded {
		t.Fatal("expected compounded=false")
	}
	if result.TxHash != "" {
		t.Fatalf("expected empty tx_hash (noop invoker), got %q", result.TxHash)
	}

	// yield_earned resets to zero; fees_paid grows by the performance fee.
	afterHarvest, err := vaultSvc.GetVault(ctx, vault.ID)
	if err != nil {
		t.Fatalf("GetVault after harvest error = %v", err)
	}
	if !afterHarvest.YieldEarned.IsZero() {
		t.Fatalf("yield_earned after harvest: got %s, want 0", afterHarvest.YieldEarned)
	}
	if !afterHarvest.FeesPaid.Equal(expectedFee) {
		t.Fatalf("fees_paid: got %s, want %s", afterHarvest.FeesPaid, expectedFee)
	}

	// Exactly one 'harvest' ledger row is logged.
	transactions, err := vaultRepo.ListUserVaultTransactions(ctx, userID, vault.ID)
	if err != nil {
		t.Fatalf("ListUserVaultTransactions error = %v", err)
	}
	harvestCount := 0
	for _, txn := range transactions {
		if txn.Type == "harvest" {
			harvestCount++
		}
	}
	if harvestCount != 1 {
		t.Fatalf("expected exactly 1 harvest transaction, got %d", harvestCount)
	}

	// ── 5. APY History: 30 daily buckets ────────────────────────────────────
	hist, err := perfSvc.GetAPYHistory(ctx, vault.ID, "30d", "daily")
	if err != nil {
		t.Fatalf("GetAPYHistory() error = %v", err)
	}
	if len(hist.DataPoints) != 30 {
		t.Fatalf("expected 30 daily data points, got %d", len(hist.DataPoints))
	}

	var sum float64
	for _, dp := range hist.DataPoints {
		v, parseErr := strconv.ParseFloat(dp.APY, 64)
		if parseErr != nil {
			t.Fatalf("parse APY %q: %v", dp.APY, parseErr)
		}
		sum += v
	}
	mean := sum / float64(len(hist.DataPoints))

	// With linear accrual the per-day cumulative return spans 0.022 → 0.66,
	// averaging ≈ 0.34 (≈ half the final cumulative return). Allow loose
	// bounds so date_trunc edge-cases don't cause flake.
	const meanMin = 0.20
	const meanMax = 0.50
	if mean < meanMin || mean > meanMax {
		t.Fatalf("APY history mean %.4f outside [%v, %v]", mean, meanMin, meanMax)
	}

	// Data points are chronologically ordered.
	for i := 1; i < len(hist.DataPoints); i++ {
		if hist.DataPoints[i-1].Date > hist.DataPoints[i].Date {
			t.Fatalf("data points not ordered chronologically: %s > %s",
				hist.DataPoints[i-1].Date, hist.DataPoints[i].Date)
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func openYieldCycleDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set; integration tests require a real PostgreSQL instance")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func applyYieldCycleMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	// Migration ordering notes (also required for production):
	//  - 008 creates vault_transactions with the `tx_hash` column.
	//  - 033 renames `tx_hash` → `transaction_hash` and adds the fee columns.
	//  - 023 builds a UNIQUE INDEX on `transaction_hash`, so it MUST run after
	//    033. Numeric ordering (008→023→033) fails on a fresh DB because 023
	//    would reference a column that does not exist yet. We pin 033 before
	//    023 here to keep the integration test self-contained on a fresh DB.
	//  - 035 is intentionally skipped (byte-identical duplicate of 033 which
	//    would re-run the non-idempotent RENAME COLUMN and fail).
	//  - 036 widens vault_transactions.type CHECK to allow 'harvest' rows
	//    produced by RecordHarvest.
	for _, name := range []string{
		"001_create_users_table.up.sql",
		"002_create_vaults_table.up.sql",
		"005_create_allocations_table.up.sql",
		"006_create_settlements_table.up.sql",
		"007_add_vault_deleted_at.up.sql",
		"008_add_vault_transactions.up.sql",
		"014_add_missing_columns.up.sql",
		"016_add_indices_and_constraints.up.sql",
		"018_create_vault_performance.up.sql",
		// 033 BEFORE 023 — see ordering note above.
		"033_update_vault_transactions.up.sql",
		"023_vault_transactions_hash_unique.up.sql",
		"036_allow_harvest_transaction_type.up.sql",
	} {
		// Test file lives at apps/api/internal/service/ → migrations are two
		// directories up at apps/api/migrations.
		path := filepath.Join("..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("apply migration %q: %v", name, err)
		}
	}
}

func resetYieldCycleTables(t *testing.T, db *sql.DB) {
	t.Helper()
	const stmt = `TRUNCATE TABLE
		apy_history,
		vault_performance_snapshots,
		settlements,
		vault_transactions,
		allocations,
		vaults,
		users
		RESTART IDENTITY CASCADE`
	if _, err := db.Exec(stmt); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}
}

func seedYieldCycleUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(),
		userID.String()+"@example.com",
		"Yield Cycle Integration User",
	); err != nil {
		t.Fatalf("seed user failed: %v", err)
	}
	return userID
}

func mustDecimal(t *testing.T, raw string) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(raw)
	if err != nil {
		t.Fatalf("parse decimal %q: %v", raw, err)
	}
	return d
}
