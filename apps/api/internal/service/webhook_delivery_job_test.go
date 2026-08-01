package service_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/crypto"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/internal/webhookssrf"
)

// allowAllSSRF is a permissive SSRF-validate stub so tests can deliver to an
// httptest.Server (127.0.0.1, normally SSRF-disallowed) without depending on
// the real check. Production always uses the real webhookssrf.Validate,
// wired by NewWebhookDeliveryJobHandler — this stub only ever appears here.
func allowAllSSRF(context.Context, webhookssrf.Resolver, string) error { return nil }

// fakeRebindResolver simulates DNS rebinding: it resolves the given host to
// a private address, the way an attacker-controlled DNS record would after
// registration-time validation already passed against a public address.
type fakeRebindResolver struct{ host string }

func (f fakeRebindResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if host == f.host {
		return []net.IPAddr{{IP: net.ParseIP("10.0.0.1")}}, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: host}
}

// --- fakes ---

type fakeWebhookRepo struct {
	mu           sync.Mutex
	byID         map[uuid.UUID]webhook.Webhook
	dlCounts     map[uuid.UUID]int
	suspendCalls []uuid.UUID
}

func newFakeWebhookRepo(hooks ...webhook.Webhook) *fakeWebhookRepo {
	r := &fakeWebhookRepo{byID: map[uuid.UUID]webhook.Webhook{}, dlCounts: map[uuid.UUID]int{}}
	for _, h := range hooks {
		r.byID[h.ID] = h
	}
	return r
}

func (r *fakeWebhookRepo) Create(_ context.Context, wh *webhook.Webhook) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[wh.ID] = *wh
	return nil
}
func (r *fakeWebhookRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]webhook.Webhook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []webhook.Webhook
	for _, h := range r.byID {
		if h.UserID == userID {
			out = append(out, h)
		}
	}
	return out, nil
}
func (r *fakeWebhookRepo) GetByID(_ context.Context, id uuid.UUID) (*webhook.Webhook, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.byID[id]
	if !ok {
		return nil, webhook.ErrWebhookNotFound
	}
	return &h, nil
}
func (r *fakeWebhookRepo) Delete(_ context.Context, id, _ uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byID, id)
	return nil
}
func (r *fakeWebhookRepo) RecordDeliveryOutcome(_ context.Context, webhookID uuid.UUID, outcome webhook.DeliveryOutcome) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if outcome == webhook.DeliverySucceeded {
		r.dlCounts[webhookID] = 0
		return false, nil
	}
	if outcome != webhook.DeliveryDeadLetter {
		return false, nil
	}
	r.dlCounts[webhookID]++
	if r.dlCounts[webhookID] == webhook.DeadLetterSuspendThreshold {
		h := r.byID[webhookID]
		h.Status = webhook.StatusSuspended
		r.byID[webhookID] = h
		r.suspendCalls = append(r.suspendCalls, webhookID)
		return true, nil
	}
	return false, nil
}

type fakeDeliveryRepo struct {
	mu        sync.Mutex
	log       []webhook.Delivery
	lastLimit int // limit received by the most recent ListByWebhook call
}

func (r *fakeDeliveryRepo) Log(_ context.Context, d *webhook.Delivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	r.log = append(r.log, *d)
	return nil
}
func (r *fakeDeliveryRepo) ListByWebhook(_ context.Context, webhookID uuid.UUID, limit int) ([]webhook.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastLimit = limit
	var out []webhook.Delivery
	for _, d := range r.log {
		if d.WebhookID == webhookID {
			out = append(out, d)
		}
	}
	return out, nil
}
func (r *fakeDeliveryRepo) GetByID(_ context.Context, id uuid.UUID) (*webhook.Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.log {
		if d.ID == id {
			return &d, nil
		}
	}
	return nil, webhook.ErrDeliveryNotFound
}

func (r *fakeDeliveryRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.log)
}

type allowAllLimiter struct{}

func (allowAllLimiter) Allow(context.Context, string) (bool, time.Duration) { return true, 0 }

type denyLimiter struct{}

func (denyLimiter) Allow(context.Context, string) (bool, time.Duration) { return false, time.Second }

type recordingNotifier struct {
	mu       sync.Mutex
	notified []webhook.Webhook
}

func (n *recordingNotifier) NotifyWebhookSuspended(_ context.Context, wh webhook.Webhook) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notified = append(n.notified, wh)
}

// testCipher is defined once in bankaccount_service_test.go and reused here.

