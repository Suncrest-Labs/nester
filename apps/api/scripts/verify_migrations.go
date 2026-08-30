package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://nester:nester@localhost:5432/nester_dev?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("database ping failed: %v", err)
	}

	migrationsDir := "apps/api/migrations"
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		log.Fatalf("read migrations dir %q: %v", migrationsDir, err)
	}

	type migrationPair struct {
		version string
		name    string
		upPath  string
		downPath string
	}

	pairsMap := make(map[string]*migrationPair)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue
		}
		version := parts[0]

		pair, ok := pairsMap[version]
		if !ok {
			pair = &migrationPair{version: version, name: parts[1]}
			pairsMap[version] = pair
		}

		if strings.HasSuffix(name, ".up.sql") {
			pair.upPath = filepath.Join(migrationsDir, name)
		} else if strings.HasSuffix(name, ".down.sql") {
			pair.downPath = filepath.Join(migrationsDir, name)
		}
	}

	var versions []string
	for v := range pairsMap {
		versions = append(versions, v)
	}

sort.Strings(versions)

	// 1. Wipe database
	fmt.Println("[Migration Safety] Wiping public schema...")
	if _, err := db.Exec(`
		DO $$
		DECLARE r record;
		BEGIN
			FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
				EXECUTE 'DROP TABLE IF EXISTS public.' || quote_ident(r.tablename) || ' CASCADE';
			END LOOP;
		END$$;
	`); err != nil {
		log.Fatalf("drop tables failed: %v", err)
	}

	// 2. Apply all up migrations
	fmt.Println("[Migration Safety] Applying all up migrations...")
	for _, v := range versions {
		pair := pairsMap[v]
		contents, err := os.ReadFile(pair.upPath)
		if err != nil {
			log.Fatalf("read up migration %s: %v", pair.upPath, err)
		}

		// Check for destructive operations flagged for review (e.g. DROP TABLE, DROP COLUMN)
		lower := strings.ToLower(string(contents))
		if strings.Contains(lower, "drop table") || strings.Contains(lower, "drop column") {
			fmt.Printf("[Migration Safety] WARNING: Destructive operation detected in migration %s (%s)\n", v, pair.name)
		}

		if _, err := db.Exec(string(contents)); err != nil {
			log.Fatalf("applying up migration %s (%s) failed: %v", v, pair.name, err)
		}
	}

	// 3. Seed test data into the fully migrated schema
	fmt.Println("[Migration Safety] Seeding test data into populated database...")
	seedSQL := `
		INSERT INTO users (id, wallet_address, display_name, kyc_status, tier, risk_profile, savings_goal, onboarding_completed, created_at, updated_at)
		VALUES ('00000000-0000-0000-0000-000000000001', 'GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA', 'Test User', 'verified', 'standard', 'conservative', 'Emergency Fund', true, NOW(), NOW())
		ON CONFLICT DO NOTHING;
	`
	// Only execute seed if users table exists
	var tableExists bool
	err = db.QueryRow("SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'users')").Scan(&tableExists)
	if err == nil && tableExists {
		if _, err := db.Exec(seedSQL); err != nil {
			log.Printf("[Migration Safety] Notice: seed execution skipped or partially applied: %v", err)
		}
	}

	// 4. Apply all down migrations in reverse order
	fmt.Println("[Migration Safety] Rolling back all down migrations on populated database...")
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		pair := pairsMap[v]

		if pair.downPath == "" {
			log.Fatalf("[Migration Safety] ERROR: Migration %s (%s) is missing a .down.sql file and has not declared itself explicitly irreversible.", v, pair.name)
		}

		downContentsBytes, err := os.ReadFile(pair.downPath)
		if err != nil {
			log.Fatalf("read down migration %s: %v", pair.downPath, err)
		}
		downContents := string(downContentsBytes)

		// Check if explicitly declared irreversible
		if strings.Contains(strings.ToLower(downContents), "irreversible") || strings.TrimSpace(downContents) == "" {
			fmt.Printf("[Migration Safety] Migration %s (%s) is explicitly irreversible or a no-op down.\n", v, pair.name)
			continue
		}

		if _, err := db.Exec(downContents); err != nil {
			log.Fatalf("[Migration Safety] ERROR: Rolling back migration %s (%s) on populated database failed: %v", v, pair.name, err)
		}
	}

	fmt.Println("[Migration Safety] All migration safety checks passed successfully!")
}
