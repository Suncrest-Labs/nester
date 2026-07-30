package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
)

// NotificationRetryJobType is the durable job type used to retry a failed
// channel delivery (#829's "failed deliveries on a retryable channel go
// through the durable job queue for retry"). Register the handler from
// NewNotificationRetryJobHandler on the shared jobqueue.Worker.
const NotificationRetryJobType = "notification_retry"

// notificationRetryPayload is what's actually persisted in the job queue —
// small and JSON-serializable, not the full Notification (its Payload
// field is carried through as-is since channel adapters need it to
// re-render the message).
type notificationRetryPayload struct {
	NotificationID uuid.UUID      `json:"notification_id"`
	UserID         uuid.UUID      `json:"user_id"`
	Type           EventType      `json:"type"`
	Category       Category       `json:"category"`
	Title          string         `json:"title"`
	Body           string         `json:"body"`
	Payload        map[string]any `json:"payload"`
	Channel        ChannelKind    `json:"channel"`
}

// JobQueueRetryEnqueuer adapts a *jobqueue.Client to the RetryEnqueuer seam
// the Dispatcher uses, so the low-level notifications package's own types
// don't need every caller to also depend on jobqueue.
type JobQueueRetryEnqueuer struct {
	client *jobqueue.Client
}

// NewJobQueueRetryEnqueuer constructs a JobQueueRetryEnqueuer.
func NewJobQueueRetryEnqueuer(client *jobqueue.Client) *JobQueueRetryEnqueuer {
	return &JobQueueRetryEnqueuer{client: client}
}

// EnqueueRetry implements RetryEnqueuer.
func (e *JobQueueRetryEnqueuer) EnqueueRetry(ctx context.Context, n Notification, channel ChannelKind) error {
	payload := notificationRetryPayload{
		NotificationID: n.ID,
		UserID:         n.UserID,
		Type:           n.Type,
		Category:       n.Category,
		Title:          n.Title,
		Body:           n.Body,
		Payload:        n.Payload,
		Channel:        channel,
	}
	// Idempotency key scopes retries to one (notification, channel) pair so
	// a delivery that fails on two channels in the same Send call enqueues
	// two independent, non-duplicating retry jobs, and a crash-and-restart
	// of the enqueue itself doesn't double-enqueue the same retry.
	idempotencyKey := fmt.Sprintf("%s:%s", n.ID, channel)
	_, err := e.client.EnqueueJSON(ctx, NotificationRetryJobType, payload, jobqueue.WithIdempotencyKey(idempotencyKey))
	return err
}

// NewNotificationRetryJobHandler builds the jobqueue.Handler that
// redelivers a failed channel on retry via dispatcher.RedeliverChannel. A
// retry that still fails returns a plain (non-Permanent) error so the job
// queue's own backoff/dead-letter policy applies — this handler does not
// re-implement retry counting itself.
func NewNotificationRetryJobHandler(dispatcher *Dispatcher) jobqueue.Handler {
	return jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
		var payload notificationRetryPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return jobqueue.Permanent(fmt.Errorf("notification retry: unmarshal payload: %w", err))
		}

		n := Notification{
			ID:        payload.NotificationID,
			UserID:    payload.UserID,
			Type:      payload.Type,
			Category:  payload.Category,
			Title:     payload.Title,
			Body:      payload.Body,
			Payload:   payload.Payload,
			CreatedAt: time.Now(),
		}
		if err := dispatcher.RedeliverChannel(ctx, n, payload.Channel); err != nil {
			return fmt.Errorf("notification retry: redeliver %q: %w", payload.Channel, err)
		}
		return nil
	})
}
