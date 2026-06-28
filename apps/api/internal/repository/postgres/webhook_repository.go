package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
)

type WebhookRepository struct {
	db *sql.DB
}

func NewWebhookRepository(db *sql.DB) *WebhookRepository {
	return &WebhookRepository{db: db}
}

func (r *WebhookRepository) Create(ctx context.Context, wh *webhook.Webhook) error {
	return r.db.QueryRowContext(ctx, `
		INSERT INTO webhooks (id, user_id, url, secret)
		VALUES ($1, $2, $3, $4)
		RETURNING created_at
	`, wh.ID, wh.UserID, wh.URL, nullSQLString(wh.Secret)).Scan(&wh.CreatedAt)
}

func (r *WebhookRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]webhook.Webhook, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, url, secret, created_at
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
		SELECT id, user_id, url, secret, created_at
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

type webhookScanner interface {
	Scan(dest ...any) error
}

func scanWebhook(row webhookScanner) (webhook.Webhook, error) {
	var (
		id, userID string
		url        string
		secret     sql.NullString
		createdAt  time.Time
	)
	if err := row.Scan(&id, &userID, &url, &secret, &createdAt); err != nil {
		return webhook.Webhook{}, err
	}
	parsedID, _ := uuid.Parse(id)
	parsedUserID, _ := uuid.Parse(userID)
	secretVal := ""
	if secret.Valid {
		secretVal = secret.String
	}
	return webhook.Webhook{
		ID:        parsedID,
		UserID:    parsedUserID,
		URL:       url,
		Secret:    secretVal,
		CreatedAt: createdAt,
	}, nil
}
