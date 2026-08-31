package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
)

type WebhookRepository struct {
	db *sql.DB
}

func NewWebhookRepository(db *sql.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

func (r *WebhookRepository) Create(ctx context.Context, wh *webhook.Webhook) error {
	if wh.Status == "" {
		wh.Status = webhook.StatusActive
	}
	return r.db.QueryRowContext(ctx, `
		INSERT INTO webhooks (id, user_id, url, secret, event_types, secret_key_version, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`, wh.ID, wh.UserID, wh.URL, wh.SecretCiphertext, pq.Array(wh.EventTypes), nullSQLString(wh.SecretKeyVersion), string(wh.Status),
	).Scan(&wh.CreatedAt)
}

func (r *WebhookRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]webhook.Webhook, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, url, secret, event_types, secret_key_version, status, consecutive_dead_letters, suspended_at, created_at
		FROM webhooks
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []webhook.Webhook
	for rows.Next() {
		wh, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, wh)
	}
	return out, rows.Err()
}

func (r *WebhookRepository) GetByID(ctx context.Context, id uuid.UUID) (*webhook.Webhook, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, url, secret, event_types, secret_key_version, status, consecutive_dead_letters, suspended_at, created_at
		FROM webhooks WHERE id = $1
	`, id)
	wh, err := scanWebhook(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, webhook.ErrWebhookNotFound
		}
		return nil, err
	}
	return &wh, nil
}

func (r *WebhookRepository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM webhooks WHERE id = $1 AND user_id = $2
	`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return webhook.ErrWebhookNotFound
	}
	return nil
}

// RecordDeliveryOutcome implements webhook.Repository. A dead-letter
// increments the streak and, only on the transaction that crosses the
// threshold (old count < threshold, new count >= threshold), flips the
// subscription to suspended and reports suspended=true so the caller
// notifies the owner exactly once rather than on every subsequent failure.
func (r *WebhookRepository) RecordDeliveryOutcome(ctx context.Context, webhookID uuid.UUID, outcome webhook.DeliveryOutcome) (bool, error) {
	if outcome == webhook.DeliverySucceeded {
		_, err := r.db.ExecContext(ctx, `
			UPDATE webhooks SET consecutive_dead_letters = 0 WHERE id = $1
		`, webhookID)
		return false, err
	}
	if outcome != webhook.DeliveryDeadLetter {
		return false, nil
	}

	var newCount int
	var status string
	err := r.db.QueryRowContext(ctx, `
		UPDATE webhooks
		SET consecutive_dead_letters = consecutive_dead_letters + 1,
		    status = CASE WHEN consecutive_dead_letters + 1 >= $2 THEN 'suspended' ELSE status END,
		    suspended_at = CASE WHEN consecutive_dead_letters + 1 >= $2 AND status <> 'suspended' THEN NOW() ELSE suspended_at END
		WHERE id = $1
		RETURNING consecutive_dead_letters, status
	`, webhookID, webhook.DeadLetterSuspendThreshold).Scan(&newCount, &status)
	if err != nil {
		return false, err
	}
	suspendedNow := status == string(webhook.StatusSuspended) && newCount == webhook.DeadLetterSuspendThreshold
	return suspendedNow, nil
}

type webhookScanner interface {
	Scan(dest ...any) error
}

func scanWebhook(row webhookScanner) (webhook.Webhook, error) {
	var (
		id, userID       string
		url              string
		secret           []byte
		eventTypes       pq.StringArray
		secretKeyVersion sql.NullString
		status           string
		consecutiveDLs   int
		suspendedAt      sql.NullTime
		createdAt        time.Time
	)
	if err := row.Scan(&id, &userID, &url, &secret, &eventTypes, &secretKeyVersion, &status, &consecutiveDLs, &suspendedAt, &createdAt); err != nil {
		return webhook.Webhook{}, err
	}
	parsedID, _ := uuid.Parse(id)
	parsedUserID, _ := uuid.Parse(userID)
	wh := webhook.Webhook{
		ID:                     parsedID,
		UserID:                 parsedUserID,
		URL:                    url,
		SecretCiphertext:       secret,
		SecretKeyVersion:       secretKeyVersion.String,
		EventTypes:             []string(eventTypes),
		Status:                 webhook.Status(status),
		ConsecutiveDeadLetters: consecutiveDLs,
		CreatedAt:              createdAt,
	}
	if suspendedAt.Valid {
		t := suspendedAt.Time
		wh.SuspendedAt = &t
	}
	return wh, nil
}
