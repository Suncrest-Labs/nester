package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/digest"
)

// DigestRepository is the Postgres-backed implementation of digest.Repository
// (#859): the authoritative cache/audit table for generated digests.
type DigestRepository struct {
	db *sql.DB
}

func NewDigestRepository(db *sql.DB) *DigestRepository {
	return &DigestRepository{db: db}
}

func (r *DigestRepository) GetCached(ctx context.Context, userID uuid.UUID, period digest.Period, periodStart time.Time) (*digest.CachedDigest, error) {
	const query = `
		SELECT id, user_id, period, period_start, period_end, facts_hash, facts::text,
		       narrative, attention_items::text, honest_zero_period, delivered_at, generated_at
		FROM user_digests
		WHERE user_id = $1 AND period = $2 AND period_start = $3
	`
	d, err := scanDigest(r.db.QueryRowContext(ctx, query, userID, string(period), periodStart.UTC()))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (r *DigestRepository) GetLatest(ctx context.Context, userID uuid.UUID) (*digest.CachedDigest, error) {
	const query = `
		SELECT id, user_id, period, period_start, period_end, facts_hash, facts::text,
		       narrative, attention_items::text, honest_zero_period, delivered_at, generated_at
		FROM user_digests
		WHERE user_id = $1
		ORDER BY period_start DESC
		LIMIT 1
	`
	d, err := scanDigest(r.db.QueryRowContext(ctx, query, userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

func (r *DigestRepository) Save(ctx context.Context, d digest.CachedDigest) (digest.CachedDigest, error) {
	const query = `
		INSERT INTO user_digests (
			id, user_id, period, period_start, period_end, facts_hash, facts,
			narrative, attention_items, honest_zero_period, generated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9::jsonb, $10, $11)
		ON CONFLICT (user_id, period, period_start)
		DO UPDATE SET
			period_end = EXCLUDED.period_end,
			facts_hash = EXCLUDED.facts_hash,
			facts = EXCLUDED.facts,
			narrative = EXCLUDED.narrative,
			attention_items = EXCLUDED.attention_items,
			honest_zero_period = EXCLUDED.honest_zero_period,
			generated_at = EXCLUDED.generated_at
		RETURNING id, user_id, period, period_start, period_end, facts_hash, facts::text,
		          narrative, attention_items::text, honest_zero_period, delivered_at, generated_at
	`
	id := d.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	generatedAt := d.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now().UTC()
	}

	factsJSON := d.FactsJSON
	if factsJSON == "" {
		factsJSON = "{}"
	}
	attentionJSON := d.AttentionItemsJSON
	if attentionJSON == "" {
		attentionJSON = "[]"
	}

	saved, err := scanDigest(r.db.QueryRowContext(
		ctx, query,
		id, d.UserID, string(d.Period), d.PeriodStart.UTC(), d.PeriodEnd.UTC(),
		d.FactsHash, factsJSON, d.Narrative, attentionJSON, d.HonestZeroPeriod, generatedAt,
	))
	if err != nil {
		return digest.CachedDigest{}, err
	}
	return saved, nil
}

func (r *DigestRepository) MarkDelivered(ctx context.Context, id uuid.UUID, deliveredAt time.Time) error {
	const query = `UPDATE user_digests SET delivered_at = $2 WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id, deliveredAt.UTC())
	return err
}

type digestScanner interface {
	Scan(dest ...any) error
}

func scanDigest(row digestScanner) (digest.CachedDigest, error) {
	var d digest.CachedDigest
	var period string
	var deliveredAt sql.NullTime
	if err := row.Scan(
		&d.ID, &d.UserID, &period, &d.PeriodStart, &d.PeriodEnd, &d.FactsHash, &d.FactsJSON,
		&d.Narrative, &d.AttentionItemsJSON, &d.HonestZeroPeriod, &deliveredAt, &d.GeneratedAt,
	); err != nil {
		return digest.CachedDigest{}, err
	}
	d.Period = digest.Period(period)
	if deliveredAt.Valid {
		t := deliveredAt.Time
		d.DeliveredAt = &t
	}
	return d, nil
}
