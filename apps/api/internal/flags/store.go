package flags

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrSecretRejected is returned when a caller attempts to store a value that
// looks like a secret. Secrets belong in the environment / secret store, not
// the flag database, which is readable through the admin surface.
var ErrSecretRejected = errors.New("flags: secret-marked values must not be stored in the flag store")

// secretMarkers are name fragments that indicate a value is a secret.
var secretMarkers = []string{"secret", "password", "passwd", "private_key", "privatekey", "api_key", "apikey", "token", "credential"}

// rejectSecrets is the guard enforcing the hard boundary between operational
// flags and secret/infrastructure configuration.
func rejectSecrets(f Flag) error {
	lower := strings.ToLower(f.Name)
	for _, m := range secretMarkers {
		if strings.Contains(lower, m) {
			return fmt.Errorf("%w: flag name %q matches secret marker %q", ErrSecretRejected, f.Name, m)
		}
	}
	return nil
}

// Store persists flags in Postgres — the source of truth for flag state.
type Store struct {
	db    *sql.DB
	audit AuditRecorder
	// invalidate broadcasts a flag-changed event to all instances
	// (Redis pub/sub in production; see Cache.SubscribeInvalidations).
	invalidate func(ctx context.Context, name string)
}

// NewStore builds a Store. audit may not be nil: every flag change must be
// traceable. invalidate may be nil in tests.
func NewStore(db *sql.DB, audit AuditRecorder, invalidate func(ctx context.Context, name string)) (*Store, error) {
	if audit == nil {
		return nil, errors.New("flags: an AuditRecorder is required — flag changes must be audited")
	}
	return &Store{db: db, audit: audit, invalidate: invalidate}, nil
}

// Get loads one flag by name.
func (s *Store) Get(ctx context.Context, name string) (Flag, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT name, type, enabled, percentage, cohort, value, description, owner, fail_safe_on, created_at, updated_at
		FROM feature_flags WHERE name = $1`, name)
	return scanFlag(row)
}

// List returns all flags ordered by name (admin surface).
func (s *Store) List(ctx context.Context) ([]Flag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, type, enabled, percentage, cohort, value, description, owner, fail_safe_on, created_at, updated_at
		FROM feature_flags ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("flags: list: %w", err)
	}
	defer rows.Close()

	var out []Flag
	for rows.Next() {
		f, err := scanFlag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Set creates or updates a flag, records the change through the audit log,
// and broadcasts an invalidation so every instance re-reads within seconds.
// actor identifies who made the change.
func (s *Store) Set(ctx context.Context, actor string, f Flag) error {
	if err := f.Validate(); err != nil {
		return err
	}
	if err := rejectSecrets(f); err != nil {
		return err
	}

	var before *Flag
	if prev, err := s.Get(ctx, f.Name); err == nil {
		before = &prev
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}

	cohort, err := json.Marshal(f.Cohort)
	if err != nil {
		return fmt.Errorf("flags: marshal cohort: %w", err)
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO feature_flags (name, type, enabled, percentage, cohort, value, description, owner, fail_safe_on, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)
		ON CONFLICT (name) DO UPDATE SET
			type = EXCLUDED.type,
			enabled = EXCLUDED.enabled,
			percentage = EXCLUDED.percentage,
			cohort = EXCLUDED.cohort,
			value = EXCLUDED.value,
			description = EXCLUDED.description,
			owner = EXCLUDED.owner,
			fail_safe_on = EXCLUDED.fail_safe_on,
			updated_at = EXCLUDED.updated_at`,
		f.Name, string(f.Type), f.Enabled, f.Percentage, cohort, f.Value, f.Description, f.Owner, f.FailSafeOn, now)
	if err != nil {
		return fmt.Errorf("flags: set %q: %w", f.Name, err)
	}

	f.UpdatedAt = now
	if err := s.audit.RecordFlagChange(ctx, actor, f.Name, before, &f); err != nil {
		return fmt.Errorf("flags: audit record for %q: %w", f.Name, err)
	}
	if s.invalidate != nil {
		s.invalidate(ctx, f.Name)
	}
	return nil
}

// Delete removes a flag, auditing the removal.
func (s *Store) Delete(ctx context.Context, actor, name string) error {
	prev, err := s.Get(ctx, name)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM feature_flags WHERE name = $1`, name); err != nil {
		return fmt.Errorf("flags: delete %q: %w", name, err)
	}
	if err := s.audit.RecordFlagChange(ctx, actor, name, &prev, nil); err != nil {
		return fmt.Errorf("flags: audit record for %q: %w", name, err)
	}
	if s.invalidate != nil {
		s.invalidate(ctx, name)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFlag(row rowScanner) (Flag, error) {
	var (
		f      Flag
		typ    string
		cohort []byte
	)
	err := row.Scan(&f.Name, &typ, &f.Enabled, &f.Percentage, &cohort, &f.Value, &f.Description, &f.Owner, &f.FailSafeOn, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Flag{}, ErrNotFound
	}
	if err != nil {
		return Flag{}, fmt.Errorf("flags: scan: %w", err)
	}
	f.Type = Type(typ)
	if len(cohort) > 0 {
		if err := json.Unmarshal(cohort, &f.Cohort); err != nil {
			return Flag{}, fmt.Errorf("flags: unmarshal cohort: %w", err)
		}
	}
	return f, nil
}
