package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
)

// NotificationSendJobType is the job type the outbox relay routes
// OutboxEventNotificationSend to (#1049).
//
// Distinct from NotificationRetryJobType: that one redelivers ONE channel of
// an already-dispatched notification, whereas this one performs the original
// dispatch — the send that used to happen inline (or in a detached
// goroutine) right after a domain write, and was therefore lost whenever the
// process died in between.
const NotificationSendJobType = "notification_send"

// OutboxEventNotificationSend is the outbox event type for "send this
// notification to this user".
const OutboxEventNotificationSend = "notification.send"

// outboxDedupWindow is how long the dispatcher suppresses a repeat of the
// same (user, event, dedupe key). It is what turns the outbox's at-least-once
// hand-off into an at-most-once-in-practice notification: a redelivered
// outbox event carries the same dedupe key, so the second send is suppressed
// rather than shown to the user twice.
//
// Wide enough to cover a realistic redelivery window (queue backoff plus a
// relay outage), narrow enough that a genuinely repeated event a day later
// still reaches the user.
const outboxDedupWindow = 6 * time.Hour

// NotificationSendPayload is the outbox payload for a notification side
// effect. It carries the rendered title and body rather than the data needed
// to render them: the copy belongs to the moment the domain event happened,
// and re-deriving it at delivery time would describe the world as it is when
// the queue gets around to it instead.
type NotificationSendPayload struct {
	UserID    uuid.UUID      `json:"user_id"`
	EventType EventType      `json:"event_type"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Payload   map[string]any `json:"payload,omitempty"`
	DedupeKey string         `json:"dedupe_key"`
}

// NewNotificationSendEvent builds the outbox event a producer inserts inside
// its domain transaction. dedupeKey must be derived from stable inputs (the
// aggregate and what happened to it) so a retried producer transaction, or a
// redelivered outbox row, resolves to the same notification rather than a
// second one.
func NewNotificationSendEvent(
	aggregateType, aggregateID string,
	userID uuid.UUID,
	evt EventType,
	title, body string,
	payload map[string]any,
	dedupeKey string,
) (outbox.Event, error) {
	return outbox.NewEvent(aggregateType, aggregateID, OutboxEventNotificationSend, dedupeKey, NotificationSendPayload{
		UserID:    userID,
		EventType: evt,
		Title:     title,
		Body:      body,
		Payload:   payload,
		DedupeKey: dedupeKey,
	})
}

// NewNotificationSendJobHandler builds the jobqueue.Handler that dispatches
// an outbox notification event.
//
// Idempotency is delegated to the dispatcher's existing deduplicator via
// SendOptions rather than reimplemented: a repeat of the same dedupe key
// inside outboxDedupWindow is recorded as suppressed and not shown again. If
// no Deduplicator is configured the send simply proceeds — at-least-once, as
// documented, rather than silently dropped.
func NewNotificationSendJobHandler(dispatcher *Dispatcher) jobqueue.Handler {
	return jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
		if dispatcher == nil {
			return jobqueue.Permanent(errors.New("notification send: no dispatcher configured"))
		}
		var p NotificationSendPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return jobqueue.Permanent(fmt.Errorf("notification send: unmarshal payload: %w", err))
		}
		if p.DedupeKey == "" {
			return jobqueue.Permanent(errors.New("notification send: payload carries no dedupe key"))
		}
		if len(ChannelsFor(p.EventType)) == 0 {
			// An event type the matrix does not know cannot be routed to any
			// channel, and retrying will not teach it one.
			return jobqueue.Permanent(fmt.Errorf("notification send: unknown event type %q", p.EventType))
		}

		err := dispatcher.SendWithOptions(ctx, p.UserID, p.EventType, p.Title, p.Body, p.Payload, SendOptions{
			DedupKey:    p.DedupeKey,
			DedupWindow: outboxDedupWindow,
		})
		if err != nil {
			return fmt.Errorf("notification send: %w", err)
		}
		return nil
	})
}
