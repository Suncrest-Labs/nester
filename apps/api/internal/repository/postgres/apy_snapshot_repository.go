package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
)

type APYSnapshotRepository struct {
	db *sql.DB
}

func NewAPYSnapshotRepository(db *sql.DB) *APYSnapshotRepository {
	return &APYSnapshotRepository{db: db}
}

func (r *APYSnapshotRepository) Upsert(ctx context.Context, snap apysnapshot.APYSnapshot) error {
	const q = `
		INSERT INTO apy_snapshots (id, protocol_slug, apy, tvl, captured_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING`
	_, err := r.db.ExecContext(ctx, q,
		snap.ID,
		snap.ProtocolSlug,
		snap.APY.String(),
		snap.TVL.String(),
		snap.CapturedAt,
	)
	return err
}

func (r *APYSnapshotRepository) ListByProtocol(ctx context.Context, slug string, since time.Time) ([]apysnapshot.APYSnapshot, error) {
	const q = `
		SELECT id, protocol_slug, apy, tvl, captured_at
		FROM apy_snapshots
		WHERE protocol_slug = $1
		  AND captured_at >= $2
		ORDER BY captured_at ASC`

	rows, err := r.db.QueryContext(ctx, q, slug, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []apysnapshot.APYSnapshot
	for rows.Next() {
		s, err := scanAPYSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snaps = append(snaps, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return snaps, nil
}

func (r *APYSnapshotRepository) PruneOlderThan(ctx context.Context, age time.Duration) error {
	cutoff := time.Now().UTC().Add(-age)
	const q = `DELETE FROM apy_snapshots WHERE captured_at < $1`
	_, err := r.db.ExecContext(ctx, q, cutoff)
	return err
}

func scanAPYSnapshot(row interface {
	Scan(dest ...any) error
}) (apysnapshot.APYSnapshot, error) {
	var (
		id           string
		protocolSlug string
		apy          string
		tvl          string
		capturedAt   time.Time
	)
	if err := row.Scan(&id, &protocolSlug, &apy, &tvl, &capturedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apysnapshot.APYSnapshot{}, apysnapshot.ErrProtocolNotFound
		}
		return apysnapshot.APYSnapshot{}, err
	}

	parsedID, err := uuid.Parse(id)
	if err != nil {
		return apysnapshot.APYSnapshot{}, err
	}
	apyDec, err := decimal.NewFromString(apy)
	if err != nil {
		return apysnapshot.APYSnapshot{}, err
	}
	tvlDec, err := decimal.NewFromString(tvl)
	if err != nil {
		return apysnapshot.APYSnapshot{}, err
	}

	return apysnapshot.APYSnapshot{
		ID:           parsedID,
		ProtocolSlug: protocolSlug,
		APY:          apyDec,
		TVL:          tvlDec,
		CapturedAt:   capturedAt,
	}, nil
}
