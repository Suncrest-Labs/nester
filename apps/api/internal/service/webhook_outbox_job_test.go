package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// TestFanOut_DeliveryIDsAreDerivedFromTheDedupeKey is the acceptance
// criterion "duplicate delivery carries a stable dedupe key". Running the
// fan-out twice — which the at-least-once queue will do — must produce the
// same delivery ids, or the recipient sees two unrelated-looking deliveries
// for one event and a payout consumer double-pays.
func TestFanOut_DeliveryIDsAreDerivedFromTheDedupeKey(t *testing.T) {
	userID := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	svc, _, _, enqueuer := newServiceForTest(t, wh)
	ctx := context.Background()

	const dedupeKey = "savings_goal:goal-1:milestone:50"
	if err := svc.FanOut(ctx, userID, "goal.milestone.50", []byte(`{}`), dedupeKey); err != nil {
		t.Fatalf("first FanOut: %v", err)
	}
	if err := svc.FanOut(ctx, userID, "goal.milestone.50", []byte(`{}`), dedupeKey); err != nil {
		t.Fatalf("second FanOut: %v", err)
	}

	if enqueuer.count() != 2 {
		t.Fatalf("enqueue calls = %d, want 2", enqueuer.count())
	}
	first, second := enqueuer.calls[0].payload, enqueuer.calls[1].payload
	if first.DeliveryID != second.DeliveryID {
		t.Fatalf("delivery ids differ across runs: %s != %s", first.DeliveryID, second.DeliveryID)
	}
	if want := outbox.DeriveID(dedupeKey, wh.ID.String()); first.DeliveryID != want {
		t.Fatalf("delivery id = %s, want the derived %s", first.DeliveryID, want)
	}
	if first.DedupeKey != dedupeKey {
		t.Fatalf("payload dedupe key = %q, want %q", first.DedupeKey, dedupeKey)
	}
}

// TestFanOut_DerivesADistinctIDPerSubscription: two subscribers must not
// share a delivery id, or one recipient's dedupe store suppresses the
// other's delivery if they ever compare notes (and the queue's idempotency
// key would collapse the two enqueues into one).
func TestFanOut_DerivesADistinctIDPerSubscription(t *testing.T) {
	userID := uuid.New()
	a := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	b := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://b.example.com/hook", Status: webhook.StatusActive}
	svc, _, _, enqueuer := newServiceForTest(t, a, b)

	if err := svc.FanOut(context.Background(), userID, "x", []byte(`{}`), "key-1"); err != nil {
		t.Fatalf("FanOut: %v", err)
	}
	if enqueuer.count() != 2 {
		t.Fatalf("enqueue calls = %d, want 2", enqueuer.count())
	}
	if enqueuer.calls[0].payload.DeliveryID == enqueuer.calls[1].payload.DeliveryID {
		t.Fatal("two subscriptions received the same delivery id")
	}
}

func TestFanOut_SkipsSuspendedAndUnsubscribed(t *testing.T) {
	userID := uuid.New()
	active := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://a.example.com/hook", Status: webhook.StatusActive, EventTypes: []string{"goal.milestone.50"}}
	suspended := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://b.example.com/hook", Status: webhook.StatusSuspended}
	filtered := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://c.example.com/hook", Status: webhook.StatusActive, EventTypes: []string{"other.event"}}
	svc, _, _, enqueuer := newServiceForTest(t, active, suspended, filtered)

	if err := svc.FanOut(context.Background(), userID, "goal.milestone.50", []byte(`{}`), "key-1"); err != nil {
		t.Fatalf("FanOut: %v", err)
	}
	if enqueuer.count() != 1 {
		t.Fatalf("enqueue calls = %d, want 1", enqueuer.count())
	}
	if enqueuer.calls[0].payload.WebhookID != active.ID {
		t.Fatalf("delivered to %s, want %s", enqueuer.calls[0].payload.WebhookID, active.ID)
	}
}

// TestFanOut_ReturnsErrorSoTheQueueRetries: unlike FireForUser, whose caller
// is a request handler with nowhere to put an error, this one is called by a
// queue job — a swallowed failure here is a lost side effect.
func TestFanOut_ReturnsErrorSoTheQueueRetries(t *testing.T) {
	userID := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	svc, _, _, enqueuer := newServiceForTest(t, wh)
	enqueuer.err = errors.New("queue unavailable")

	if err := svc.FanOut(context.Background(), userID, "x", []byte(`{}`), "key-1"); err == nil {
		t.Fatal("FanOut swallowed an enqueue failure; the queue would mark the job succeeded")
	}
}

