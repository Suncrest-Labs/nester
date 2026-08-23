package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
	"github.com/suncrestlabs/nester/apps/api/internal/webhookssrf"
)

// maxWebhookDeliveriesLimit caps ListDeliveries' page size regardless of what
// a caller requests — without this, ?limit=10000000 becomes one unbounded
// query plus full JSON serialisation.
const maxWebhookDeliveriesLimit = 200

// WebhookDeliveryJobType is the job-queue type webhook deliveries are
// enqueued under (#836), driven by the same durable at-least-once worker
// pool (retry+backoff+dead-letter) every other producer in this codebase
// uses — see WebhookDeliveryJobHandler in webhook_delivery_job.go.
const WebhookDeliveryJobType = "webhook_delivery"

// ErrWebhookCipherNotConfigured is returned when no AccountCipher is wired
// (cfg.AccountCipher().Configured() is false — see main.go). Webhooks are
// unusable without it since secrets can never be encrypted at rest; this
// mirrors bankaccount_service.go's identical nil-cipher convention rather
// than failing the whole process at startup over an optional deployment
// config.
var ErrWebhookCipherNotConfigured = errors.New("webhook signing secret cipher is not configured")

// WebhookDeliveryEnqueuer is the narrow jobqueue.Client surface the service
// needs to enqueue a delivery job per matching subscription.
type WebhookDeliveryEnqueuer interface {
	EnqueueJSON(ctx context.Context, jobType string, payload any, opts ...jobqueue.EnqueueOption) (jobqueue.Job, error)
}

// WebhookDeliveryJobPayload is the job-queue payload the delivery job
// handler decodes. DeliveryID is generated once here (not per-attempt) so
// every retry of the same logical delivery carries the same id — the
// dedup key recipients are told to key on.
type WebhookDeliveryJobPayload struct {
	WebhookID  uuid.UUID `json:"webhook_id"`
	DeliveryID uuid.UUID `json:"delivery_id"`
	EventType  string    `json:"event_type"`
	Payload    []byte    `json:"payload"`
	// DedupeKey is the transactional outbox's key for the side effect this
	// delivery carries (#1049). It is stable across every redelivery of the
	// same logical event — including across the fan-out job re-running —
	// and is surfaced to the recipient in both the body and a header so
	// they can discard repeats. Empty for deliveries that did not originate
	// in the outbox (manual redelivery, and FireForUser's legacy path).
	DedupeKey string `json:"dedupe_key,omitempty"`
}

type WebhookService struct {
	repo         webhook.Repository
	deliveries   webhook.DeliveryRepository
	cipher       *crypto.AccountCipher
	enqueue      WebhookDeliveryEnqueuer
	ssrfResolver webhookssrf.Resolver
	logger       *slog.Logger
}

