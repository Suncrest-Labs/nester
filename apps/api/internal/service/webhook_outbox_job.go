package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
)

// WebhookFanoutJobType is the job type the outbox relay routes
// OutboxEventWebhookFanout to (#1049). It exists as a separate step from
// WebhookDeliveryJobType because fan-out needs to read the user's
// subscriptions, and doing that inside the producer's domain transaction
// would put a query nobody needs on the write path of every domain write
// that happens to emit a webhook.
const WebhookFanoutJobType = "webhook_fanout"

// OutboxEventWebhookFanout is the outbox event type for "deliver this
// payload to every subscription of this user that wants it".
const OutboxEventWebhookFanout = "webhook.fanout"

// WebhookFanoutPayload is the outbox payload for a webhook side effect.
// It carries the dedupe key rather than deriving one at fan-out time: the
// key has to be identical across every re-run of this job, and anything
// generated here would not be.
type WebhookFanoutPayload struct {
	UserID    uuid.UUID       `json:"user_id"`
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
	DedupeKey string          `json:"dedupe_key"`
}

// NewWebhookFanoutEvent builds the outbox event a producer inserts inside
// its domain transaction. aggregateType/aggregateID scope the ordering
// guarantee; dedupeKey must be derived from stable inputs (the aggregate and
// what happened to it), never from a UUID generated at call time, or a
// retried producer transaction emits a second, unrecognisable delivery.
func NewWebhookFanoutEvent(
	aggregateType, aggregateID string,
	userID uuid.UUID,
	eventType string,
	payload []byte,
	dedupeKey string,
) (outbox.Event, error) {
	if len(payload) > 0 && !json.Valid(payload) {
		return outbox.Event{}, errors.New("webhook fanout: payload is not valid JSON")
	}
	return outbox.NewEvent(aggregateType, aggregateID, OutboxEventWebhookFanout, dedupeKey, WebhookFanoutPayload{
		UserID:    userID,
		EventType: eventType,
		Payload:   payload,
		DedupeKey: dedupeKey,
	})
}

// WebhookFanoutEnqueuer is the WebhookService surface the fan-out handler
// needs.
type WebhookFanoutEnqueuer interface {
	FanOut(ctx context.Context, userID uuid.UUID, eventType string, payload []byte, dedupeKey string) error
}

// NewWebhookFanoutJobHandler builds the jobqueue.Handler that turns one
// outbox webhook event into one delivery job per matching subscription.
//
// The handler is idempotent, which it must be: the queue is at-least-once,
// so this can run twice for the same event. Every delivery id it produces is
// derived from the outbox dedupe key plus the subscription id, so a second
// run produces exactly the same ids and the delivery enqueues collapse
// instead of duplicating.
func NewWebhookFanoutJobHandler(svc WebhookFanoutEnqueuer, logger *slog.Logger) jobqueue.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
		var p WebhookFanoutPayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return jobqueue.Permanent(fmt.Errorf("webhook fanout: unmarshal payload: %w", err))
		}
		if p.DedupeKey == "" {
			// Without a stable key the fan-out cannot be idempotent, and a
			// retry would double-deliver. Retrying will not add one.
			return jobqueue.Permanent(errors.New("webhook fanout: payload carries no dedupe key"))
		}
		if err := svc.FanOut(ctx, p.UserID, p.EventType, p.Payload, p.DedupeKey); err != nil {
			return fmt.Errorf("webhook fanout: %w", err)
		}
		logger.Debug("webhook fanout complete",
			"user_id", p.UserID, "event_type", p.EventType, "dedupe_key", p.DedupeKey)
		return nil
	})
}

// FanOut enqueues one delivery job per active subscription of userID that is
// subscribed to eventType, deriving each delivery id deterministically from
// dedupeKey so the whole operation is safe to repeat.
//
// This is FireForUser's idempotent sibling. FireForUser generates a fresh
// delivery id per call, which means a caller retry produces a delivery the
// recipient cannot recognise as a repeat; that is exactly the duplicate-side-
// effect failure mode #1049 exists to close, so outbox-driven webhooks go
// through here instead.
//
// Unlike FireForUser it returns an error rather than swallowing one: the
// caller is a queue job, and a failed fan-out has to be retried rather than
// logged and forgotten.
func (s *WebhookService) FanOut(ctx context.Context, userID uuid.UUID, eventType string, payload []byte, dedupeKey string) error {
	if dedupeKey == "" {
		return errors.New("webhook: fan-out requires a dedupe key")
	}
	hooks, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("list subscriptions for %s: %w", userID, err)
	}

	var failures []error
	for _, wh := range hooks {
		if wh.Status != webhook.StatusActive || !wh.Subscribes(eventType) {
			continue
		}
		// Derived, not random: the same (dedupe key, subscription) always
		// yields the same delivery id, in this process and every future one.
		// That is what lets a recipient discard a redelivery, and what lets
		// the queue's idempotency key collapse a repeated fan-out.
		deliveryID := outbox.DeriveID(dedupeKey, wh.ID.String())
		jobPayload := WebhookDeliveryJobPayload{
			WebhookID:  wh.ID,
			DeliveryID: deliveryID,
			EventType:  eventType,
			Payload:    payload,
			DedupeKey:  dedupeKey,
		}
		if _, err := s.enqueue.EnqueueJSON(ctx, WebhookDeliveryJobType, jobPayload,
			jobqueue.WithIdempotencyKey(deliveryID.String()),
			jobqueue.WithCorrelationID(dedupeKey)); err != nil {
			failures = append(failures, fmt.Errorf("enqueue delivery for webhook %s: %w", wh.ID, err))
		}
	}
	if len(failures) > 0 {
		// Returning an error re-runs the whole fan-out. That is safe
		// precisely because every delivery id above is derived: the
		// subscriptions that already enqueued collapse onto their existing
		// jobs, and only the ones that failed actually enqueue.
		return errors.Join(failures...)
	}
	return nil
}