func TestFanOut_RequiresADedupeKey(t *testing.T) {
	userID := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	svc, _, _, enqueuer := newServiceForTest(t, wh)

	if err := svc.FanOut(context.Background(), userID, "x", []byte(`{}`), ""); err == nil {
		t.Fatal("FanOut without a dedupe key succeeded; it cannot be idempotent")
	}
	if enqueuer.count() != 0 {
		t.Fatalf("enqueued %d deliveries without a dedupe key, want 0", enqueuer.count())
	}
}

// --- fan-out job handler ---

type recordingFanout struct {
	mu    sync.Mutex
	calls []struct {
		userID    uuid.UUID
		eventType string
		dedupeKey string
	}
	err error
}

func (f *recordingFanout) FanOut(_ context.Context, userID uuid.UUID, eventType string, _ []byte, dedupeKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct {
		userID    uuid.UUID
		eventType string
		dedupeKey string
	}{userID, eventType, dedupeKey})
	return f.err
}

func fanoutJob(t *testing.T, p service.WebhookFanoutPayload) jobqueue.Job {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return jobqueue.Job{ID: uuid.New(), Type: service.WebhookFanoutJobType, Payload: raw}
}

func TestWebhookFanoutJobHandler_DelegatesToFanOut(t *testing.T) {
	fanout := &recordingFanout{}
	h := service.NewWebhookFanoutJobHandler(fanout, nil)
	userID := uuid.New()

	err := h.Handle(context.Background(), fanoutJob(t, service.WebhookFanoutPayload{
		UserID:    userID,
		EventType: "goal.milestone.50",
		Payload:   json.RawMessage(`{"a":1}`),
		DedupeKey: "key-1",
	}))
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(fanout.calls) != 1 {
		t.Fatalf("fan-out calls = %d, want 1", len(fanout.calls))
	}
	got := fanout.calls[0]
	if got.userID != userID || got.eventType != "goal.milestone.50" || got.dedupeKey != "key-1" {
		t.Fatalf("fan-out called with %+v, want the payload's values", got)
	}
}

func TestWebhookFanoutJobHandler_MalformedPayloadIsPermanent(t *testing.T) {
	h := service.NewWebhookFanoutJobHandler(&recordingFanout{}, nil)
	err := h.Handle(context.Background(), jobqueue.Job{Payload: json.RawMessage(`{oops`)})
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want a permanent failure — retrying cannot fix a malformed payload", err)
	}
}

func TestWebhookFanoutJobHandler_MissingDedupeKeyIsPermanent(t *testing.T) {
	h := service.NewWebhookFanoutJobHandler(&recordingFanout{}, nil)
	err := h.Handle(context.Background(), fanoutJob(t, service.WebhookFanoutPayload{UserID: uuid.New(), EventType: "x"}))
	if !jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want a permanent failure — a fan-out without a stable key would double-deliver on retry", err)
	}
}

func TestWebhookFanoutJobHandler_TransientFailureRetries(t *testing.T) {
	fanout := &recordingFanout{err: errors.New("database down")}
	h := service.NewWebhookFanoutJobHandler(fanout, nil)

	err := h.Handle(context.Background(), fanoutJob(t, service.WebhookFanoutPayload{
		UserID: uuid.New(), EventType: "x", DedupeKey: "key-1",
	}))
	if err == nil {
		t.Fatal("Handle returned nil for a failed fan-out")
	}
	if jobqueue.IsPermanent(err) {
		t.Fatalf("error = %v, want a retryable failure", err)
	}
}

func TestNewWebhookFanoutEvent_CarriesRoutingAndDedupeKey(t *testing.T) {
	userID := uuid.New()
	e, err := service.NewWebhookFanoutEvent("savings_goal", "goal-1", userID, "goal.milestone.50", []byte(`{"x":1}`), "key-1")
	if err != nil {
		t.Fatalf("NewWebhookFanoutEvent: %v", err)
	}
	if e.EventType != service.OutboxEventWebhookFanout {
		t.Fatalf("event type = %q, want %q", e.EventType, service.OutboxEventWebhookFanout)
	}
	if e.AggregateType != "savings_goal" || e.AggregateID != "goal-1" {
		t.Fatalf("aggregate = %s/%s, want savings_goal/goal-1", e.AggregateType, e.AggregateID)
	}
	if e.DedupeKey != "key-1" {
		t.Fatalf("dedupe key = %q, want key-1", e.DedupeKey)
	}
	var p service.WebhookFanoutPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.UserID != userID || p.DedupeKey != "key-1" {
		t.Fatalf("payload = %+v, want the user and dedupe key carried through", p)
	}
}

