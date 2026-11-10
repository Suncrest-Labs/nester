package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savings"
)

type SavingsRepository struct {
	db *sql.DB
}

var _ savings.SavingsRepository = (*SavingsRepository)(nil)

func NewSavingsRepository(db *sql.DB) *SavingsRepository {
	return &SavingsRepository{db: db}
}

func (r *SavingsRepository) Insert(ctx context.Context, s savings.Snapshot) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO savings_snapshots (taken_at, share_price, total_assets, total_supply, reserve_shares)
		VALUES ($1, $2, $3, $4, $5)
	`, s.TakenAt, s.SharePrice.String(), s.TotalAssets.String(), s.TotalSupply.String(), s.ReserveShares.String())
	return err
}

func (r *SavingsRepository) Since(ctx context.Context, t time.Time) ([]savings.Snapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT taken_at, share_price::text, total_assets::text, total_supply::text, reserve_shares::text
		FROM savings_snapshots
		WHERE taken_at >= $1
		ORDER BY taken_at ASC, id ASC
	`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []savings.Snapshot
	for rows.Next() {
		s, err := scanSavingsSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *SavingsRepository) Latest(ctx context.Context) (savings.Snapshot, error) {
	s, err := scanSavingsSnapshot(r.db.QueryRowContext(ctx, `
		SELECT taken_at, share_price::text, total_assets::text, total_supply::text, reserve_shares::text
		FROM savings_snapshots
		ORDER BY taken_at DESC, id DESC
		LIMIT 1
	`))
	if errors.Is(err, sql.ErrNoRows) {
		return savings.Snapshot{}, savings.ErrSnapshotNotFound
	}
	return s, err
}

type savingsRowScanner interface {
	Scan(dest ...any) error
}

func scanSavingsSnapshot(row savingsRowScanner) (savings.Snapshot, error) {
	var (
		s                              savings.Snapshot
		price, assets, supply, reserve string
	)
	if err := row.Scan(&s.TakenAt, &price, &assets, &supply, &reserve); err != nil {
		return savings.Snapshot{}, err
	}
	var err error
	if s.SharePrice, err = decimal.NewFromString(price); err != nil {
		return savings.Snapshot{}, err
	}
	if s.TotalAssets, err = decimal.NewFromString(assets); err != nil {
		return savings.Snapshot{}, err
	}
	if s.TotalSupply, err = decimal.NewFromString(supply); err != nil {
		return savings.Snapshot{}, err
	}
	if s.ReserveShares, err = decimal.NewFromString(reserve); err != nil {
		return savings.Snapshot{}, err
	}
	return s, nil
}
