package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
	"github.com/suncrestlabs/nester/apps/api/internal/webhookssrf"
)

// webhookDeliveryTimeout bounds a single HTTP delivery attempt. Strict on
// purpose: a slow subscriber must not tie up a worker slot indefinitely and
// starve other subscriptions' deliveries.
const webhookDeliveryTimeout = 10 * time.Second

// responseSnippetLimit bounds how much of a delivery's response body is
// stored in the delivery log — enough to debug, not enough to let a
// misbehaving endpoint bloat the log table.
const responseSnippetLimit = 2 * 1024

// webhookDeliveryHTTPClient never follows redirects: a redirect response
// could otherwise bounce a request from a validated public URL to an
// internal address, defeating the SSRF check that just ran (#836's
// technical notes call this out explicitly).
var webhookDeliveryHTTPClient = &http.Client{
	Timeout: webhookDeliveryTimeout,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// WebhookLimiter is the narrow middleware.Limiter surface the delivery job
// handler needs for per-subscription rate limiting.
type WebhookLimiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration)
}

// webhookRateLimited is a transient error: the queue's normal backoff+retry
// handles it, it is never dead-lettered on its own account.
var errWebhookRateLimited = errors.New("webhook delivery rate limited for this subscription")

// SuspensionNotifier is called exactly once when a subscription crosses
// webhook.DeadLetterSuspendThreshold consecutive dead-lettered deliveries.
type SuspensionNotifier interface {
	NotifyWebhookSuspended(ctx context.Context, wh webhook.Webhook)
}

// DispatcherSuspensionNotifier delivers the auto-suspend notification via
// the shared notifications.Dispatcher, matching every other
// Dispatcher*Notifier in this package (see streak_notifier.go,
// goal_milestone_notifier.go).
type DispatcherSuspensionNotifier struct {
	Dispatcher *notifications.Dispatcher
}

func (n DispatcherSuspensionNotifier) NotifyWebhookSuspended(ctx context.Context, wh webhook.Webhook) {
	if n.Dispatcher == nil {
		return
	}
	_ = n.Dispatcher.Send(ctx, wh.UserID, notifications.EventWebhookSubscriptionSuspended,
		"Webhook subscription suspended",
		"Your webhook endpoint has failed repeatedly and been automatically suspended. Re-activate it by registering a new subscription once the issue is fixed.",
		map[string]any{
			"webhook_id":               wh.ID.String(),
			"url":                      wh.URL,
			"consecutive_dead_letters": wh.ConsecutiveDeadLetters,
		},
	)
}

// webhookSSRFValidate matches webhookssrf.Validate's signature. Injectable
// purely so tests can point delivery at an httptest.Server (127.0.0.1,
// normally SSRF-disallowed) without weakening the real check: production
// always wires webhookssrf.Validate itself via
// NewWebhookDeliveryJobHandler, never a stub.
type webhookSSRFValidate func(ctx context.Context, resolver webhookssrf.Resolver, rawURL string) error

// WebhookDeliveryJobHandler adapts webhook delivery to jobqueue.Handler. It
// is registered on the shared worker pool under WebhookDeliveryJobType, so
// retry/backoff/dead-lettering are the queue's, not reimplemented here —
// this handler's job is one attempt: re-validate the target (SSRF defence
// against DNS rebinding — see webhookssrf's doc comment), decrypt the
// secret, sign, send, and log the outcome.
type WebhookDeliveryJobHandler struct {
	webhooks     webhook.Repository
	deliveries   webhook.DeliveryRepository
	cipher       *crypto.AccountCipher
	limiter      WebhookLimiter
	notifier     SuspensionNotifier
	ssrfResolver webhookssrf.Resolver
	ssrfValidate webhookSSRFValidate
	httpClient   *http.Client
	logger       *slog.Logger
}

// cipher must be non-nil in any deployment where webhook subscriptions can
// actually be registered — Register in webhook_service.go already refuses
// to create a subscription without one, so by the time a delivery job
// exists here, a secret was necessarily encrypted with a configured cipher.
func NewWebhookDeliveryJobHandler(
	webhooks webhook.Repository,
	deliveries webhook.DeliveryRepository,
	cipher *crypto.AccountCipher,
	limiter WebhookLimiter,
	notifier SuspensionNotifier,
	logger *slog.Logger,
) *WebhookDeliveryJobHandler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &WebhookDeliveryJobHandler{
		webhooks:     webhooks,
		deliveries:   deliveries,
		cipher:       cipher,
		limiter:      limiter,
		notifier:     notifier,
		ssrfResolver: webhookssrf.DefaultResolver,
		ssrfValidate: webhookssrf.Validate,
		httpClient:   webhookDeliveryHTTPClient,
		logger:       logger,
	}
}

