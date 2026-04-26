package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
)

type PerformanceRepository struct {
	db *sql.DB
}

func NewPerformanceRepository(db *sql.DB) *PerformanceRepository {
	return &PerformanceRepository{db: db}
}

func (r *PerformanceRepository) Insert(ctx context.Context, snapshot performance.Snapshot) (performance.Snapshot, error) {
	if snapshot.ID == uuid.Nil {
		snapshot.ID = uuid.New()
	}
	if snapshot.SnapshotAt.IsZero() {
		snapshot.SnapshotAt = time.Now().UTC()
	}

	breakdown, err := marshalBreakdown(snapshot.AllocationBreakdown)
	if err != nil {
		return performance.Snapshot{}, err
	}

	const stmt = `
        INSERT INTO vault_performance_snapshots
            (id, vault_id, total_balance, total_deposited, total_yield_earned, share_price, snapshot_at, allocation_breakdown)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
    `
	if _, err := r.db.ExecContext(ctx, stmt,
		snapshot.ID,
		snapshot.VaultID,
		snapshot.TotalBalance.String(),
		snapshot.TotalDeposited.String(),
		snapshot.TotalYieldEarned.String(),
		snapshot.SharePrice.String(),
		snapshot.SnapshotAt,
		breakdown,
	); err != nil {
		return performance.Snapshot{}, fmt.Errorf("insert snapshot: %w", err)
	}
	return snapshot, nil
}

func (r *PerformanceRepository) LatestForVault(ctx context.Context, vaultID uuid.UUID) (performance.Snapshot, error) {
	const stmt = `
        SELECT id, vault_id, total_balance, total_deposited, total_yield_earned, share_price, snapshot_at, allocation_breakdown
        FROM vault_performance_snapshots
        WHERE vault_id = $1
        ORDER BY snapshot_at DESC
        LIMIT 1
    `
	row := r.db.QueryRowContext(ctx, stmt, vaultID)
	return scanSnapshot(row)
}

func (r *PerformanceRepository) HistoryForVault(ctx context.Context, vaultID uuid.UUID, since time.Time) ([]performance.Snapshot, error) {
	const stmt = `
        SELECT id, vault_id, total_balance, total_deposited, total_yield_earned, share_price, snapshot_at, allocation_breakdown
        FROM vault_performance_snapshots
        WHERE vault_id = $1 AND snapshot_at >= $2
        ORDER BY snapshot_at ASC
    `
	rows, err := r.db.QueryContext(ctx, stmt, vaultID, since)
	if err != nil {
		return nil, fmt.Errorf("history query: %w", err)
	}
	defer rows.Close()

	out := []performance.Snapshot{}
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PerformanceRepository) FirstAtOrAfter(ctx context.Context, vaultID uuid.UUID, since time.Time) (performance.Snapshot, error) {
	const stmt = `
        SELECT id, vault_id, total_balance, total_deposited, total_yield_earned, share_price, snapshot_at, allocation_breakdown
        FROM vault_performance_snapshots
        WHERE vault_id = $1 AND snapshot_at >= $2
        ORDER BY snapshot_at ASC
        LIMIT 1
    `
	row := r.db.QueryRowContext(ctx, stmt, vaultID, since)
	return scanSnapshot(row)
}

func (r *PerformanceRepository) UpsertAPY(ctx context.Context, record performance.APYRecord) error {
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	if record.CalculatedAt.IsZero() {
		record.CalculatedAt = time.Now().UTC()
	}
	const stmt = `
        INSERT INTO apy_history (id, vault_id, period, realized_apy, calculated_at)
        VALUES ($1, $2, $3, $4, $5)
    `
	if _, err := r.db.ExecContext(ctx, stmt,
		record.ID,
		record.VaultID,
		string(record.Period),
		record.RealizedAPY.String(),
		record.CalculatedAt,
	); err != nil {
		return fmt.Errorf("insert apy_history: %w", err)
	}
	return nil
}

func (r *PerformanceRepository) ListAPY(ctx context.Context, vaultID uuid.UUID) ([]performance.APYRecord, error) {
	// One latest row per period.
	const stmt = `
        SELECT DISTINCT ON (period) id, vault_id, period, realized_apy, calculated_at
        FROM apy_history
        WHERE vault_id = $1
        ORDER BY period, calculated_at DESC
    `
	rows, err := r.db.QueryContext(ctx, stmt, vaultID)
	if err != nil {
		return nil, fmt.Errorf("list apy: %w", err)
	}
	defer rows.Close()

	out := []performance.APYRecord{}
	for rows.Next() {
		var rec performance.APYRecord
		var period string
		var apyStr string
		if err := rows.Scan(&rec.ID, &rec.VaultID, &period, &apyStr, &rec.CalculatedAt); err != nil {
			return nil, err
		}
		rec.Period = performance.Period(period)
		apy, err := decimal.NewFromString(apyStr)
		if err != nil {
			return nil, fmt.Errorf("parse apy %q: %w", apyStr, err)
		}
		rec.RealizedAPY = apy
		out = append(out, rec)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSnapshot(row rowScanner) (performance.Snapshot, error) {
	var s performance.Snapshot
	var totalBalance, totalDeposited, yieldEarned, sharePrice string
	var breakdownRaw []byte

	err := row.Scan(
		&s.ID,
		&s.VaultID,
		&totalBalance,
		&totalDeposited,
		&yieldEarned,
		&sharePrice,
		&s.SnapshotAt,
		&breakdownRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return performance.Snapshot{}, performance.ErrSnapshotNotFound
	}
	if err != nil {
		return performance.Snapshot{}, fmt.Errorf("scan snapshot: %w", err)
	}

	if s.TotalBalance, err = decimal.NewFromString(totalBalance); err != nil {
		return performance.Snapshot{}, err
	}
	if s.TotalDeposited, err = decimal.NewFromString(totalDeposited); err != nil {
		return performance.Snapshot{}, err
	}
	if s.TotalYieldEarned, err = decimal.NewFromString(yieldEarned); err != nil {
		return performance.Snapshot{}, err
	}
	if s.SharePrice, err = decimal.NewFromString(sharePrice); err != nil {
		return performance.Snapshot{}, err
	}

	if len(breakdownRaw) > 0 {
		if err := json.Unmarshal(breakdownRaw, &s.AllocationBreakdown); err != nil {
			return performance.Snapshot{}, fmt.Errorf("decode breakdown: %w", err)
		}
	}
	return s, nil
}

func marshalBreakdown(entries []performance.AllocationBreakdownEntry) ([]byte, error) {
	if len(entries) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(entries)
}
