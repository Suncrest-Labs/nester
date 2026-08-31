package postgres_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

// seedPath is scripts/seed.sql, relative to this package. Five levels up:
// postgres -> repository -> internal -> api -> apps -> repo root.
func seedPath() string {
	return filepath.Join("..", "..", "..", "..", "..", "scripts", "seed.sql")
}

// TestSeedAppliesToAFullyMigratedDatabase is the gate #1122 asks for: apply
// every migration to an empty database, then apply the seed, and fail on any
// error.
//
// The failure this catches is seed.sql drifting from the schema — referencing
// a column a later migration renamed or dropped. Nothing else notices, because
// the API never reads seed.sql; the first person to hit it is a new
// contributor whose dev environment will not come up.
func TestSeedAppliesToAFullyMigratedDatabase(t *testing.T) {
	db := openSeedTestDB(t)
	resetPublicSchema(t, db)

	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "..", "migrations"))

	seed, err := os.ReadFile(seedPath())
	if err != nil {
		t.Fatalf("read seed.sql: %v", err)
	}
	if _, err := db.Exec(string(seed)); err != nil {
		t.Fatalf("seed.sql failed against a fully migrated database: %v", err)
	}
}

// The seed must produce a known fixture set, not merely run without error: a
// seed that silently inserted nothing would pass the test above while leaving
// a new contributor with an empty app.
func TestSeedProducesKnownFixtures(t *testing.T) {
	db := openSeedTestDB(t)
	resetPublicSchema(t, db)
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "..", "migrations"))
	applySeed(t, db)

	for _, tc := range []struct {
		table   string
		atLeast int
	}{
		{"users", 1},
		{"vaults", 1},
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tc.table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", tc.table, err)
		}
		if count < tc.atLeast {
			t.Errorf("%s has %d rows, want at least %d", tc.table, count, tc.atLeast)
		}
	}
}

// Re-running the seed must not fail. The documented reset command re-applies
// it, and a contributor who runs it twice should not have to drop the database
// to recover.
func TestSeedIsRerunnable(t *testing.T) {
	db := openSeedTestDB(t)
	resetPublicSchema(t, db)
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "..", "migrations"))

	applySeed(t, db)
	applySeed(t, db)

	var users int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users == 0 {
		t.Fatal("re-running the seed left no users")
	}
}

func applySeed(t *testing.T, db *sql.DB) {
	t.Helper()
	seed, err := os.ReadFile(seedPath())
	if err != nil {
		t.Fatalf("read seed.sql: %v", err)
	}
	if _, err := db.Exec(string(seed)); err != nil {
		t.Fatalf("apply seed.sql: %v", err)
	}
}

func openSeedTestDB(t *testing.T) *sql.DB {
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

// resetPublicSchema drops everything, so the migration chain runs against a
// genuinely empty database. Applying migrations over tables another test left
// behind would not prove the seed works from scratch, which is the claim.
func resetPublicSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
}
