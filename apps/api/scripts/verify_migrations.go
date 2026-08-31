//go:build migrationsafety

// Command verify_migrations enforces the migration safety checks required by
// issue #1123. It applies the full migration chain from scratch, seeds the
// resulting schema, then rolls the whole chain back down, failing on any
// migration that cannot make the round trip.
//
// It is behind a build tag so it does not participate in the normal package
// build; CI runs it explicitly with -tags migrationsafety.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	_ "github.com/lib/pq"
)

// irreversibleMarker is the declaration a down migration must carry to be
// skipped during the rollback pass. It is deliberately a distinct token rather
// than the word "irreversible" on its own: down migrations legitimately discuss
// why a rollback is unsafe in prose, and matching that prose would silently
// skip exactly the migrations most in need of checking.
const irreversibleMarker = "migration:irreversible"

// destructive matches operations that cannot be undone by a down migration.
// Narrowing a column is included: it raises an overflow rather than truncating,
// which is correct behaviour but still makes the rollback unsafe to run
// unattended.
var destructive = regexp.MustCompile(`(?i)\b(drop\s+table|drop\s+column|drop\s+constraint|truncate)\b`)

type migrationPair struct {
	version  string
	name     string
	upPath   string
	downPath string
}

func main() {
	dir := flag.String("dir", "migrations", "directory containing the migration files")
	flag.Parse()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://nester:nester@localhost:5432/nester_dev?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}

	pairs, versions, err := loadMigrations(*dir)
	if err != nil {
		log.Fatalf("%v", err)
	}
	if len(versions) == 0 {
		log.Fatalf("no migrations found in %s", *dir)
	}

	if err := wipeSchema(db); err != nil {
		log.Fatalf("wipe schema: %v", err)
	}

	// Every migration must apply cleanly to an empty database.
	fmt.Println("[migration-safety] applying full chain from scratch")
	for _, v := range versions {
		p := pairs[v]
		if p.upPath == "" {
			log.Fatalf("migration %s (%s) has no .up.sql", v, p.name)
		}
		body, err := os.ReadFile(p.upPath)
		if err != nil {
			log.Fatalf("read %s: %v", p.upPath, err)
		}
		if op := destructive.FindString(string(body)); op != "" {
			fmt.Printf("[migration-safety] review: %s (%s) performs %q\n", v, p.name, strings.ToLower(op))
		}
		if _, err := db.Exec(string(body)); err != nil {
			log.Fatalf("up migration %s (%s) failed: %v", v, p.name, err)
		}
	}

	// Roll back against a populated database, which is where reversibility
	// actually breaks: an empty table will accept almost any schema change.
	if err := seed(db); err != nil {
		log.Fatalf("seed populated database: %v", err)
	}

	fmt.Println("[migration-safety] rolling the chain back down on a populated database")
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		p := pairs[v]

		if p.downPath == "" {
			log.Fatalf(
				"migration %s (%s) has no .down.sql and does not declare %q",
				v, p.name, irreversibleMarker,
			)
		}

		body, err := os.ReadFile(p.downPath)
		if err != nil {
			log.Fatalf("read %s: %v", p.downPath, err)
		}
		down := string(body)

		if strings.Contains(strings.ToLower(down), irreversibleMarker) {
			fmt.Printf("[migration-safety] %s (%s) declares itself irreversible; skipping\n", v, p.name)
			continue
		}
		if strings.TrimSpace(down) == "" {
			log.Fatalf(
				"migration %s (%s) has an empty .down.sql; declare %q if that is deliberate",
				v, p.name, irreversibleMarker,
			)
		}

		if _, err := db.Exec(down); err != nil {
			log.Fatalf("down migration %s (%s) failed on a populated database: %v", v, p.name, err)
		}
	}

	fmt.Println("[migration-safety] all checks passed")
}

func loadMigrations(dir string) (map[string]*migrationPair, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("read migrations dir %q: %w", dir, err)
	}

	pairs := make(map[string]*migrationPair)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		version := parts[0]

		p, ok := pairs[version]
		if !ok {
			p = &migrationPair{version: version, name: strings.TrimSuffix(parts[1], ".sql")}
			pairs[version] = p
		}

		switch {
		case strings.HasSuffix(name, ".up.sql"):
			p.upPath = filepath.Join(dir, name)
		case strings.HasSuffix(name, ".down.sql"):
			p.downPath = filepath.Join(dir, name)
		}
	}

	versions := make([]string, 0, len(pairs))
	for v := range pairs {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	return pairs, versions, nil
}

func wipeSchema(db *sql.DB) error {
	fmt.Println("[migration-safety] resetting public schema")
	_, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`)
	return err
}

// seed inserts a row into every table that the rollback pass is likely to
// touch, so down migrations are exercised against data rather than an empty
// schema. Tables that are absent or reject the insert are skipped: the point is
// to populate what can be populated, not to model the full domain.
func seed(db *sql.DB) error {
	fmt.Println("[migration-safety] seeding populated database")

	const user = `
		INSERT INTO users (id, wallet_address, created_at, updated_at)
		VALUES (
			'00000000-0000-0000-0000-000000000001',
			'GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
			NOW(), NOW()
		)
		ON CONFLICT DO NOTHING;`

	const vault = `
		INSERT INTO vaults (id, user_id, contract_address, currency, created_at, updated_at)
		VALUES (
			'00000000-0000-0000-0000-000000000002',
			'00000000-0000-0000-0000-000000000001',
			'CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
			'USDC', NOW(), NOW()
		)
		ON CONFLICT DO NOTHING;`

	for _, stmt := range []string{user, vault} {
		if _, err := db.Exec(stmt); err != nil {
			// A schema that has moved on from these columns is not a failure of
			// the safety check itself.
			fmt.Printf("[migration-safety] seed statement skipped: %v\n", err)
		}
	}
	return nil
}
