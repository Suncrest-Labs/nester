package webhook

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrWebhookNotFound = errors.New("webhook not found")
	ErrInvalidWebhook  = errors.New("invalid webhook")
)

type Webhook struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	URL       string    `json:"url"`
	Secret    string    `json:"secret,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Repository interface {
	Create(ctx context.Context, wh *Webhook) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]Webhook, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Webhook, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error
}
