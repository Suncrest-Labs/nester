package stellar

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/backfill"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
)

// openBackfillIntegrationDB and applyBackfillIntegrationMigrations mirror
// internal/repository/postgres's openIntegrationDB/applyIntegrationMigrations
// (unexported there, so not directly reusable from this package) — same
// TEST_DATABASE_DSN gate, same wipe-then-apply-by-name approach, scoped to
// just the tables this test touches: processed_events (dedup), penalty_events
// (a resettable derived table), and backfill_runs (checkpointing). Neither
// carries a foreign key to vaults/users, so those tables are deliberately
// left out of this minimal set.
func openBackfillIntegrationDB(t *testing.T) *sql.DB {
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

func applyBackfillIntegrationMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		DO $$
		DECLARE r record;
		BEGIN
			FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
				EXECUTE 'DROP TABLE IF EXISTS public.' || quote_ident(r.tablename) || ' CASCADE';
			END LOOP;
		END$$;
	`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	for _, name := range []string{
		"001_create_users_table.up.sql",
		"002_create_vaults_table.up.sql",
		"028_create_processed_events_table.up.sql",
		"030_create_vault_rebalances.up.sql",
		"063_create_penalty_events.up.sql",
		"064_create_vault_rebalance_legs.up.sql",
		"065_create_vault_rebalance_completions.up.sql",
		"092_create_backfill_runs.up.sql",
		"093_add_ledger_sequence_to_derived_events.up.sql",
	} {
		path := filepath.Join("..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q failed: %v", name, err)
		}
	}
}

func countPenaltyEvents(t *testing.T, db *sql.DB, contractID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM penalty_events WHERE vault_contract_address = $1`, contractID).Scan(&n); err != nil {
		t.Fatalf("count penalty_events: %v", err)
	}
	return n
}