func sealSecret(t *testing.T, c *crypto.AccountCipher, secret string) (ciphertext []byte, keyVersion string) {
	t.Helper()
	env, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	return env.Ciphertext, env.KeyVersion
}

func jobFor(t *testing.T, payload service.WebhookDeliveryJobPayload, attempts, maxAttempts int) jobqueue.Job {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return jobqueue.Job{ID: uuid.New(), Type: service.WebhookDeliveryJobType, Payload: raw, Attempts: attempts, MaxAttempts: maxAttempts}
}

// --- tests ---

func TestWebhookDeliveryJobHandler_SuccessfulDelivery(t *testing.T) {
	var gotSig, gotDeliveryID, gotEvent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Nester-Signature")
		gotDeliveryID = r.Header.Get("X-Nester-Delivery-Id")
		gotEvent = r.Header.Get("X-Nester-Event")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cipher := testCipher(t)
	ciphertext, keyVersion := sealSecret(t, cipher, "whsec_test")
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL, Status: webhook.StatusActive, SecretCiphertext: ciphertext, SecretKeyVersion: keyVersion}
	repo := newFakeWebhookRepo(wh)
	deliveries := &fakeDeliveryRepo{}

	h := service.NewWebhookDeliveryJobHandler(repo, deliveries, cipher, allowAllLimiter{}, nil, nil)
	h.SetSSRFValidateForTest(allowAllSSRF)

	payload := service.WebhookDeliveryJobPayload{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "goal.milestone.50", Payload: []byte(`{"a":1}`)}
	job := jobFor(t, payload, 1, 5)

	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSig == "" || !strings.HasPrefix(gotSig, "t=") {
		t.Errorf("expected signature header shaped like t=...,v1=..., got %q", gotSig)
	}
	if gotDeliveryID != payload.DeliveryID.String() {
		t.Errorf("delivery id header = %q, want %q", gotDeliveryID, payload.DeliveryID)
	}
	if gotEvent != "goal.milestone.50" {
		t.Errorf("event header = %q", gotEvent)
	}
	if deliveries.count() != 1 {
		t.Fatalf("expected the successful attempt to be logged, got %d log entries", deliveries.count())
	}
	if got := deliveries.log[0].Outcome; got != webhook.DeliverySucceeded {
		t.Errorf("logged delivery outcome = %q, want %q", got, webhook.DeliverySucceeded)
	}
}

func TestWebhookDeliveryJobHandler_NonRetryableWhenWebhookDeleted(t *testing.T) {
	repo := newFakeWebhookRepo() // empty: webhook was deleted
	deliveries := &fakeDeliveryRepo{}
	cipher := testCipher(t)

	h := service.NewWebhookDeliveryJobHandler(repo, deliveries, cipher, allowAllLimiter{}, nil, nil)
	payload := service.WebhookDeliveryJobPayload{WebhookID: uuid.New(), DeliveryID: uuid.New(), EventType: "x", Payload: []byte(`{}`)}
	job := jobFor(t, payload, 1, 5)

	err := h.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected an error for a missing webhook")
	}
	if !jobqueue.IsPermanent(err) {
		t.Errorf("expected a permanent (non-retryable) error, got %v", err)
	}
}

func TestWebhookDeliveryJobHandler_SkipsSuspendedSubscription(t *testing.T) {
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: "https://example.invalid/hook", Status: webhook.StatusSuspended}
	repo := newFakeWebhookRepo(wh)
	deliveries := &fakeDeliveryRepo{}
	cipher := testCipher(t)

	h := service.NewWebhookDeliveryJobHandler(repo, deliveries, cipher, allowAllLimiter{}, nil, nil)
	payload := service.WebhookDeliveryJobPayload{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Payload: []byte(`{}`)}
	job := jobFor(t, payload, 1, 5)

	err := h.Handle(context.Background(), job)
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("expected a permanent error for a suspended subscription, got %v", err)
	}
	if deliveries.count() != 0 {
		t.Errorf("expected no delivery log entry for a subscription skipped before send, got %d", deliveries.count())
	}
}

