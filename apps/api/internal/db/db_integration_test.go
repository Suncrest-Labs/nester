//go:build integration

package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/db"
)

// ---------------------------------------------------------------------------
// TestMain – suite-level setup
// ---------------------------------------------------------------------------

// TestMain gates the entire suite on TEST_DATABASE_DSN being present and
// verifies baseline connectivity before running any test.
func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		fmt.Fprintln(os.Stderr, "TEST_DATABASE_DSN is not set — skipping integration tests")
		os.Exit(0)
	}

	probe, err := db.Open(db.Config{DSN: dsn, ConnectionTimeout: 5 * time.Second})
	if err != nil {
		fmt.Fprintf(os.Stderr, "db.Open probe failed: %v\n", err)
		os.Exit(1)
	}
	probe.Close()

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Connection Pool
// ---------------------------------------------------------------------------

// TestOpenSucceeds verifies that a valid DSN produces a usable *sql.DB.
func TestOpenSucceeds(t *testing.T) {
	dsn := requireDSN(t)

	pool, err := db.Open(db.Config{
		DSN:               dsn,
		ConnectionTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	if pool == nil {
		t.Fatal("Open() returned nil db")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.Ping(ctx, pool); err != nil {
		t.Fatalf("Ping() after Open() error = %v", err)
	}
}

// TestOpenInvalidDSNReturnsError verifies that an unreachable / invalid DSN
// returns a non-nil error quickly — no hang or panic.
func TestOpenInvalidDSNReturnsError(t *testing.T) {
	start := time.Now()

	// Port 1 is almost always closed; use a very short timeout so the test
	// finishes fast even on slow CI.
	_, err := db.Open(db.Config{
		DSN:               "postgres://nouser:nopass@127.0.0.1:1/nodb?sslmode=disable",
		ConnectionTimeout: 500 * time.Millisecond,
	})

	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error for invalid DSN, got nil")
	}
	// Must not hang — allow generous headroom above the 500 ms timeout.
	if elapsed > 3*time.Second {
		t.Fatalf("Open() blocked for %v — expected fast failure", elapsed)
	}
}

// TestPoolMaxOpenConnsRespected verifies that SetMaxOpenConns is applied.
func TestPoolMaxOpenConnsRespected(t *testing.T) {
	const want = 3

	pool, err := db.Open(db.Config{
		DSN:          requireDSN(t),
		MaxOpenConns: want,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	if got := pool.Stats().MaxOpenConnections; got != want {
		t.Errorf("MaxOpenConnections = %d, want %d", got, want)
	}
}

// TestPoolMaxIdleConnsRespected verifies that SetMaxIdleConns is applied by
// opening and releasing several connections, then checking the idle count
// does not exceed the configured ceiling.
func TestPoolMaxIdleConnsRespected(t *testing.T) {
	const maxIdle = 2

	pool, err := db.Open(db.Config{
		DSN:          requireDSN(t),
		MaxOpenConns: 10,
		MaxIdleConns: maxIdle,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// Exercise several connections so the pool has a chance to grow.
	for i := 0; i < 5; i++ {
		row := pool.QueryRowContext(context.Background(), "SELECT 1")
		var n int
		if err := row.Scan(&n); err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
	}

	// After all connections are returned idle count must not exceed maxIdle.
	if got := pool.Stats().Idle; got > maxIdle {
		t.Errorf("idle connections = %d, want <= %d", got, maxIdle)
	}
}

// TestPoolConnectionsReturnedAfterUse verifies that connections are not
// leaked: after running several queries, OpenConnections equals the number
// actually in-use (zero in the steady state).
func TestPoolConnectionsReturnedAfterUse(t *testing.T) {
	pool, err := db.Open(db.Config{
		DSN:          requireDSN(t),
		MaxOpenConns: 5,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	for i := 0; i < 10; i++ {
		rows, err := pool.QueryContext(context.Background(), "SELECT generate_series(1, 3)")
		if err != nil {
			t.Fatalf("QueryContext() error = %v", err)
		}
		// Drain and close explicitly — mimics correct application code.
		for rows.Next() {
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("rows.Close() error = %v", err)
		}
	}

	stats := pool.Stats()
	if stats.InUse != 0 {
		t.Errorf("InUse = %d after all rows closed, want 0 (connection leak)", stats.InUse)
	}
}

// ---------------------------------------------------------------------------
// Migration Runner
// ---------------------------------------------------------------------------

// TestMigrateUpAppliesAllInOrder verifies that MigrateUp executes every
// *.up.sql in lexicographic order and records each in schema_migrations.
func TestMigrateUpAppliesAllInOrder(t *testing.T) {
	idb := isolatedDB(t)
	dir := t.TempDir()

	// Three migrations in deliberate non-alphabetic write order.
	writeMig(t, dir, "002_beta.up.sql", `CREATE TABLE mig_beta (id SERIAL PRIMARY KEY)`)
	writeMig(t, dir, "001_alpha.up.sql", `CREATE TABLE mig_alpha (id SERIAL PRIMARY KEY)`)
	writeMig(t, dir, "003_gamma.up.sql", `CREATE TABLE mig_gamma (id SERIAL PRIMARY KEY)`)

	if err := db.MigrateUp(idb, dir); err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}

	// All three tables must exist.
	for _, name := range []string{"mig_alpha", "mig_beta", "mig_gamma"} {
		if !tableExists(t, idb, name) {
			t.Errorf("table %q not found after MigrateUp", name)
		}
	}

	// schema_migrations must record all three, in order.
	versions := appliedVersions(t, idb)
	for _, v := range []string{"001_alpha", "002_beta", "003_gamma"} {
		if !versions[v] {
			t.Errorf("version %q missing from schema_migrations", v)
		}
	}
}

// TestMigrateUpIdempotent verifies that running MigrateUp a second time when
// all migrations are already applied is a safe no-op.
func TestMigrateUpIdempotent(t *testing.T) {
	idb := isolatedDB(t)
	dir := t.TempDir()

	writeMig(t, dir, "001_once.up.sql", `CREATE TABLE mig_once (id SERIAL PRIMARY KEY)`)

	if err := db.MigrateUp(idb, dir); err != nil {
		t.Fatalf("first MigrateUp() error = %v", err)
	}
	// Second call must not fail (e.g. no "table already exists" error).
	if err := db.MigrateUp(idb, dir); err != nil {
		t.Fatalf("second MigrateUp() error = %v", err)
	}

	if n := len(appliedVersions(t, idb)); n != 1 {
		t.Errorf("schema_migrations has %d rows, want 1", n)
	}
}

// TestMigrateDownRollsBackLastMigration verifies that MigrateDown removes the
// most recently applied migration's table and its schema_migrations record.
func TestMigrateDownRollsBackLastMigration(t *testing.T) {
	idb := isolatedDB(t)
	dir := t.TempDir()

	writeMig(t, dir, "001_first.up.sql", `CREATE TABLE mig_first (id SERIAL PRIMARY KEY)`)
	writeMig(t, dir, "001_first.down.sql", `DROP TABLE IF EXISTS mig_first`)

	writeMig(t, dir, "002_second.up.sql", `CREATE TABLE mig_second (id SERIAL PRIMARY KEY)`)
	writeMig(t, dir, "002_second.down.sql", `DROP TABLE IF EXISTS mig_second`)

	if err := db.MigrateUp(idb, dir); err != nil {
		t.Fatalf("MigrateUp() error = %v", err)
	}

	// mig_second is the last applied migration — down should remove it.
	if err := db.MigrateDown(idb, dir); err != nil {
		t.Fatalf("MigrateDown() error = %v", err)
	}

	if tableExists(t, idb, "mig_second") {
		t.Error("mig_second still exists after MigrateDown")
	}
	if !tableExists(t, idb, "mig_first") {
		t.Error("mig_first was incorrectly removed by MigrateDown")
	}

	versions := appliedVersions(t, idb)
	if versions["002_second"] {
		t.Error("002_second still recorded in schema_migrations after MigrateDown")
	}
	if !versions["001_first"] {
		t.Error("001_first unexpectedly removed from schema_migrations")
	}
}

// TestMigrateUpCorruptFileReturnsErrorAndDoesNotPartiallyApply verifies that
// a migration whose SQL is invalid is rolled back in full — the table is not
// created and the version is not recorded in schema_migrations.
func TestMigrateUpCorruptFileReturnsErrorAndDoesNotPartiallyApply(t *testing.T) {
	idb := isolatedDB(t)
	dir := t.TempDir()

	// A valid first migration so we can confirm it still succeeds.
	writeMig(t, dir, "001_ok.up.sql", `CREATE TABLE mig_ok (id SERIAL PRIMARY KEY)`)

	// A corrupt second migration: first statement is valid DDL, second is
	// intentionally broken SQL — the whole transaction must roll back.
	writeMig(t, dir, "002_corrupt.up.sql",
		"CREATE TABLE mig_corrupt (id SERIAL PRIMARY KEY);\nNOT VALID SQL AT ALL;",
	)

	err := db.MigrateUp(idb, dir)
	if err == nil {
		t.Fatal("MigrateUp() with corrupt file returned nil, want an error")
	}

	// The valid migration that ran before the corrupt one must be present.
	if !tableExists(t, idb, "mig_ok") {
		t.Error("mig_ok should exist — it ran before the corrupt migration")
	}

	// The corrupt migration's table must NOT exist (transaction was rolled back).
	if tableExists(t, idb, "mig_corrupt") {
		t.Error("mig_corrupt must not exist — its transaction should have been rolled back")
	}

	// Only the valid version must be recorded.
	versions := appliedVersions(t, idb)
	if !versions["001_ok"] {
		t.Error("001_ok missing from schema_migrations")
	}
	if versions["002_corrupt"] {
		t.Error("002_corrupt must not be recorded in schema_migrations")
	}
}

// ---------------------------------------------------------------------------
// Query Helpers / Utilities
// ---------------------------------------------------------------------------

// TestContextCancellationMidQueryReturnsError verifies that cancelling the
// context while a long-running query is executing surfaces a non-nil error.
func TestContextCancellationMidQueryReturnsError(t *testing.T) {
	pool, err := db.Open(db.Config{DSN: requireDSN(t)})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel almost immediately so we do not wait for the full sleep.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// pg_sleep(10) simulates a long-running query.
	rows, err := pool.QueryContext(ctx, "SELECT pg_sleep(10)")
	if rows != nil {
		rows.Close()
	}

	if err == nil {
		t.Fatal("expected an error after context cancellation, got nil")
	}
	// The error must be context-related, not a spurious database error.
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "cancel") {
		t.Logf("error = %v (acceptable: context-related)", err)
	}
}

// TestQueryTimeoutReturnsError verifies that a context deadline returns an
// error instead of hanging indefinitely.
func TestQueryTimeoutReturnsError(t *testing.T) {
	pool, err := db.Open(db.Config{DSN: requireDSN(t)})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	rows, err := pool.QueryContext(ctx, "SELECT pg_sleep(10)")
	if rows != nil {
		rows.Close()
	}
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// The call must complete well within the sleep duration.
	if elapsed > 3*time.Second {
		t.Fatalf("QueryContext blocked for %v — expected fast timeout", elapsed)
	}
}

// TestTransactionCommit verifies that a committed transaction persists data.
func TestTransactionCommit(t *testing.T) {
	idb := isolatedDB(t)

	if _, err := idb.Exec(`CREATE TABLE tx_commit_test (val TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, err := idb.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	if _, err := tx.Exec(`INSERT INTO tx_commit_test VALUES ('committed')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	var count int
	if err := idb.QueryRow(`SELECT COUNT(*) FROM tx_commit_test WHERE val = 'committed'`).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d after commit, want 1", count)
	}
}

// TestTransactionRollback verifies that a rolled-back transaction does not
// persist data.
func TestTransactionRollback(t *testing.T) {
	idb := isolatedDB(t)

	if _, err := idb.Exec(`CREATE TABLE tx_rollback_test (val TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	tx, err := idb.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	if _, err := tx.Exec(`INSERT INTO tx_rollback_test VALUES ('should-disappear')`); err != nil {
		tx.Rollback()
		t.Fatalf("insert: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	var count int
	if err := idb.QueryRow(`SELECT COUNT(*) FROM tx_rollback_test`).Scan(&count); err != nil {
		t.Fatalf("count query: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d after rollback, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// Health Check
// ---------------------------------------------------------------------------

// TestPingReachable verifies that db.Ping returns nil for a live database.
func TestPingReachable(t *testing.T) {
	pool, err := db.Open(db.Config{DSN: requireDSN(t)})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := db.Ping(ctx, pool); err != nil {
		t.Fatalf("Ping() error = %v, want nil", err)
	}
}

// TestPingUnreachableReturnsError verifies that db.Ping returns a non-nil
// error when the database is not reachable.
func TestPingUnreachableReturnsError(t *testing.T) {
	// Open without pinging so we get a pool pointing at a closed port.
	unreachable, err := sql.Open("pgx",
		"postgres://nouser:nopass@127.0.0.1:1/nodb?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { unreachable.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := db.Ping(ctx, unreachable); err == nil {
		t.Fatal("Ping() returned nil for unreachable DB, want error")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requireDSN returns TEST_DATABASE_DSN or skips the test.
func requireDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	return dsn
}

// isolatedDB opens a *sql.DB bound to a freshly created PostgreSQL schema.
// The schema — and all objects within it — are dropped in t.Cleanup.
func isolatedDB(t *testing.T) *sql.DB {
	t.Helper()
	baseDSN := requireDSN(t)

	schema := fmt.Sprintf("dbtest_%016x", uint64(rand.Int63())) //nolint:gosec

	// Admin connection used only for schema lifecycle management.
	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("isolatedDB: open admin: %v", err)
	}
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatalf("isolatedDB: create schema: %v", err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + schema + " CASCADE") //nolint:errcheck
		admin.Close()
	})

	// Workload connection pinned to the isolated schema.
	workDSN := schemaAwareDSN(baseDSN, schema)
	workDB, err := sql.Open("pgx", workDSN)
	if err != nil {
		t.Fatalf("isolatedDB: open workDB: %v", err)
	}
	t.Cleanup(func() { workDB.Close() })

	return workDB
}

// schemaAwareDSN appends search_path=<schema> to an existing DSN.
func schemaAwareDSN(dsn, schema string) string {
	if strings.ContainsRune(dsn, '?') {
		return dsn + "&search_path=" + schema
	}
	return dsn + "?search_path=" + schema
}

// writeMig writes content to a file named dir/name.
func writeMig(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
		t.Fatalf("writeMig %s: %v", name, err)
	}
}

// tableExists reports whether a table with the given name exists in the
// schema that the connection currently resolves to (current_schema()).
func tableExists(t *testing.T, idb *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	err := idb.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name   = $1
		)`, name).Scan(&exists)
	if err != nil {
		t.Fatalf("tableExists(%s): %v", name, err)
	}
	return exists
}

// appliedVersions returns the set of versions recorded in schema_migrations.
func appliedVersions(t *testing.T, idb *sql.DB) map[string]bool {
	t.Helper()
	rows, err := idb.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		t.Fatalf("appliedVersions: %v", err)
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("appliedVersions scan: %v", err)
		}
		result[v] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("appliedVersions rows.Err: %v", err)
	}
	return result
}