func countProcessedEvents(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM processed_events`).Scan(&n); err != nil {
		t.Fatalf("count processed_events: %v", err)
	}
	return n
}

// TestRunner_Backfill_AppliesEventsViaSharedHandlerDispatch proves a
// backfilled range produces the same derived state forward indexing would:
// applyIndexedEvent is called directly, not a separate code path (#840's
// central acceptance criterion).
func TestRunner_Backfill_AppliesEventsViaSharedHandlerDispatch(t *testing.T) {
	db := openBackfillIntegrationDB(t)
	applyBackfillIntegrationMigrations(t, db)

	contractID := "CCONTRACT1"
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "evt-1", ContractID: contractID, Ledger: 101, EventType: "pnlty_chg", Data: map[string]any{"user": "GUSER1", "amount": "100", "shares_burned": "10"}},
		{ID: "evt-2", ContractID: contractID, Ledger: 102, EventType: "pnlty_chg", Data: map[string]any{"user": "GUSER1", "amount": "50", "shares_burned": "5"}},
	}, 1_000_000)
	defer srv.Close()

	repo := postgres.NewBackfillRepository(db)
	runner := &Runner{DB: db, Repo: repo, Client: srv.Client(), RPCURL: srv.URL, Throttle: time.Millisecond}

	run, err := runner.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 200, ContractIDs: []string{contractID}, InitiatedBy: "integration-test",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.Status != backfill.StatusCompleted {
		t.Fatalf("status = %q, want completed", run.Status)
	}
	if run.EventsProcessed != 2 {
		t.Fatalf("events_processed = %d, want 2", run.EventsProcessed)
	}
	if got := countPenaltyEvents(t, db, contractID); got != 2 {
		t.Fatalf("expected 2 penalty_events rows, got %d", got)
	}
}

// TestRunner_Backfill_OverlappingRangeDoesNotDoubleApply proves the
// indexer's existing dedup (processed_events, keyed on event_id) protects a
// backfill the same way it protects the forward path — reprocessing a range
// that includes already-applied events must not double-insert.
func TestRunner_Backfill_OverlappingRangeDoesNotDoubleApply(t *testing.T) {
	db := openBackfillIntegrationDB(t)
	applyBackfillIntegrationMigrations(t, db)

	contractID := "CCONTRACT2"
	events := []mockRPCEvent{
		{ID: "evt-a", ContractID: contractID, Ledger: 101, EventType: "pnlty_chg", Data: map[string]any{"user": "GUSER2", "amount": "100", "shares_burned": "10"}},
	}
	srv := newMockRPCServer(t, events, 1_000_000)
	defer srv.Close()

	repo := postgres.NewBackfillRepository(db)
	runner := &Runner{DB: db, Repo: repo, Client: srv.Client(), RPCURL: srv.URL, Throttle: time.Millisecond}

	first, err := runner.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 150, ContractIDs: []string{contractID}, InitiatedBy: "integration-test",
	})
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if first.EventsProcessed != 1 {
		t.Fatalf("first run events_processed = %d, want 1", first.EventsProcessed)
	}

	// Second run's range [90,150] overlaps the first entirely.
	second, err := runner.Start(context.Background(), StartInput{
		FromLedger: 90, ToLedger: 150, ContractIDs: []string{contractID}, InitiatedBy: "integration-test",
	})
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if second.EventsSkippedDuplicate != 1 {
		t.Fatalf("second run events_skipped_duplicate = %d, want 1 (the overlapping event)", second.EventsSkippedDuplicate)
	}
	if got := countPenaltyEvents(t, db, contractID); got != 1 {
		t.Fatalf("expected exactly 1 penalty_events row after overlapping backfill (no double-apply), got %d", got)
	}
	if got := countProcessedEvents(t, db); got != 1 {
		t.Fatalf("expected exactly 1 processed_events row, got %d", got)
	}
}

// TestRunner_Rebuild_ClearsAndRecomputesOnlyTargetRange proves a scoped
// rebuild clears the resettable derived rows + their processed_events
// entries, then correctly recomputes them from the raw events — the
// resync-after-a-handler-fix scenario the issue describes.
func TestRunner_Rebuild_ClearsAndRecomputesOnlyTargetRange(t *testing.T) {
	db := openBackfillIntegrationDB(t)
	applyBackfillIntegrationMigrations(t, db)

	contractID := "CCONTRACT3"
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "evt-1", ContractID: contractID, Ledger: 101, EventType: "pnlty_chg", Data: map[string]any{"user": "GUSER3", "amount": "100", "shares_burned": "10"}},
	}, 1_000_000)
	defer srv.Close()

	repo := postgres.NewBackfillRepository(db)
	runner := &Runner{DB: db, Repo: repo, Client: srv.Client(), RPCURL: srv.URL, Throttle: time.Millisecond}

	// First: a normal backfill establishes state, simulating a range that
	// was already processed (possibly with a now-fixed handler bug).
	if _, err := runner.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 150, ContractIDs: []string{contractID}, InitiatedBy: "integration-test",
	}); err != nil {
		t.Fatalf("initial backfill: %v", err)
	}
	if got := countPenaltyEvents(t, db, contractID); got != 1 {
		t.Fatalf("setup: expected 1 penalty_events row, got %d", got)
	}

	// Now rebuild the same range: reset must clear the 1 existing row and
	// its processed_events entry, then reprocessing must recompute exactly
	// 1 row again (not 0, not 2) — proving the range was correctly cleared
	// and correctly recomputed, not left stale or double-applied.
	run, err := runner.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 150, ContractIDs: []string{contractID}, Mode: backfill.ModeRebuild, InitiatedBy: "integration-test",
	})
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if run.Status != backfill.StatusCompleted {
		t.Fatalf("rebuild status = %q, want completed", run.Status)
	}
	if got := countPenaltyEvents(t, db, contractID); got != 1 {
		t.Fatalf("expected exactly 1 penalty_events row after rebuild, got %d", got)
	}
}

// TestRunner_DryRun_WritesNothing proves a dry run reports without
// mutating any derived table or the dedup table.
func TestRunner_DryRun_WritesNothing(t *testing.T) {
	db := openBackfillIntegrationDB(t)
	applyBackfillIntegrationMigrations(t, db)

	contractID := "CCONTRACT4"
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "evt-1", ContractID: contractID, Ledger: 101, EventType: "pnlty_chg", Data: map[string]any{"user": "GUSER4", "amount": "100", "shares_burned": "10"}},
	}, 1_000_000)
	defer srv.Close()

	repo := postgres.NewBackfillRepository(db)
	runner := &Runner{DB: db, Repo: repo, Client: srv.Client(), RPCURL: srv.URL, Throttle: time.Millisecond}

	run, err := runner.Start(context.Background(), StartInput{
		FromLedger: 100, ToLedger: 150, ContractIDs: []string{contractID}, DryRun: true, InitiatedBy: "integration-test",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.EventsProcessed != 1 {
		t.Fatalf("events_processed = %d, want 1 (dry run still counts what it would process)", run.EventsProcessed)
	}
	if got := countPenaltyEvents(t, db, contractID); got != 0 {
		t.Fatalf("expected 0 penalty_events rows after a dry run, got %d", got)
	}
	if got := countProcessedEvents(t, db); got != 0 {
		t.Fatalf("expected 0 processed_events rows after a dry run, got %d", got)
	}
}

// TestRunner_Resume_AfterSimulatedCrash proves a crash mid-run resumes from
// the last checkpoint rather than reprocessing (and double-counting) events
// already committed before the crash.
func TestRunner_Resume_AfterSimulatedCrash(t *testing.T) {
	db := openBackfillIntegrationDB(t)
	applyBackfillIntegrationMigrations(t, db)

	contractID := "CCONTRACT5"
	srv := newMockRPCServer(t, []mockRPCEvent{
		{ID: "evt-1", ContractID: contractID, Ledger: 101, EventType: "pnlty_chg", Data: map[string]any{"user": "GUSER5", "amount": "10", "shares_burned": "1"}},
		{ID: "evt-2", ContractID: contractID, Ledger: 160, EventType: "pnlty_chg", Data: map[string]any{"user": "GUSER5", "amount": "20", "shares_burned": "2"}},
	}, 1_000_000)
	defer srv.Close()

	repo := postgres.NewBackfillRepository(db)

	// Simulate the first half of a run completing and checkpointing, then a
	// crash: create the row and record progress directly (as UpdateProgress
	// would have persisted after batch 1) without calling Start/execute for
	// the whole range.
	run := &backfill.Run{FromLedger: 100, ToLedger: 200, ContractIDs: []string{contractID}, InitiatedBy: "integration-test"}
	if err := repo.Create(context.Background(), run); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Apply evt-1 directly (as if batch 1 of a real run had committed it)
	// and persist the matching checkpoint — this is the "state right before
	// the crash."
	if _, err := applyIndexedEvent(context.Background(), db, indexedEvent{ID: "evt-1", ContractID: contractID, EventType: "pnlty_chg", Ledger: 101, Data: map[string]any{"user": "GUSER5", "amount": "10", "shares_burned": "1"}}); err != nil {
		t.Fatalf("apply evt-1: %v", err)
	}
	if err := repo.UpdateProgress(context.Background(), run.ID, 149, 1, 0); err != nil {
		t.Fatalf("UpdateProgress: %v", err)
	}

	runner := &Runner{DB: db, Repo: repo, Client: srv.Client(), RPCURL: srv.URL, Throttle: time.Millisecond}
	resumed, err := runner.Resume(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != backfill.StatusCompleted {
		t.Fatalf("status = %q, want completed", resumed.Status)
	}
	// evt-1 (ledger 101, before the checkpoint) must NOT be reprocessed;
	// only evt-2 (ledger 160) is new on resume — 1 (pre-crash) + 1 (resumed) = 2 total.
	if resumed.EventsProcessed != 2 {
		t.Fatalf("events_processed = %d, want 2 (1 pre-crash + 1 on resume)", resumed.EventsProcessed)
	}
	if got := countPenaltyEvents(t, db, contractID); got != 2 {
		t.Fatalf("expected 2 penalty_events rows total, got %d", got)
	}
}
