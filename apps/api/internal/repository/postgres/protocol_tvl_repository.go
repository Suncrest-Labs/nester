package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/protocoltvl"
)

// ProtocolTVLRepository persists DeFiLlama protocol TVL snapshots and alert records.
type ProtocolTVLRepository struct {
	db *sql.DB
}

// NewProtocolTVLRepository constructs a ProtocolTVLRepository.
func NewProtocolTVLRepository(db *sql.DB) *ProtocolTVLRepository {
	return &ProtocolTVLRepository{db: db}
}

func (r *ProtocolTVLRepository) InsertSnapshot(ctx context.Context, slug string, tvlUSD float64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO protocol_tvl_snapshots (id, protocol_slug, tvl_usd, snapshotted_at)
		VALUES ($1, $2, $3, NOW())
	`, uuid.New(), slug, tvlUSD)
	return err
}

func (r *ProtocolTVLRepository) SnapshotAt(ctx context.Context, slug string, at time.Time) (*protocoltvl.Snapshot, error) {
	var (
		id           string
		protocolSlug string
		tvlStr       string
		snapshotted  time.Time
	)
	err := r.db.QueryRowContext(ctx, `
		SELECT id, protocol_slug, tvl_usd::text, snapshotted_at
		FROM protocol_tvl_snapshots
		WHERE protocol_slug = $1 AND snapshotted_at <= $2
		ORDER BY snapshotted_at DESC
		LIMIT 1
	`, slug, at).Scan(&id, &protocolSlug, &tvlStr, &snapshotted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	parsedID, _ := uuid.Parse(id)
	var tvl float64
	if _, scanErr := parseFloat64FromString(tvlStr, &tvl); scanErr != nil {
		return nil, scanErr
	}
	return &protocoltvl.Snapshot{
		ID:            parsedID,
		ProtocolSlug:  protocolSlug,
		TVLUSD:        tvl,
		SnapshottedAt: snapshotted,
	}, nil
}

func (r *ProtocolTVLRepository) LatestSnapshot(ctx context.Context, slug string) (*protocoltvl.Snapshot, error) {
	return r.SnapshotAt(ctx, slug, time.Now().Add(time.Minute))
}

func (r *ProtocolTVLRepository) ListSince(ctx context.Context, slug string, since time.Time) ([]protocoltvl.Snapshot, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, protocol_slug, tvl_usd::text, snapshotted_at
		FROM protocol_tvl_snapshots
		WHERE protocol_slug = $1 AND snapshotted_at >= $2
		ORDER BY snapshotted_at ASC
	`, slug, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []protocoltvl.Snapshot
	for rows.Next() {
		var (
			id, protocolSlug, tvlStr string
			snapshotted              time.Time
		)
		if err := rows.Scan(&id, &protocolSlug, &tvlStr, &snapshotted); err != nil {
			return nil, err
		}
		parsedID, _ := uuid.Parse(id)
		var tvl float64
		if _, scanErr := parseFloat64FromString(tvlStr, &tvl); scanErr != nil {
			return nil, scanErr
		}
		out = append(out, protocoltvl.Snapshot{ID: parsedID, ProtocolSlug: protocolSlug, TVLUSD: tvl, SnapshottedAt: snapshotted})
	}
	return out, rows.Err()
}

func (r *ProtocolTVLRepository) CanAlert(ctx context.Context, slug string) (bool, error) {
	var lastAlerted time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT last_alerted_at FROM protocol_health_alerts WHERE protocol_slug = $1
	`, slug).Scan(&lastAlerted)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, err
	}
	return time.Since(lastAlerted) >= protocoltvl.AlertCooldown, nil
}

func (r *ProtocolTVLRepository) RecordAlert(ctx context.Context, slug string) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO protocol_health_alerts (protocol_slug, last_alerted_at)
		VALUES ($1, NOW())
		ON CONFLICT (protocol_slug) DO UPDATE SET last_alerted_at = NOW()
	`, slug)
	return err
}

// parseFloat64FromString converts a Postgres NUMERIC text to float64.
func parseFloat64FromString(s string, out *float64) (bool, error) {
	if s == "" {
		return false, nil
	}
	n, err := fmt.Sscanf(s, "%f", out)
	return n == 1, err
}
