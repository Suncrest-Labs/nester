package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type Repository interface {
	CreateEntry(ctx context.Context, entry *Entry) error
}

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateEntry(ctx context.Context, entry *Entry) error {
	query := `
		INSERT INTO ledger_entries (source_type, source_id, entry_type, amount, currency, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (source_type, source_id, entry_type) DO NOTHING
		RETURNING id
	`
	err := r.db.QueryRowContext(
		ctx,
		query,
		entry.SourceType,
		entry.SourceID,
		entry.EntryType,
		entry.Amount,
		entry.Currency,
		entry.CreatedAt,
	).Scan(&entry.ID)

	if errors.Is(err, sql.ErrNoRows) {
		// Entry already exists due to idempotency key violation (ON CONFLICT DO NOTHING)
		return ErrDuplicateEntry
	}
	if err != nil {
		return fmt.Errorf("failed to create ledger entry: %w", err)
	}

	return nil
}