func TestWebhookDeliveryJobHandler_RejectsSendTimeSSRFRebind(t *testing.T) {
	// Simulates DNS rebinding: the URL is a hostname that now resolves
	// privately, even though it would have passed registration-time
	// validation when it resolved publicly.
	cipher := testCipher(t)
	ciphertext, keyVersion := sealSecret(t, cipher, "whsec_test")
	wh := webhook.Webhook{
		ID: uuid.New(), UserID: uuid.New(), URL: "https://rebind.example.com/hook",
		Status: webhook.StatusActive, SecretCiphertext: ciphertext, SecretKeyVersion: keyVersion,
	}
	repo := newFakeWebhookRepo(wh)
	deliveries := &fakeDeliveryRepo{}

	h := service.NewWebhookDeliveryJobHandler(repo, deliveries, cipher, allowAllLimiter{}, nil, nil)
	// Real webhookssrf.Validate (not stubbed) with a fake resolver that
	// simulates the rebind: the hostname now resolves to a private address.
	h.SetSSRFResolverForTest(fakeRebindResolver{host: "rebind.example.com"})

	payload := service.WebhookDeliveryJobPayload{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Payload: []byte(`{}`)}
	job := jobFor(t, payload, 5, 5) // last attempt -> dead-letters and logs

	err := h.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected an error for an SSRF-disallowed target at send time")
	}
	if deliveries.count() != 1 {
		t.Fatalf("expected 1 logged (dead-lettered) attempt, got %d", deliveries.count())
	}
	if deliveries.log[0].Outcome != webhook.DeliveryDeadLetter {
		t.Errorf("outcome = %q, want dead_letter", deliveries.log[0].Outcome)
	}
	if !strings.Contains(deliveries.log[0].Error, "SSRF") {
		t.Errorf("expected the logged error to mention SSRF, got %q", deliveries.log[0].Error)
	}
}

func TestWebhookDeliveryJobHandler_FailureBeforeLastAttemptRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cipher := testCipher(t)
	ciphertext, keyVersion := sealSecret(t, cipher, "whsec_test")
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL, Status: webhook.StatusActive, SecretCiphertext: ciphertext, SecretKeyVersion: keyVersion}
	repo := newFakeWebhookRepo(wh)
	deliveries := &fakeDeliveryRepo{}

	h := service.NewWebhookDeliveryJobHandler(repo, deliveries, cipher, allowAllLimiter{}, nil, nil)
	h.SetSSRFValidateForTest(allowAllSSRF)

	payload := service.WebhookDeliveryJobPayload{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Payload: []byte(`{}`)}
	job := jobFor(t, payload, 1, 5) // attempt 1 of 5 -> not yet exhausted

	err := h.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected an error on 500 response")
	}
	if jobqueue.IsPermanent(err) {
		t.Error("a transient HTTP failure with retries remaining must not be permanent")
	}
	if deliveries.count() != 1 || deliveries.log[0].Outcome != webhook.DeliveryFailed {
		t.Fatalf("expected 1 logged 'failed' (not dead-lettered) attempt, got %+v", deliveries.log)
	}
}

func TestWebhookDeliveryJobHandler_DeadLetterAfterExhaustingRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cipher := testCipher(t)
	ciphertext, keyVersion := sealSecret(t, cipher, "whsec_test")
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL, Status: webhook.StatusActive, SecretCiphertext: ciphertext, SecretKeyVersion: keyVersion}
	repo := newFakeWebhookRepo(wh)
	deliveries := &fakeDeliveryRepo{}

	h := service.NewWebhookDeliveryJobHandler(repo, deliveries, cipher, allowAllLimiter{}, nil, nil)
	h.SetSSRFValidateForTest(allowAllSSRF)

	payload := service.WebhookDeliveryJobPayload{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Payload: []byte(`{}`)}
	job := jobFor(t, payload, 5, 5) // final attempt

	if err := h.Handle(context.Background(), job); err == nil {
		t.Fatal("expected an error on the final failing attempt")
	}
	if deliveries.count() != 1 || deliveries.log[0].Outcome != webhook.DeliveryDeadLetter {
		t.Fatalf("expected the final attempt logged as dead_letter, got %+v", deliveries.log)
	}
}

