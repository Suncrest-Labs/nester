package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT        PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

// MigrateUp reads all *.up.sql files from dir, sorts them lexicographically,
// and applies any that are not yet recorded in schema_migrations.
//
// Each migration runs inside its own transaction: if the SQL fails the
// transaction is rolled back and the version is never recorded, so the
// database remains consistent.  Returns on the first error.
func MigrateUp(db *sql.DB, dir string) error {
	if _, err := db.Exec(createMigrationsTable); err != nil {
		return fmt.Errorf("migrate up: ensure schema_migrations: %w", err)
	}

	files, err := upFiles(dir)
	if err != nil {
		return err
	}

	applied, err := appliedVersions(db)
	if err != nil {
		return err
	}

	for _, file := range files {
		version := versionOf(file)
		if applied[version] {
			continue
		}

		content, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			return fmt.Errorf("migrate up: read %s: %w", file, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("migrate up: begin tx (%s): %w", file, err)
		}

		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate up: apply %s: %w", file, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version) VALUES ($1)`,
			version,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate up: record %s: %w", file, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrate up: commit %s: %w", file, err)
		}
	}

	return nil
}

// MigrateDown rolls back the single most-recently applied migration by
// executing its *.down.sql counterpart and removing its record from
// schema_migrations.  If no migrations have been applied it is a no-op.
func MigrateDown(db *sql.DB, dir string) error {
	if _, err := db.Exec(createMigrationsTable); err != nil {
		return fmt.Errorf("migrate down: ensure schema_migrations: %w", err)
	}

	var version string
	err := db.QueryRow(
		`SELECT version FROM schema_migrations ORDER BY applied_at DESC, version DESC LIMIT 1`,
	).Scan(&version)
	if err == sql.ErrNoRows {
		return nil // nothing applied yet
	}
	if err != nil {
		return fmt.Errorf("migrate down: query last version: %w", err)
	}

	downFile := version + ".down.sql"
	content, err := os.ReadFile(filepath.Join(dir, downFile))
	if err != nil {
		return fmt.Errorf("migrate down: read %s: %w", downFile, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migrate down: begin tx (%s): %w", version, err)
	}

	if _, err := tx.Exec(string(content)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migrate down: apply %s: %w", downFile, err)
	}

	if _, err := tx.Exec(
		`DELETE FROM schema_migrations WHERE version = $1`,
		version,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("migrate down: remove record %s: %w", version, err)
	}

	return tx.Commit()
}

// upFiles returns all *.up.sql filenames in dir, sorted lexicographically.
func upFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("migrate: read dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}

	sort.Strings(files)
	return files, nil
}

// appliedVersions returns a set of all version strings recorded in
// schema_migrations.
func appliedVersions(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: query applied versions: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("migrate: scan version: %w", err)
		}
		applied[v] = true
	}

	return applied, rows.Err()
}

// versionOf strips the ".up.sql" suffix from a migration filename to produce
// the canonical version key stored in schema_migrations.
// "001_create_users_table.up.sql" → "001_create_users_table"
func versionOf(filename string) string {
	return strings.TrimSuffix(filename, ".up.sql")
}
