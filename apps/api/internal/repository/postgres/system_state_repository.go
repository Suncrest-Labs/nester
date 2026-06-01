package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
)

// SystemStateRepository implements systemstate.Repository backed by the
// system_state PostgreSQL table.
type SystemStateRepository struct {
	db *sql.DB
}

// NewSystemStateRepository returns a new SystemStateRepository.
func NewSystemStateRepository(db *sql.DB) *SystemStateRepository {
	return &SystemStateRepository{db: db}
}

// Get returns the value stored under key.  If the key does not exist it
// returns ("", systemstate.ErrKeyNotFound).
func (r *SystemStateRepository) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(
		ctx,
		`SELECT value FROM system_state WHERE key = $1`,
		key,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", systemstate.ErrKeyNotFound
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

// Set upserts the value for key, refreshing updated_at to NOW().
func (r *SystemStateRepository) Set(ctx context.Context, key string, value string) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO system_state (key, value, updated_at)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (key) DO UPDATE
		     SET value      = EXCLUDED.value,
		         updated_at = NOW()`,
		key,
		value,
	)
	return err
}