// SetSSRFValidateForTest overrides the SSRF validation function so tests can
// deliver to an httptest.Server (loopback) without weakening the real check
// production uses. Test-only: never call this outside a _test.go file.
func (h *WebhookDeliveryJobHandler) SetSSRFValidateForTest(v func(ctx context.Context, resolver webhookssrf.Resolver, rawURL string) error) {
	h.ssrfValidate = v
}

// SetSSRFResolverForTest overrides the DNS resolver the real
// webhookssrf.Validate uses, so a rebind scenario can be simulated with a
// fake resolver instead of depending on real (flaky, slow) DNS lookups.
func (h *WebhookDeliveryJobHandler) SetSSRFResolverForTest(r webhookssrf.Resolver) {
	h.ssrfResolver = r
}

// Handle executes exactly one delivery attempt. Returning nil marks the
// queue job succeeded; a plain error triggers the queue's normal
// retry-with-backoff; jobqueue.Permanent skips straight to dead-letter
// (used when the subscription itself no longer exists or is suspended —
// retrying cannot help either case).
func (h *WebhookDeliveryJobHandler) Handle(ctx context.Context, job jobqueue.Job) error {
	var p WebhookDeliveryJobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return jobqueue.Permanent(err)
	}

	wh, err := h.webhooks.GetByID(ctx, p.WebhookID)
	if err != nil {
		if errors.Is(err, webhook.ErrWebhookNotFound) {
			// Deleted after the job was enqueued — nothing to deliver to.
			return jobqueue.Permanent(err)
		}
		return err
	}
	if wh.Status != webhook.StatusActive {
		// Suspended between enqueue and now (e.g. a prior delivery in this
		// same burst just crossed the threshold) — do not keep hammering it.
		return jobqueue.Permanent(fmt.Errorf("webhook %s is not active", wh.ID))
	}

	if allowed, retryAfter := h.limiter.Allow(ctx, "webhook:"+wh.ID.String()); !allowed {
		// A rate-limited attempt is not logged as a delivery outcome — it
		// never left the building — and is retried like any transient
		// failure rather than counted toward dead-lettering.
		return fmt.Errorf("%w: retry after %s", errWebhookRateLimited, retryAfter)
	}

	attempt := job.Attempts
	if attempt < 1 {
		attempt = 1
	}

	outcome, deliverErr := h.attemptDelivery(ctx, *wh, p, attempt)

	final := deliverErr == nil
	permanentFailure := job.Attempts >= job.MaxAttempts

	if final {
		outcome.Outcome = webhook.DeliverySucceeded
		if logErr := h.deliveries.Log(ctx, &outcome); logErr != nil {
			h.logger.Error("webhook delivery: log attempt failed", "webhook_id", wh.ID, "error", logErr)
		}
		if _, err := h.webhooks.RecordDeliveryOutcome(ctx, wh.ID, webhook.DeliverySucceeded); err != nil {
			h.logger.Error("webhook delivery: record success failed", "webhook_id", wh.ID, "error", err)
		}
		return nil
	}

	if !permanentFailure {
		// Still has retries left on the queue; not yet a dead-letter.
		outcome.Outcome = webhook.DeliveryFailed
		if logErr := h.deliveries.Log(ctx, &outcome); logErr != nil {
			h.logger.Error("webhook delivery: log attempt failed", "webhook_id", wh.ID, "error", logErr)
		}
		return deliverErr
	}

	// This was the last attempt the queue will make — dead-letter.
	outcome.Outcome = webhook.DeliveryDeadLetter
	if logErr := h.deliveries.Log(ctx, &outcome); logErr != nil {
		h.logger.Error("webhook delivery: log dead-letter failed", "webhook_id", wh.ID, "error", logErr)
	}
	suspended, recErr := h.webhooks.RecordDeliveryOutcome(ctx, wh.ID, webhook.DeliveryDeadLetter)
	if recErr != nil {
		h.logger.Error("webhook delivery: record dead-letter failed", "webhook_id", wh.ID, "error", recErr)
	}
	if suspended && h.notifier != nil {
		suspendedWh := *wh
		suspendedWh.Status = webhook.StatusSuspended
		h.notifier.NotifyWebhookSuspended(ctx, suspendedWh)
	}
	return deliverErr
}

