// Package testutil provides shared helpers for database-backed integration
// tests.
//
// It exists because every integration test previously carried its own
// hand-curated list of migration files, and those lists rotted: whenever a
// migration was added, renamed, or corrected, tests that were not running in
// CI (they gate on TEST_DATABASE_DSN, which CI did not set) silently drifted
// until the whole suite failed on a fresh database. Applying the complete
// chain in numeric order — exactly what golang-migrate does in production —
// makes the schema under test the schema the code actually runs against, and
// removes the lists that rotted.
package testutil

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ApplyAllMigrations wipes every table in the public schema and applies every
// up-migration in migrationsDir in ascending numeric order.
//
// The wipe makes the helper idempotent across test functions and across runs
// against a persistent database: several migrations create tables without
// IF NOT EXISTS guards, so re-applying onto leftovers from a prior run would
// fail. Only tables are dropped because the migration set creates only
// tables, indexes, and constraints — no types, functions, triggers, or
// extensions (verified against the full set; revisit if that changes).
//
// The full chain is applied rather than a per-test subset deliberately. The
// numeric order is the production order, and the resulting schema is the one
// production code targets — including later migrations that drop or rename
// columns earlier ones created. Seed helpers must therefore insert the FINAL
// shape of each table, not the shape it had when some early migration
// created it.
func ApplyAllMigrations(t *testing.T, db *sql.DB, migrationsDir string) {
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

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations dir %q: %v", migrationsDir, err)
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	// Lexicographic order equals numeric order here: every file carries a
	// fixed-width zero-padded prefix (001_..., 023_..., 100_...).
	sort.Strings(names)

	if len(names) == 0 {
		t.Fatalf("no up-migrations found in %q", migrationsDir)
	}

	for _, name := range names {
		contents, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			t.Fatalf("read migration %q: %v", name, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q failed: %v", name, err)
		}
	}
}
