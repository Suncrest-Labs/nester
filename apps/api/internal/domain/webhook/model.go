package webhook

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrWebhookNotFound  = errors.New("webhook not found")
	ErrInvalidWebhook   = errors.New("invalid webhook")
	ErrDeliveryNotFound = errors.New("webhook delivery not found")
)

// Status is a subscription's lifecycle state.
type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
)

// DeadLetterSuspendThreshold is how many consecutive dead-lettered
// deliveries auto-suspend a subscription (#836): a permanently-broken
// endpoint should not be retried forever, and the counter resets only on a
// subsequent success.
const DeadLetterSuspendThreshold = 5

type Webhook struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
	URL    string    `json:"url"`
	// Secret is only ever populated in the Register response (shown once);
	// every other read returns it empty — SecretCiphertext is what's stored.
	Secret                 string     `json:"secret,omitempty"`
	SecretCiphertext       []byte     `json:"-"`
	SecretKeyVersion       string     `json:"-"`
	EventTypes             []string   `json:"event_types"`
	Status                 Status     `json:"status"`
	ConsecutiveDeadLetters int        `json:"-"`
	SuspendedAt            *time.Time `json:"suspended_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
}

// Subscribes reports whether the subscription wants deliveries for
// eventType. An empty EventTypes list means "all events" — the common case
// for a subscriber that has not opted into filtering.
func (w Webhook) Subscribes(eventType string) bool {
	if len(w.EventTypes) == 0 {
		return true
	}
	for _, t := range w.EventTypes {
		if t == eventType {
			return true
		}
	}
	return false
}

// DeliveryOutcome is the result of a single delivery attempt.
type DeliveryOutcome string

const (
	DeliveryPending    DeliveryOutcome = "pending"
	DeliverySucceeded  DeliveryOutcome = "succeeded"
	DeliveryFailed     DeliveryOutcome = "failed"
	DeliveryDeadLetter DeliveryOutcome = "dead_letter"
)

// Delivery is one logged attempt to deliver an event to a subscription.
// DeliveryID is stable across retries of the same logical delivery (it is
// what recipients dedupe on); a manual redelivery starts a new DeliveryID
// since it is a new attempt chain, not a retry of the original.
type Delivery struct {
	ID                  uuid.UUID       `json:"id"`
	WebhookID           uuid.UUID       `json:"webhook_id"`
	DeliveryID          uuid.UUID       `json:"delivery_id"`
	EventType           string          `json:"event_type"`
	Payload             []byte          `json:"-"`
	Attempt             int             `json:"attempt"`
	Outcome             DeliveryOutcome `json:"outcome"`
	ResponseStatus      *int            `json:"response_status,omitempty"`
	ResponseBodySnippet string          `json:"response_body_snippet,omitempty"`
	Error               string          `json:"error,omitempty"`
	DurationMS          int             `json:"duration_ms"`
	CreatedAt           time.Time       `json:"created_at"`
}

type Repository interface {
	Create(ctx context.Context, wh *Webhook) error
	ListByUser(ctx context.Context, userID uuid.UUID) ([]Webhook, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Webhook, error)
	Delete(ctx context.Context, id, userID uuid.UUID) error

	// RecordDeliveryOutcome updates the subscription's consecutive-dead-letter
	// counter: incremented on DeliveryDeadLetter, reset to 0 on
	// DeliverySucceeded. When the increment crosses DeadLetterSuspendThreshold
	// the subscription is transitioned to StatusSuspended and suspended=true
	// is returned so the caller can notify the owner exactly once.
	RecordDeliveryOutcome(ctx context.Context, webhookID uuid.UUID, outcome DeliveryOutcome) (suspended bool, err error)
}

// DeliveryRepository is the persistence port for the delivery log.
type DeliveryRepository interface {
	Log(ctx context.Context, d *Delivery) error
	ListByWebhook(ctx context.Context, webhookID uuid.UUID, limit int) ([]Delivery, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Delivery, error)
}
