package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
)

var ErrRebalanceInFlight = errors.New("rebalance already in flight for this vault")

func (r *AdminRepository) HasInFlightRebalance(ctx context.Context, vaultID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM vault_rebalances
			WHERE vault_id = $1 AND status IN ('pending', 'submitted')
		)`, vaultID).Scan(&exists)
	return exists, err
}

func (r *AdminRepository) CreateVaultRebalance(
	ctx context.Context,
	record admindomain.VaultRebalanceRecord,
) (admindomain.VaultRebalanceRecord, error) {
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now

	const q = `
		INSERT INTO vault_rebalances (
			id, vault_id, strategy, dry_run, status, tx_hash, projected_deltas, error_message, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, vault_id, strategy, dry_run, status, tx_hash, projected_deltas, error_message, created_at, updated_at
	`

	row := r.db.QueryRowContext(ctx, q,
		record.ID,
		record.VaultID,
		record.Strategy,
		record.DryRun,
		record.Status,
		rebalanceNullString(record.TxHash),
		rebalanceNullJSON(record.ProjectedDeltas),
		rebalanceNullString(record.ErrorMessage),
		record.CreatedAt,
		record.UpdatedAt,
	)

	out, err := scanVaultRebalance(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return admindomain.VaultRebalanceRecord{}, ErrRebalanceInFlight
		}
		return admindomain.VaultRebalanceRecord{}, err
	}
	return out, nil
}

func (r *AdminRepository) UpdateVaultRebalance(
	ctx context.Context,
	record admindomain.VaultRebalanceRecord,
) (admindomain.VaultRebalanceRecord, error) {
	record.UpdatedAt = time.Now().UTC()

	const q = `
		UPDATE vault_rebalances
		SET status = $2,
		    tx_hash = $3,
		    projected_deltas = $4,
		    error_message = $5,
		    updated_at = $6
		WHERE id = $1
		RETURNING id, vault_id, strategy, dry_run, status, tx_hash, projected_deltas, error_message, created_at, updated_at
	`

	row := r.db.QueryRowContext(ctx, q,
		record.ID,
		record.Status,
		rebalanceNullString(record.TxHash),
		rebalanceNullJSON(record.ProjectedDeltas),
		rebalanceNullString(record.ErrorMessage),
		record.UpdatedAt,
	)

	return scanVaultRebalance(row)
}

func (r *AdminRepository) ListVaultRebalances(ctx context.Context, vaultID uuid.UUID) ([]admindomain.VaultRebalanceRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, vault_id, strategy, dry_run, status, tx_hash, projected_deltas, error_message, created_at, updated_at
		FROM vault_rebalances
		WHERE vault_id = $1
		ORDER BY created_at DESC
	`, vaultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]admindomain.VaultRebalanceRecord, 0)
	for rows.Next() {
		var out admindomain.VaultRebalanceRecord
		var txHash, errMsg sql.NullString
		var deltas []byte
		if err := rows.Scan(
			&out.ID,
			&out.VaultID,
			&out.Strategy,
			&out.DryRun,
			&out.Status,
			&txHash,
			&deltas,
			&errMsg,
			&out.CreatedAt,
			&out.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if txHash.Valid {
			out.TxHash = &txHash.String
		}
		if errMsg.Valid {
			out.ErrorMessage = &errMsg.String
		}
		if len(deltas) > 0 {
			out.ProjectedDeltas = deltas
		}
		items = append(items, out)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func scanVaultRebalance(row *sql.Row) (admindomain.VaultRebalanceRecord, error) {
	var out admindomain.VaultRebalanceRecord
	var txHash, errMsg sql.NullString
	var deltas []byte

	if err := row.Scan(
		&out.ID,
		&out.VaultID,
		&out.Strategy,
		&out.DryRun,
		&out.Status,
		&txHash,
		&deltas,
		&errMsg,
		&out.CreatedAt,
		&out.UpdatedAt,
	); err != nil {
		return admindomain.VaultRebalanceRecord{}, err
	}
	if txHash.Valid {
		out.TxHash = &txHash.String
	}
	if errMsg.Valid {
		out.ErrorMessage = &errMsg.String
	}
	if len(deltas) > 0 {
		out.ProjectedDeltas = deltas
	}
	return out, nil
}

func rebalanceNullString(s *string) sql.NullString {
	if s == nil || *s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func rebalanceNullJSON(raw []byte) interface{} {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
