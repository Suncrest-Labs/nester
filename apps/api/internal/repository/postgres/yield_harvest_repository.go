package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/yieldharvest"
)

// YieldHarvestRepository implements yieldharvest.Repository using Postgres.
type YieldHarvestRepository struct {
	db *sql.DB
}

// NewYieldHarvestRepository constructs a YieldHarvestRepository.
func NewYieldHarvestRepository(db *sql.DB) *YieldHarvestRepository {
	return &YieldHarvestRepository{db: db}
}

// Create inserts a new yield_harvests row and returns the persisted record.
func (r *YieldHarvestRepository) Create(ctx context.Context, input yieldharvest.CreateInput) (yieldharvest.YieldHarvest, error) {
	if input.HarvestedAt.IsZero() {
		input.HarvestedAt = time.Now().UTC()
	}

	const q = `
		INSERT INTO yield_harvests (user_id, vault_id, amount, currency, harvested_at, tx_hash)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, user_id, vault_id, amount, currency, harvested_at, tx_hash`

	row := r.db.QueryRowContext(ctx, q,
		input.UserID,
		input.VaultID,
		input.Amount,
		input.Currency,
		input.HarvestedAt,
		nullableString(input.TxHash),
	)

	return scanYieldHarvest(row)
}

// ListForUser returns harvest records for a user with keyset cursor pagination.
// Results are ordered (harvested_at DESC, id DESC). An optional protocol value
// is resolved by joining vault_allocations to surface the primary protocol.
func (r *YieldHarvestRepository) ListForUser(ctx context.Context, filter yieldharvest.ListFilter) ([]yieldharvest.YieldHarvest, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	args := []any{filter.UserID, limit}
	where := "yh.user_id = $1"

	if filter.CursorTime != nil && filter.CursorID != nil {
		args = append(args, *filter.CursorTime, *filter.CursorID)
		idx := len(args) - 1
		where += fmt.Sprintf(" AND (yh.harvested_at, yh.id) < ($%d, $%d)", idx, idx+1)
	}
	if filter.Since != nil {
		args = append(args, *filter.Since)
		where += fmt.Sprintf(" AND yh.harvested_at >= $%d", len(args))
	}
	if filter.Until != nil {
		args = append(args, *filter.Until)
		where += fmt.Sprintf(" AND yh.harvested_at < $%d", len(args))
	}

	q := fmt.Sprintf(`
		SELECT
			yh.id,
			yh.user_id,
			yh.vault_id,
			yh.amount,
			yh.currency,
			COALESCE(va.protocol, '') AS protocol,
			yh.harvested_at,
			COALESCE(yh.tx_hash, '') AS tx_hash
		FROM yield_harvests yh
		LEFT JOIN LATERAL (
			SELECT protocol
			FROM vault_allocations
			WHERE vault_id = yh.vault_id
			ORDER BY allocated_at DESC
			LIMIT 1
		) va ON true
		WHERE %s
		ORDER BY yh.harvested_at DESC, yh.id DESC
		LIMIT $2`, where)

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("yield_harvests list: %w", err)
	}
	defer rows.Close()

	var results []yieldharvest.YieldHarvest
	for rows.Next() {
		var h yieldharvest.YieldHarvest
		var amountStr string
		if err := rows.Scan(
			&h.ID,
			&h.UserID,
			&h.VaultID,
			&amountStr,
			&h.Currency,
			&h.Protocol,
			&h.HarvestedAt,
			&h.TxHash,
		); err != nil {
			return nil, fmt.Errorf("yield_harvests scan: %w", err)
		}
		h.Amount, err = decimal.NewFromString(amountStr)
		if err != nil {
			return nil, fmt.Errorf("yield_harvests parse amount: %w", err)
		}
		results = append(results, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("yield_harvests rows: %w", err)
	}
	return results, nil
}

func scanYieldHarvest(row *sql.Row) (yieldharvest.YieldHarvest, error) {
	var h yieldharvest.YieldHarvest
	var amountStr string
	var txHash sql.NullString
	if err := row.Scan(
		&h.ID,
		&h.UserID,
		&h.VaultID,
		&amountStr,
		&h.Currency,
		&h.HarvestedAt,
		&txHash,
	); err != nil {
		return yieldharvest.YieldHarvest{}, fmt.Errorf("yield_harvest scan: %w", err)
	}
	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		return yieldharvest.YieldHarvest{}, fmt.Errorf("yield_harvest parse amount: %w", err)
	}
	h.Amount = amount
	if txHash.Valid {
		h.TxHash = txHash.String
	}
	return h, nil
}

// nullableString returns nil when s is empty so Postgres stores NULL instead of "".
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
