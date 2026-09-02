package reconciliation

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

// Acceptance test for nester#1082, criterion 4: a deliberately corrupted
// vault row must be detected as a divergence — recorded in the audit tables
// with both values, alerted, counted on the metric — and NEVER corrected.
//
// The helpers are package-local copies of the repository/postgres harness
// (that package's helpers are unexported; precedent for the copy is
// internal/scheduler/leadership_integration_test.go).

func openReconciliationTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func resetReconciliationTables(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`TRUNCATE TABLE reconciliation_findings, reconciliation_runs, reconciliation_checkpoints, allocations, vault_transactions, yield_harvests, vaults, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}
}

func seedReconciliationUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, wallet_address, display_name) VALUES ($1, $2, $3)`,
		userID.String(),
		"G"+userID.String(), // final schema: wallet_address NOT NULL UNIQUE
		"Reconciliation User",
	); err != nil {
		t.Fatalf("seed user failed: %v", err)
	}
	return userID
}

// seedActiveVault inserts an active vault holding balance (raw stroops, the
// unit the event indexer stores — migration 103).
func seedActiveVault(t *testing.T, db *sql.DB, userID uuid.UUID, contractAddress, balance string) uuid.UUID {
	t.Helper()

	vaultID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO vaults (id, user_id, contract_address, currency, status, total_deposited, current_balance)
		 VALUES ($1, $2, $3, 'USDC', 'active', $4::numeric, $4::numeric)`,
		vaultID.String(), userID.String(), contractAddress, balance,
	); err != nil {
		t.Fatalf("seed vault failed: %v", err)
	}
	return vaultID
}

// stubChain returns fixed authoritative balances per contract address,
// standing in for ContractReader.TotalAssetsStroops.
type stubChain struct {
	balances map[string]decimal.Decimal
}

func (s stubChain) TotalAssets(_ context.Context, contractAddress string) (decimal.Decimal, error) {
	return s.balances[contractAddress], nil
}

// newIntegrationRunner wires the runner exactly as main.go does: real audit
// repository, real vault lister, stroop-denominated classifier thresholds.
func newIntegrationRunner(db *sql.DB, chain VaultBalanceReader, dryRun bool, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	comparator := BalanceComparator{
		Vaults: postgres.NewVaultRepository(db),
		Chain:  chain,
		Classifier: Classifier{
			DustTolerance:     decimal.RequireFromString("0.5"),
			WarningThreshold:  decimal.NewFromInt(1),
			CriticalThreshold: decimal.NewFromInt(10_000_000),
		},
		Logger: logger,
	}
	return NewRunner(
		RunnerConfig{Enabled: true, DryRun: dryRun},
		NewPostgresRepository(db),
		[]Comparator{comparator},
		NewLogAlerter(logger),
		logger,
	)
}

func TestReconciliationIntegrationDetectsCorruptedRow(t *testing.T) {
	db := openReconciliationTestDB(t)
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "migrations"))
	resetReconciliationTables(t, db)

	userID := seedReconciliationUser(t, db)

	// A healthy vault: database and chain agree exactly.
	seedActiveVault(t, db, userID, "CRECON-OK", "38250000000")
	// The corrupted row: the database claims 1e7 stroops (1 USDC) more than
	// the chain holds — as if the indexer misapplied a withdrawal.
	corruptedID := seedActiveVault(t, db, userID, "CRECON-BAD", "38250000000")

	chain := stubChain{balances: map[string]decimal.Decimal{
		"CRECON-OK":  decimal.RequireFromString("38250000000"),
		"CRECON-BAD": decimal.RequireFromString("38240000000"),
	}}

	rec := &fakeRunnerMetrics{}
	runner := newIntegrationRunner(db, chain, false, nil)
	runner.SetMetrics(rec)

	runner.Tick(context.Background())

	// The run row: completed, checked both vaults, found exactly one
	// critical divergence.
	var status string
	var checked, findings, critical int
	if err := db.QueryRow(
		`SELECT status, checked_count, finding_count, critical_count
		 FROM reconciliation_runs WHERE comparator = 'vault_balance'`,
	).Scan(&status, &checked, &findings, &critical); err != nil {
		t.Fatalf("read run row: %v", err)
	}
	if status != "completed" || checked != 2 || findings != 1 || critical != 1 {
		t.Fatalf("run = status %q checked %d findings %d critical %d, want completed/2/1/1",
			status, checked, findings, critical)
	}

	// The finding row: the corrupted vault, both values, the exact
	// difference, open for review.
	var entityID, findingType, severity, resolution string
	var recorded, onChain, difference string
	if err := db.QueryRow(
		`SELECT entity_id, type, severity, resolution_state,
		        recorded_value::text, on_chain_value::text, difference::text
		 FROM reconciliation_findings`,
	).Scan(&entityID, &findingType, &severity, &resolution, &recorded, &onChain, &difference); err != nil {
		t.Fatalf("read finding row: %v", err)
	}
	if entityID != corruptedID.String() {
		t.Fatalf("finding entity = %s, want the corrupted vault %s", entityID, corruptedID)
	}
	if findingType != "mismatch" || severity != "critical" || resolution != "open" {
		t.Fatalf("finding = type %q severity %q resolution %q, want mismatch/critical/open",
			findingType, severity, resolution)
	}
	if !decimal.RequireFromString(recorded).Equal(decimal.RequireFromString("38250000000")) {
		t.Fatalf("recorded_value = %s, want 38250000000", recorded)
	}
	if !decimal.RequireFromString(onChain).Equal(decimal.RequireFromString("38240000000")) {
		t.Fatalf("on_chain_value = %s, want 38240000000", onChain)
	}
	if !decimal.RequireFromString(difference).Equal(decimal.RequireFromString("10000000")) {
		t.Fatalf("difference = %s, want 10000000", difference)
	}

	// Never silently auto-corrected: the corrupted balance is untouched.
	var balanceAfter string
	if err := db.QueryRow(
		`SELECT current_balance::text FROM vaults WHERE id = $1`, corruptedID.String(),
	).Scan(&balanceAfter); err != nil {
		t.Fatalf("re-read corrupted vault: %v", err)
	}
	if !decimal.RequireFromString(balanceAfter).Equal(decimal.RequireFromString("38250000000")) {
		t.Fatalf("corrupted balance was modified to %s — reconciliation must never correct", balanceAfter)
	}

	// The divergence reached the metric exactly once, and the pass completed.
	if len(rec.divergences) != 1 || rec.divergences[0] != "mismatch" {
		t.Fatalf("divergence metrics = %v, want [mismatch]", rec.divergences)
	}
	if len(rec.runs) != 1 || string(rec.runs[0]) != "completed" {
		t.Fatalf("run metrics = %v, want [completed]", rec.runs)
	}
}

func TestReconciliationIntegrationDryRunWritesNothing(t *testing.T) {
	db := openReconciliationTestDB(t)
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "migrations"))
	resetReconciliationTables(t, db)

	userID := seedReconciliationUser(t, db)
	corruptedID := seedActiveVault(t, db, userID, "CRECON-DRY", "5000000000")

	chain := stubChain{balances: map[string]decimal.Decimal{
		"CRECON-DRY": decimal.RequireFromString("4000000000"),
	}}

	rec := &fakeRunnerMetrics{}
	var logs []string
	runner := newIntegrationRunner(db, chain, true, slog.New(captureHandler{records: &logs}))
	runner.SetMetrics(rec)

	runner.Tick(context.Background())

	// Positive evidence first: the dry run must have actually compared and
	// found the corruption — otherwise "wrote nothing" would also pass for a
	// dry-run mode that silently stopped comparing at all.
	var foundDivergenceLog bool
	for _, entry := range logs {
		if strings.Contains(entry, "dry-run: divergence found") &&
			strings.Contains(entry, "recorded_value=5000000000") &&
			strings.Contains(entry, "on_chain_value=4000000000") {
			foundDivergenceLog = true
		}
	}
	if !foundDivergenceLog {
		t.Fatalf("dry run did not log the divergence with both values; logs: %v", logs)
	}

	var runRows, findingRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reconciliation_runs`).Scan(&runRows); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM reconciliation_findings`).Scan(&findingRows); err != nil {
		t.Fatalf("count findings: %v", err)
	}
	if runRows != 0 || findingRows != 0 {
		t.Fatalf("dry run wrote runs=%d findings=%d, want 0/0", runRows, findingRows)
	}

	var balanceAfter string
	if err := db.QueryRow(
		`SELECT current_balance::text FROM vaults WHERE id = $1`, corruptedID.String(),
	).Scan(&balanceAfter); err != nil {
		t.Fatalf("re-read vault: %v", err)
	}
	if !decimal.RequireFromString(balanceAfter).Equal(decimal.RequireFromString("5000000000")) {
		t.Fatalf("dry run modified the balance to %s", balanceAfter)
	}

	if len(rec.divergences) != 0 {
		t.Fatalf("dry run emitted divergence metrics: %v", rec.divergences)
	}
	// Liveness still counts: a dry-running reconciler is alive.
	if len(rec.runs) != 1 || string(rec.runs[0]) != "completed" {
		t.Fatalf("run metrics = %v, want [completed]", rec.runs)
	}
}
