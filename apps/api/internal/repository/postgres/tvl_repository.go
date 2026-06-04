package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	tvldom "github.com/suncrestlabs/nester/apps/api/internal/domain/tvl"
)

type TVLRepository struct {
	db *sql.DB
}

func NewTVLRepository(db *sql.DB) *TVLRepository {
	return &TVLRepository{db: db}
}

func (r *TVLRepository) Insert(ctx context.Context, snapshot tvldom.Snapshot) (tvldom.Snapshot, error) {
	if snapshot.ID == uuid.Nil {
		snapshot.ID = uuid.New()
	}
	query := `
		INSERT INTO vault_tvl_snapshots (id, vault_id, tvl_usdc, total_depositors, snapshot_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING snapshot_at
	`
	if err := r.db.QueryRowContext(
		ctx,
		query,
		snapshot.ID.String(),
		snapshot.VaultID.String(),
		snapshot.TVLUSDC.StringFixed(6),
		snapshot.TotalDepositors,
		snapshot.SnapshotAt,
	).Scan(&snapshot.SnapshotAt); err != nil {
		return tvldom.Snapshot{}, mapRepositoryError(err)
	}
	return snapshot, nil
}

func (r *TVLRepository) LatestForVault(ctx context.Context, vaultID uuid.UUID) (tvldom.Snapshot, error) {
	const query = `
		SELECT id, vault_id, tvl_usdc::text, total_depositors, snapshot_at
		FROM vault_tvl_snapshots
		WHERE vault_id = $1
		ORDER BY snapshot_at DESC
		LIMIT 1
	`
	return scanTVLSnapshot(r.db.QueryRowContext(ctx, query, vaultID.String()))
}

func (r *TVLRepository) LatestAtOrBefore(ctx context.Context, vaultID uuid.UUID, at time.Time) (tvldom.Snapshot, error) {
	const query = `
		SELECT id, vault_id, tvl_usdc::text, total_depositors, snapshot_at
		FROM vault_tvl_snapshots
		WHERE vault_id = $1 AND snapshot_at <= $2
		ORDER BY snapshot_at DESC
		LIMIT 1
	`
	return scanTVLSnapshot(r.db.QueryRowContext(ctx, query, vaultID.String(), at))
}

func (r *TVLRepository) LatestPerActiveVault(ctx context.Context) ([]tvldom.Snapshot, error) {
	const query = `
		SELECT DISTINCT ON (s.vault_id)
			s.id, s.vault_id, s.tvl_usdc::text, s.total_depositors, s.snapshot_at
		FROM vault_tvl_snapshots s
		JOIN vaults v ON v.id = s.vault_id
		WHERE v.deleted_at IS NULL AND v.status = 'active'
		ORDER BY s.vault_id, s.snapshot_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, mapRepositoryError(err)
	}
	defer rows.Close()

	out := make([]tvldom.Snapshot, 0)
	for rows.Next() {
		snap, err := scanTVLSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (r *TVLRepository) CountDepositors(ctx context.Context, vaultID uuid.UUID) (int, error) {
	const query = `
		SELECT COUNT(DISTINCT COALESCE(vt.user_id, v.user_id))
		FROM vault_transactions vt
		JOIN vaults v ON v.id = vt.vault_id
		WHERE vt.vault_id = $1 AND vt.type = 'deposit'
	`
	var count int
	if err := r.db.QueryRowContext(ctx, query, vaultID.String()).Scan(&count); err != nil {
		return 0, mapRepositoryError(err)
	}
	return count, nil
}

type tvlScanner interface {
	Scan(dest ...any) error
}

func scanTVLSnapshot(row tvlScanner) (tvldom.Snapshot, error) {
	var (
		idStr    string
		vaultStr string
		tvlStr   string
		depositors int
		at       time.Time
	)
	if err := row.Scan(&idStr, &vaultStr, &tvlStr, &depositors, &at); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tvldom.Snapshot{}, tvldom.ErrSnapshotNotFound
		}
		return tvldom.Snapshot{}, mapRepositoryError(err)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return tvldom.Snapshot{}, fmt.Errorf("parse snapshot id: %w", err)
	}
	vaultID, err := uuid.Parse(vaultStr)
	if err != nil {
		return tvldom.Snapshot{}, fmt.Errorf("parse vault id: %w", err)
	}
	tvl, err := decimal.NewFromString(tvlStr)
	if err != nil {
		return tvldom.Snapshot{}, fmt.Errorf("parse tvl: %w", err)
	}
	return tvldom.Snapshot{
		ID:              id,
		VaultID:         vaultID,
		TVLUSDC:         tvl,
		TotalDepositors: depositors,
		SnapshotAt:      at.UTC(),
	}, nil
}
