package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
)

// WebhookDeliveryRepository is the Postgres-backed webhook.DeliveryRepository
// (#836's delivery log).
type WebhookDeliveryRepository struct {
	db *sql.DB
}

func NewWebhookDeliveryRepository(db *sql.DB) *WebhookDeliveryRepository {
	return &WebhookDeliveryRepository{db: db}
}

func (r *WebhookDeliveryRepository) Log(ctx context.Context, d *webhook.Delivery) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return r.db.QueryRowContext(ctx, `
		INSERT INTO webhook_deliveries (
			id, webhook_id, delivery_id, event_type, payload, attempt, outcome,
			response_status, response_body_snippet, error, duration_ms
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at
	`,
		d.ID, d.WebhookID, d.DeliveryID, d.EventType, d.Payload, d.Attempt, string(d.Outcome),
		nullSQLInt(d.ResponseStatus), nullSQLString(d.ResponseBodySnippet), nullSQLString(d.Error), d.DurationMS,
	).Scan(&d.CreatedAt)
}

func (r *WebhookDeliveryRepository) ListByWebhook(ctx context.Context, webhookID uuid.UUID, limit int) ([]webhook.Delivery, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, webhook_id, delivery_id, event_type, attempt, outcome,
		       response_status, response_body_snippet, error, duration_ms, created_at
		FROM webhook_deliveries
		WHERE webhook_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, webhookID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []webhook.Delivery
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *WebhookDeliveryRepository) GetByID(ctx context.Context, id uuid.UUID) (*webhook.Delivery, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, webhook_id, delivery_id, event_type, payload, attempt, outcome,
		       response_status, response_body_snippet, error, duration_ms, created_at
		FROM webhook_deliveries WHERE id = $1
	`, id)
	var (
		idStr, webhookIDStr, deliveryIDStr string
		eventType                          string
		payload                            []byte
		attempt                            int
		outcome                            string
		responseStatus                     sql.NullInt64
		responseBodySnippet, errText       sql.NullString
		durationMS                         int
		createdAt                          time.Time
	)
	if err := row.Scan(
		&idStr, &webhookIDStr, &deliveryIDStr, &eventType, &payload, &attempt, &outcome,
		&responseStatus, &responseBodySnippet, &errText, &durationMS, &createdAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, webhook.ErrDeliveryNotFound
		}
		return nil, err
	}
	d := deliveryFromScan(idStr, webhookIDStr, deliveryIDStr, eventType, attempt, outcome,
		responseStatus, responseBodySnippet, errText, durationMS, createdAt)
	d.Payload = payload
	return &d, nil
}

type deliveryScanner interface {
	Scan(dest ...any) error
}

func scanDelivery(row deliveryScanner) (webhook.Delivery, error) {
	var (
		idStr, webhookIDStr, deliveryIDStr string
		eventType                          string
		attempt                            int
		outcome                            string
		responseStatus                     sql.NullInt64
		responseBodySnippet, errText       sql.NullString
		durationMS                         int
		createdAt                          time.Time
	)
	if err := row.Scan(
		&idStr, &webhookIDStr, &deliveryIDStr, &eventType, &attempt, &outcome,
		&responseStatus, &responseBodySnippet, &errText, &durationMS, &createdAt,
	); err != nil {
		return webhook.Delivery{}, err
	}
	return deliveryFromScan(idStr, webhookIDStr, deliveryIDStr, eventType, attempt, outcome,
		responseStatus, responseBodySnippet, errText, durationMS, createdAt), nil
}

func deliveryFromScan(
	idStr, webhookIDStr, deliveryIDStr, eventType string,
	attempt int,
	outcome string,
	responseStatus sql.NullInt64,
	responseBodySnippet, errText sql.NullString,
	durationMS int,
	createdAt time.Time,
) webhook.Delivery {
	d := webhook.Delivery{
		ID:         uuid.MustParse(idStr),
		WebhookID:  uuid.MustParse(webhookIDStr),
		DeliveryID: uuid.MustParse(deliveryIDStr),
		EventType:  eventType,
		Attempt:    attempt,
		Outcome:    webhook.DeliveryOutcome(outcome),
		DurationMS: durationMS,
		CreatedAt:  createdAt,
	}
	if responseStatus.Valid {
		v := int(responseStatus.Int64)
		d.ResponseStatus = &v
	}
	if responseBodySnippet.Valid {
		d.ResponseBodySnippet = responseBodySnippet.String
	}
	if errText.Valid {
		d.Error = errText.String
	}
	return d
}

func nullSQLInt(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}