func TestNewWebhookFanoutEvent_RejectsNonJSONPayload(t *testing.T) {
	if _, err := service.NewWebhookFanoutEvent("a", "b", uuid.New(), "x", []byte(`not json`), "key"); err == nil {
		t.Fatal("accepted a non-JSON payload; the relay would dead-letter it hours later instead")
	}
}

// TestDelivery_CarriesDedupeKeyInHeaderAndBody is the issue's explicit
// requirement that the key reach the consumer both ways: a queue consumer
// that only sees the persisted body has no headers left to read.
func TestDelivery_CarriesDedupeKeyInHeaderAndBody(t *testing.T) {
	var (
		gotHeader string
		gotBody   []byte
		gotSig    string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Nester-Dedupe-Key")
		gotSig = r.Header.Get("X-Nester-Signature")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	const dedupeKey = "savings_goal:goal-1:milestone:50"
	const secret = "whsec_test"

	cipher := testCipher(t)
	ciphertext, keyVersion := sealSecret(t, cipher, secret)
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL, Status: webhook.StatusActive, SecretCiphertext: ciphertext, SecretKeyVersion: keyVersion}
	h := service.NewWebhookDeliveryJobHandler(newFakeWebhookRepo(wh), &fakeDeliveryRepo{}, cipher, allowAllLimiter{}, nil, nil)
	h.SetSSRFValidateForTest(allowAllSSRF)

	job := jobFor(t, service.WebhookDeliveryJobPayload{
		WebhookID:  wh.ID,
		DeliveryID: uuid.New(),
		EventType:  "goal.milestone.50",
		Payload:    []byte(`{"event":"goal.milestone.50"}`),
		DedupeKey:  dedupeKey,
	}, 1, 5)
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if gotHeader != dedupeKey {
		t.Fatalf("X-Nester-Dedupe-Key = %q, want %q", gotHeader, dedupeKey)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("unmarshal delivered body: %v", err)
	}
	if body["dedupe_key"] != dedupeKey {
		t.Fatalf("body dedupe_key = %v, want %q", body["dedupe_key"], dedupeKey)
	}
	if body["event"] != "goal.milestone.50" {
		t.Fatalf("injecting the dedupe key clobbered the body: %v", body)
	}

	// The signature must cover the body actually sent, not the pre-injection
	// one — otherwise every outbox-driven delivery fails verification.
	ts, err := strconv.ParseInt(strings.TrimPrefix(strings.Split(gotSig, ",")[0], "t="), 10, 64)
	if err != nil {
		t.Fatalf("parse signature timestamp from %q: %v", gotSig, err)
	}
	want := strings.TrimPrefix(strings.Split(gotSig, ",")[1], "v1=")
	if !service.VerifyWebhookSignature([]byte(secret), ts, gotBody, want) {
		t.Fatal("signature does not verify against the delivered body")
	}
}

// TestDelivery_WithoutADedupeKeyIsUnchanged: manual redelivery and the
// legacy FireForUser path carry no key, and must not have their bodies or
// headers rewritten.
func TestDelivery_WithoutADedupeKeyIsUnchanged(t *testing.T) {
	var gotHeader string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Nester-Dedupe-Key")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cipher := testCipher(t)
	ciphertext, keyVersion := sealSecret(t, cipher, "whsec_test")
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL, Status: webhook.StatusActive, SecretCiphertext: ciphertext, SecretKeyVersion: keyVersion}
	h := service.NewWebhookDeliveryJobHandler(newFakeWebhookRepo(wh), &fakeDeliveryRepo{}, cipher, allowAllLimiter{}, nil, nil)
	h.SetSSRFValidateForTest(allowAllSSRF)

	const original = `{"event":"x"}`
	job := jobFor(t, service.WebhookDeliveryJobPayload{
		WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Payload: []byte(original),
	}, 1, 5)
	if err := h.Handle(context.Background(), job); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	if gotHeader != "" {
		t.Fatalf("X-Nester-Dedupe-Key = %q for a delivery with no key, want empty", gotHeader)
	}
	if string(gotBody) != original {
		t.Fatalf("body = %s, want it unchanged (%s)", gotBody, original)
	}
}