func TestWebhookDeliveryJobHandler_AutoSuspendsAfterThresholdAndNotifiesOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cipher := testCipher(t)
	ciphertext, keyVersion := sealSecret(t, cipher, "whsec_test")
	whID := uuid.New()
	wh := webhook.Webhook{ID: whID, UserID: uuid.New(), URL: srv.URL, Status: webhook.StatusActive, SecretCiphertext: ciphertext, SecretKeyVersion: keyVersion}
	repo := newFakeWebhookRepo(wh)
	deliveries := &fakeDeliveryRepo{}
	notifier := &recordingNotifier{}

	h := service.NewWebhookDeliveryJobHandler(repo, deliveries, cipher, allowAllLimiter{}, notifier, nil)
	h.SetSSRFValidateForTest(allowAllSSRF)

	// Drive webhook.DeadLetterSuspendThreshold separate deliveries, each
	// dead-lettered on its own final attempt (5 of 5) — simulating repeated
	// independent event deliveries all failing, not retries of one event.
	for i := 0; i < webhook.DeadLetterSuspendThreshold; i++ {
		payload := service.WebhookDeliveryJobPayload{WebhookID: whID, DeliveryID: uuid.New(), EventType: "x", Payload: []byte(`{}`)}
		job := jobFor(t, payload, 5, 5)
		_ = h.Handle(context.Background(), job)
	}

	updated, err := repo.GetByID(context.Background(), whID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if updated.Status != webhook.StatusSuspended {
		t.Fatalf("expected the subscription to be auto-suspended after %d consecutive dead-letters, got status=%q", webhook.DeadLetterSuspendThreshold, updated.Status)
	}
	notifier.mu.Lock()
	notifyCount := len(notifier.notified)
	notifier.mu.Unlock()
	if notifyCount != 1 {
		t.Errorf("expected exactly 1 suspension notification, got %d", notifyCount)
	}
}

func TestWebhookDeliveryJobHandler_RateLimitedRetriesWithoutLoggingOrCountingAsDeadLetter(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cipher := testCipher(t)
	ciphertext, keyVersion := sealSecret(t, cipher, "whsec_test")
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL, Status: webhook.StatusActive, SecretCiphertext: ciphertext, SecretKeyVersion: keyVersion}
	repo := newFakeWebhookRepo(wh)
	deliveries := &fakeDeliveryRepo{}

	h := service.NewWebhookDeliveryJobHandler(repo, deliveries, cipher, denyLimiter{}, nil, nil)
	h.SetSSRFValidateForTest(allowAllSSRF)

	payload := service.WebhookDeliveryJobPayload{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Payload: []byte(`{}`)}
	job := jobFor(t, payload, 5, 5) // even on the "final" attempt, rate limiting must not dead-letter

	err := h.Handle(context.Background(), job)
	if err == nil {
		t.Fatal("expected a transient error when rate limited")
	}
	if jobqueue.IsPermanent(err) {
		t.Error("rate limiting must never be a permanent failure")
	}
	if calls.Load() != 0 {
		t.Errorf("expected the handler under a rate limit to never call the endpoint, got %d calls", calls.Load())
	}
	if deliveries.count() != 0 {
		t.Errorf("expected rate-limited attempts to not be logged as delivery outcomes, got %d", deliveries.count())
	}
}

func TestWebhookSigning_RecomputedSignatureMatches(t *testing.T) {
	secret := []byte("s3cr3t")
	payload := []byte(`{"event":"x"}`)
	ts := time.Now().Unix()
	header := service.NewWebhookSignatureHeader(secret, ts, payload)

	parts := strings.Split(header, ",")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "t=") || !strings.HasPrefix(parts[1], "v1=") {
		t.Fatalf("unexpected header shape: %q", header)
	}
	gotTS, err := strconv.ParseInt(strings.TrimPrefix(parts[0], "t="), 10, 64)
	if err != nil {
		t.Fatalf("parse timestamp: %v", err)
	}
	gotSig := strings.TrimPrefix(parts[1], "v1=")

	if !service.VerifyWebhookSignature(secret, gotTS, payload, gotSig) {
		t.Error("recomputed signature did not match")
	}
}

func TestWebhookSigning_DifferentSecretFailsVerification(t *testing.T) {
	payload := []byte(`{"event":"x"}`)
	ts := time.Now().Unix()
	sig := service.SignWebhookPayload([]byte("secret-a"), ts, payload)
	if service.VerifyWebhookSignature([]byte("secret-b"), ts, payload, sig) {
		t.Error("expected verification to fail with the wrong secret")
	}
}

func TestWebhookSigning_DifferentTimestampFailsVerification(t *testing.T) {
	// The MAC covers the timestamp, not just the payload — a captured
	// signature must not verify against a replayed, different timestamp.
	secret := []byte("s3cr3t")
	payload := []byte(`{"event":"x"}`)
	sig := service.SignWebhookPayload(secret, 1000, payload)
	if service.VerifyWebhookSignature(secret, 2000, payload, sig) {
		t.Error("expected verification to fail when the timestamp differs from what was signed")
	}
}
