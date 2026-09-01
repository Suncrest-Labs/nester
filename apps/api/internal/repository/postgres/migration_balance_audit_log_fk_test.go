package postgres

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestMigration118_DownThenUp_RestoresBalanceAuditLogFKs guards against a
// CodeRabbit-flagged migration-safety regression: 118's down migration drops
// balance_audit_log's FKs to vaults and users (deliberately, so a full-chain
// rollback isn't blocked — see the comment in 118's down.sql) but does not
// drop the table itself. Its up migration declares those FKs only inline in
// `CREATE TABLE IF NOT EXISTS balance_audit_log`, which is a no-op once the
// table already exists — so, without an idempotent re-creation step in the
// up migration, a down-then-up cycle used to leave balance_audit_log
// permanently unconstrained, silently accepting rows referencing
// nonexistent vaults/users.
//
// This exercises exactly that cycle directly against migration 118's own
// up/down files (rather than through verify_migrations.go, which only runs
// a single up-then-down pass over the full chain and would not have caught
// this).
func TestMigration118_DownThenUp_RestoresBalanceAuditLogFKs(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db) // full chain, including 118, from scratch

	migrationsDir := filepath.Join("..", "..", "..", "migrations")

	assertBothFKsExist(t, db, "before down-migrating 118")

	downSQL := readMigrationFile(t, migrationsDir, "118_create_balance_audit_log.down.sql")
	if _, err := db.Exec(downSQL); err != nil {
		t.Fatalf("apply 118 down migration: %v", err)
	}
	assertNeitherFKExists(t, db, "after down-migrating 118")

	upSQL := readMigrationFile(t, migrationsDir, "118_create_balance_audit_log.up.sql")
	if _, err := db.Exec(upSQL); err != nil {
		t.Fatalf("re-apply 118 up migration: %v", err)
	}
	assertBothFKsExist(t, db, "after re-applying 118 up migration")
}

func readMigrationFile(t *testing.T, dir, name string) string {
	t.Helper()
	// #nosec G304 -- test-only helper reading a fixed migration filename from
	// this repository's own migrations directory.
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(body)
}

func assertBothFKsExist(t *testing.T, db *sql.DB, when string) {
	t.Helper()
	if !fkExists(t, db, "balance_audit_log_vault_id_fkey") {
		t.Fatalf("balance_audit_log_vault_id_fkey missing %s", when)
	}
	if !fkExists(t, db, "balance_audit_log_user_id_fkey") {
		t.Fatalf("balance_audit_log_user_id_fkey missing %s", when)
	}
}

func assertNeitherFKExists(t *testing.T, db *sql.DB, when string) {
	t.Helper()
	if fkExists(t, db, "balance_audit_log_vault_id_fkey") {
		t.Fatalf("balance_audit_log_vault_id_fkey unexpectedly present %s", when)
	}
	if fkExists(t, db, "balance_audit_log_user_id_fkey") {
		t.Fatalf("balance_audit_log_user_id_fkey unexpectedly present %s", when)
	}
}

func fkExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = $1)`, name).Scan(&exists); err != nil {
		t.Fatalf("check constraint %q: %v", name, err)
	}
	return exists
}
