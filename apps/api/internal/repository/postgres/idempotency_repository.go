package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
)

// IdempotencyRepository is the Postgres-backed implementation of
// middleware.IdempotencyStore (#835).
type IdempotencyRepository struct {
	db *sql.DB
}

func NewIdempotencyRepository(db *sql.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

// Claim atomically claims (userID, key) via INSERT ... ON CONFLICT DO
// NOTHING. claimed=true means this caller now owns the key and must go on
// to execute the handler and call Complete. claimed=false means someone
// else already holds (or completed) this key; existing carries whatever
// row is there now so the caller can decide what to do (reject on
// fingerprint mismatch, return the stored response, or wait).
func (r *IdempotencyRepository) Claim(
	ctx context.Context,
	userID uuid.UUID,
	key, fingerprint string,
	ttl time.Duration,
) (claimed bool, existing middleware.IdempotencyRecord, err error) {
	// Computed in Go and passed as a plain timestamp rather than an
	// interval literal, so this doesn't depend on Postgres parsing Go's
	// time.Duration string format (which isn't valid interval syntax).
	expiresAt := time.Now().Add(ttl)
	res, err := r.db.ExecContext(ctx, `
		INSERT INTO idempotency_keys (user_id, key, request_fingerprint, status, expires_at)
		VALUES ($1, $2, $3, 'in_progress', $4)
		ON CONFLICT (user_id, key) DO NOTHING
	`, userID, key, fingerprint, expiresAt)
	if err != nil {
		return false, middleware.IdempotencyRecord{}, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, middleware.IdempotencyRecord{}, err
	}
	if rowsAffected == 1 {
		return true, middleware.IdempotencyRecord{}, nil
	}

	existing, err = r.Get(ctx, userID, key)
	if err != nil {
		return false, middleware.IdempotencyRecord{}, err
	}
	return false, existing, nil
}

// Get returns the current record for (userID, key). Returns
// middleware.ErrIdempotencyKeyNotFound if no row exists (e.g. it expired
// and was purged between Claim's conflict and this read).
func (r *IdempotencyRepository) Get(ctx context.Context, userID uuid.UUID, key string) (middleware.IdempotencyRecord, error) {
	var rec middleware.IdempotencyRecord
	var responseStatus sql.NullInt64
	var responseBody []byte
	var responseContentType sql.NullString
	var completedAt sql.NullTime

	err := r.db.QueryRowContext(ctx, `
		SELECT request_fingerprint, status, response_status, response_body, response_content_type, completed_at
		FROM idempotency_keys
		WHERE user_id = $1 AND key = $2
	`, userID, key).Scan(&rec.Fingerprint, &rec.Status, &responseStatus, &responseBody, &responseContentType, &completedAt)
	if err == sql.ErrNoRows {
		return middleware.IdempotencyRecord{}, middleware.ErrIdempotencyKeyNotFound
	}
	if err != nil {
		return middleware.IdempotencyRecord{}, err
	}

	rec.ResponseStatus = int(responseStatus.Int64)
	rec.ResponseBody = responseBody
	rec.ResponseContentType = responseContentType.String
	return rec, nil
}

// Complete records the handler's response against a previously claimed
// key, transitioning it to status=completed.
func (r *IdempotencyRepository) Complete(
	ctx context.Context,
	userID uuid.UUID,
	key string,
	status int,
	body []byte,
	contentType string,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE idempotency_keys
		SET status = 'completed',
		    response_status = $3,
		    response_body = $4,
		    response_content_type = $5,
		    completed_at = NOW()
		WHERE user_id = $1 AND key = $2
	`, userID, key, status, body, contentType)
	return err
}

// Release deletes a claimed-but-never-completed key, e.g. after the
// handler panics — so a legitimate retry isn't permanently stuck behind a
// claim that will never complete. Best-effort: a failure here just means
// the row lingers until its TTL expires and PurgeExpired removes it.
func (r *IdempotencyRepository) Release(ctx context.Context, userID uuid.UUID, key string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM idempotency_keys WHERE user_id = $1 AND key = $2 AND status = 'in_progress'
	`, userID, key)
	return err
}

// PurgeExpired deletes every row past its expires_at, bounding table growth
// (#835's TTL requirement). Returns the number of rows removed.
func (r *IdempotencyRepository) PurgeExpired(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at < NOW()`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