// attemptDelivery performs one HTTP attempt and returns the delivery-log
// row to persist (Outcome is filled in by the caller, which knows whether
// the queue has retries left) plus the error that should drive the queue's
// retry decision (nil on 2xx).
func (h *WebhookDeliveryJobHandler) attemptDelivery(
	ctx context.Context,
	wh webhook.Webhook,
	p WebhookDeliveryJobPayload,
	attempt int,
) (webhook.Delivery, error) {
	rec := webhook.Delivery{
		WebhookID:  wh.ID,
		DeliveryID: p.DeliveryID,
		EventType:  p.EventType,
		Payload:    p.Payload,
		Attempt:    attempt,
	}

	// Re-validated here, not just at registration: DNS can change between
	// registration and send time (rebinding) — see webhookssrf's doc
	// comment. A target that now resolves privately is a permanent failure
	// for this delivery, not a transient one, but it is intentionally still
	// routed through the same dead-letter/log path as any other failure
	// rather than silently dropped, so the owner can see it in their log.
	if err := h.ssrfValidate(ctx, h.ssrfResolver, wh.URL); err != nil {
		rec.Error = "target failed SSRF validation at send time: " + err.Error()
		return rec, errors.New(rec.Error)
	}

	secret, err := h.cipher.Decrypt(crypto.CipherEnvelope{KeyVersion: wh.SecretKeyVersion, Ciphertext: wh.SecretCiphertext})
	if err != nil {
		rec.Error = "failed to decrypt signing secret: " + err.Error()
		return rec, errors.New(rec.Error)
	}

	// The body sent on the wire is what gets signed, so the dedupe key is
	// injected before signing rather than after. Recipients that verify the
	// signature over the raw body they received therefore still verify.
	signedBody := withDedupeKey(p.Payload, p.DedupeKey)
	rec.Payload = signedBody

	timestamp := time.Now().Unix()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(signedBody))
	if err != nil {
		rec.Error = err.Error()
		return rec, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Nester-Signature", NewWebhookSignatureHeader([]byte(secret), timestamp, signedBody))
	req.Header.Set("X-Nester-Delivery-Id", p.DeliveryID.String())
	req.Header.Set("X-Nester-Event", p.EventType)
	if p.DedupeKey != "" {
		req.Header.Set("X-Nester-Dedupe-Key", p.DedupeKey)
	}

	start := time.Now()
	resp, err := h.httpClient.Do(req)
	rec.DurationMS = int(time.Since(start).Milliseconds())
	if err != nil {
		rec.Error = err.Error()
		return rec, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, responseSnippetLimit))
	rec.ResponseBodySnippet = string(body)
	status := resp.StatusCode
	rec.ResponseStatus = &status

	if status >= 200 && status < 300 {
		return rec, nil
	}
	rec.Error = fmt.Sprintf("non-2xx response: %d", status)
	return rec, errors.New(rec.Error)
}

// withDedupeKey returns payload with a top-level "dedupe_key" field added, so
// a recipient can dedupe from the body alone without reading headers — a
// queue consumer that only ever sees the persisted body has no headers left
// to read (#1049 asks for the key in both places for exactly that reason).
//
// It is deliberately conservative: a payload that is not a JSON object, or
// that already carries the field, is returned untouched. Silently rewriting
// a body we do not understand would be worse than omitting the field, since
// the header carries it either way.
func withDedupeKey(payload []byte, dedupeKey string) []byte {
	if dedupeKey == "" || len(payload) == 0 {
		return payload
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(payload, &obj); err != nil || obj == nil {
		return payload
	}
	if _, exists := obj["dedupe_key"]; exists {
		return payload
	}
	encoded, err := json.Marshal(dedupeKey)
	if err != nil {
		return payload
	}
	obj["dedupe_key"] = encoded
	merged, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return merged
}