// cipher may be nil when AccountCipher is not configured for this
// deployment; Register fails with ErrWebhookCipherNotConfigured rather than
// panicking, matching bankaccount_service.go's convention.
func NewWebhookService(repo webhook.Repository, deliveries webhook.DeliveryRepository, cipher *crypto.AccountCipher, enqueue WebhookDeliveryEnqueuer) *WebhookService {
	return &WebhookService{
		repo:         repo,
		deliveries:   deliveries,
		cipher:       cipher,
		enqueue:      enqueue,
		ssrfResolver: webhookssrf.DefaultResolver,
		logger:       slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

// SetLogger wires a real logger. Call this from main after construction;
// without it, FireForUser's enqueue failures are discarded silently rather
// than merely unlogged.
func (s *WebhookService) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

type RegisterWebhookInput struct {
	URL string `json:"url"`
	// EventTypes filters which events this subscription receives. Empty
	// means "all events" (webhook.Webhook.Subscribes' default).
	EventTypes []string `json:"event_types"`
}

// Register validates the target (SSRF defence — see webhookssrf.Validate's
// doc comment on why this alone is not sufficient and send-time
// re-validation also matters), generates a signing secret, encrypts it at
// rest, and returns the subscription with Secret populated — the only time
// it is ever returned in plaintext.
func (s *WebhookService) Register(ctx context.Context, userID uuid.UUID, in RegisterWebhookInput) (webhook.Webhook, error) {
	u := strings.TrimSpace(in.URL)
	if u == "" {
		return webhook.Webhook{}, fmt.Errorf("%w: url is required", webhook.ErrInvalidWebhook)
	}
	if err := webhookssrf.Validate(ctx, s.ssrfResolver, u); err != nil {
		return webhook.Webhook{}, fmt.Errorf("%w: %v", webhook.ErrInvalidWebhook, err)
	}
	if s.cipher == nil {
		return webhook.Webhook{}, ErrWebhookCipherNotConfigured
	}

	secret, err := generateWebhookSecret()
	if err != nil {
		return webhook.Webhook{}, fmt.Errorf("generate webhook secret: %w", err)
	}
	envelope, err := s.cipher.Encrypt(secret)
	if err != nil {
		return webhook.Webhook{}, fmt.Errorf("encrypt webhook secret: %w", err)
	}

	wh := &webhook.Webhook{
		ID:               uuid.New(),
		UserID:           userID,
		URL:              u,
		EventTypes:       in.EventTypes,
		Status:           webhook.StatusActive,
		SecretCiphertext: envelope.Ciphertext,
		SecretKeyVersion: envelope.KeyVersion,
	}
	if err := s.repo.Create(ctx, wh); err != nil {
		return webhook.Webhook{}, err
	}
	wh.Secret = secret // shown once; never persisted or returned again
	return *wh, nil
}

func (s *WebhookService) List(ctx context.Context, userID uuid.UUID) ([]webhook.Webhook, error) {
	return s.repo.ListByUser(ctx, userID)
}

func (s *WebhookService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, userID)
}

// FireForUser enqueues one durable delivery job per subscription owned by
// userID that is active and subscribed to eventType. event is the event
// name (e.g. "goal.milestone.50"); payload is the JSON body bytes. Delivery
// itself — including retry/backoff/dead-letter — happens on the job-queue
// worker (WebhookDeliveryJobType), not inline: this call only enqueues.
func (s *WebhookService) FireForUser(ctx context.Context, userID uuid.UUID, eventType string, payload []byte) {
	hooks, err := s.repo.ListByUser(ctx, userID)
	if err != nil || len(hooks) == 0 {
		return
	}
	for _, wh := range hooks {
		if wh.Status != webhook.StatusActive || !wh.Subscribes(eventType) {
			continue
		}
		jobPayload := WebhookDeliveryJobPayload{
			WebhookID:  wh.ID,
			DeliveryID: uuid.New(),
			EventType:  eventType,
			Payload:    payload,
		}
		// DeliveryID is freshly generated above on every call, so this
		// idempotency key only guards against double-enqueuing this exact
		// in-flight payload instance (e.g. a duplicate EnqueueJSON call
		// within this loop) — it does NOT dedupe a caller retry of the same
		// logical event, since that retry would go through FireForUser
		// again with a new DeliveryID. Deduping across caller retries would
		// require deriving the key from stable inputs (subscription ID +
		// event type + a caller-supplied event ID) instead.
		if _, err := s.enqueue.EnqueueJSON(ctx, WebhookDeliveryJobType, jobPayload,
			jobqueue.WithIdempotencyKey(jobPayload.DeliveryID.String())); err != nil {
			s.logger.Error("webhook: failed to enqueue delivery", "webhook_id", wh.ID, "event_type", eventType, "error", err)
		}
	}
}

// ListDeliveries returns the delivery log for a subscription owned by
// userID (#836's "expose it to the subscription owner" requirement).
// Ownership is checked via GetByID rather than trusting webhookID alone, so
// one user cannot read another's delivery log by guessing an id.
func (s *WebhookService) ListDeliveries(ctx context.Context, userID, webhookID uuid.UUID, limit int) ([]webhook.Delivery, error) {
	wh, err := s.repo.GetByID(ctx, webhookID)
	if err != nil {
		return nil, err
	}
	if wh.UserID != userID {
		return nil, webhook.ErrWebhookNotFound
	}
	if limit <= 0 || limit > maxWebhookDeliveriesLimit {
		limit = maxWebhookDeliveriesLimit
	}
	return s.deliveries.ListByWebhook(ctx, webhookID, limit)
}

// Redeliver re-enqueues a past delivery under a fresh DeliveryID (#836's
// manual redelivery action). A new id rather than reusing the original is
// deliberate: this is a new attempt chain the owner explicitly asked for,
// not a retry of the original one, so recipients that already deduped the
// original delivery must still see this one.
func (s *WebhookService) Redeliver(ctx context.Context, userID, deliveryID uuid.UUID) error {
	d, err := s.deliveries.GetByID(ctx, deliveryID)
	if err != nil {
		return err
	}
	wh, err := s.repo.GetByID(ctx, d.WebhookID)
	if err != nil {
		return err
	}
	if wh.UserID != userID {
		return webhook.ErrWebhookNotFound
	}
	if wh.Status != webhook.StatusActive {
		return fmt.Errorf("%w: subscription is suspended", webhook.ErrInvalidWebhook)
	}

	jobPayload := WebhookDeliveryJobPayload{
		WebhookID:  wh.ID,
		DeliveryID: uuid.New(),
		EventType:  d.EventType,
		Payload:    d.Payload,
	}
	_, err = s.enqueue.EnqueueJSON(ctx, WebhookDeliveryJobType, jobPayload,
		jobqueue.WithIdempotencyKey(jobPayload.DeliveryID.String()))
	return err
}

func generateWebhookSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "whsec_" + hex.EncodeToString(b), nil
}

// BuildWebhookPayload constructs the standard JSON payload for webhook delivery.
func BuildWebhookPayload(event string, goalID, userID uuid.UUID, milestonePct int, currentAmount, targetAmount string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"event":          event,
		"goal_id":        goalID.String(),
		"user_id":        userID.String(),
		"milestone_pct":  milestonePct,
		"current_amount": currentAmount,
		"target_amount":  targetAmount,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
	})
}
