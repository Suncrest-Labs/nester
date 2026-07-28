package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type recordingEnqueuer struct {
	mu    sync.Mutex
	calls []struct {
		jobType string
		payload service.WebhookDeliveryJobPayload
		opts    []jobqueue.EnqueueOption
	}
}

func (e *recordingEnqueuer) EnqueueJSON(_ context.Context, jobType string, payload any, opts ...jobqueue.EnqueueOption) (jobqueue.Job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p, _ := payload.(service.WebhookDeliveryJobPayload)
	e.calls = append(e.calls, struct {
		jobType string
		payload service.WebhookDeliveryJobPayload
		opts    []jobqueue.EnqueueOption
	}{jobType, p, opts})
	return jobqueue.Job{ID: uuid.New(), Type: jobType}, nil
}

func (e *recordingEnqueuer) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.calls)
}

func newServiceForTest(t *testing.T, hooks ...webhook.Webhook) (*service.WebhookService, *fakeWebhookRepo, *fakeDeliveryRepo, *recordingEnqueuer) {
	t.Helper()
	repo := newFakeWebhookRepo(hooks...)
	deliveries := &fakeDeliveryRepo{}
	enqueuer := &recordingEnqueuer{}
	return service.NewWebhookService(repo, deliveries, testCipher(t), enqueuer), repo, deliveries, enqueuer
}

func TestWebhookService_Register_RejectsEmptyURL(t *testing.T) {
	svc, _, _, _ := newServiceForTest(t)
	_, err := svc.Register(context.Background(), uuid.New(), service.RegisterWebhookInput{URL: "  "})
	if err == nil {
		t.Fatal("expected an error for an empty URL")
	}
}

func TestWebhookService_Register_RejectsNonHTTPS(t *testing.T) {
	svc, _, _, _ := newServiceForTest(t)
	_, err := svc.Register(context.Background(), uuid.New(), service.RegisterWebhookInput{URL: "http://example.com/hook"})
	if err == nil {
		t.Fatal("expected an error for a non-https URL")
	}
}

func TestWebhookService_Register_RejectsPrivateTarget(t *testing.T) {
	svc, _, _, _ := newServiceForTest(t)
	_, err := svc.Register(context.Background(), uuid.New(), service.RegisterWebhookInput{URL: "https://169.254.169.254/latest/meta-data"})
	if err == nil {
		t.Fatal("expected an error for an SSRF-disallowed target")
	}
}

func TestWebhookService_Register_RejectsWhenCipherNotConfigured(t *testing.T) {
	repo := newFakeWebhookRepo()
	svc := service.NewWebhookService(repo, &fakeDeliveryRepo{}, nil, &recordingEnqueuer{})
	_, err := svc.Register(context.Background(), uuid.New(), service.RegisterWebhookInput{URL: "https://93.184.216.34/hook"})
	if !errors.Is(err, service.ErrWebhookCipherNotConfigured) {
		t.Fatalf("got %v, want ErrWebhookCipherNotConfigured", err)
	}
}

func TestWebhookService_Register_ReturnsSecretOnceAndNeverStoresPlaintext(t *testing.T) {
	svc, repo, _, _ := newServiceForTest(t)
	userID := uuid.New()
	wh, err := svc.Register(context.Background(), userID, service.RegisterWebhookInput{URL: "https://93.184.216.34/hook"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if wh.Secret == "" {
		t.Fatal("expected a generated secret to be returned on registration")
	}
	if !strings.HasPrefix(wh.Secret, "whsec_") {
		t.Errorf("expected a whsec_-prefixed secret, got %q", wh.Secret)
	}

	stored, err := repo.GetByID(context.Background(), wh.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if stored.Secret != "" {
		t.Error("expected the stored webhook's plaintext Secret field to be empty")
	}
	if len(stored.SecretCiphertext) == 0 {
		t.Error("expected the secret to be persisted as ciphertext")
	}
	if string(stored.SecretCiphertext) == wh.Secret {
		t.Error("ciphertext must not equal the plaintext secret")
	}
}

func TestWebhookService_Register_DefaultsToActiveStatus(t *testing.T) {
	svc, _, _, _ := newServiceForTest(t)
	wh, err := svc.Register(context.Background(), uuid.New(), service.RegisterWebhookInput{URL: "https://93.184.216.34/hook"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if wh.Status != webhook.StatusActive {
		t.Errorf("status = %q, want active", wh.Status)
	}
}

func TestWebhookService_FireForUser_EnqueuesOneJobPerActiveSubscribedWebhook(t *testing.T) {
	userID := uuid.New()
	subscribed := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://a.example.com/hook", Status: webhook.StatusActive, EventTypes: []string{"goal.milestone.50"}}
	unrelated := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://b.example.com/hook", Status: webhook.StatusActive, EventTypes: []string{"other.event"}}
	allEvents := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://c.example.com/hook", Status: webhook.StatusActive} // no filter = all events
	otherUser := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: "https://d.example.com/hook", Status: webhook.StatusActive}

	svc, _, _, enqueuer := newServiceForTest(t, subscribed, unrelated, allEvents, otherUser)
	svc.FireForUser(context.Background(), userID, "goal.milestone.50", []byte(`{}`))

	if enqueuer.count() != 2 {
		t.Fatalf("expected 2 enqueued deliveries (subscribed + allEvents), got %d", enqueuer.count())
	}
	got := map[uuid.UUID]bool{}
	for _, c := range enqueuer.calls {
		got[c.payload.WebhookID] = true
	}
	if !got[subscribed.ID] || !got[allEvents.ID] {
		t.Errorf("expected deliveries for %s and %s, got %+v", subscribed.ID, allEvents.ID, got)
	}
	if got[unrelated.ID] {
		t.Error("did not expect a delivery for a subscription filtered to a different event type")
	}
	if got[otherUser.ID] {
		t.Error("did not expect a delivery for another user's webhook")
	}
}

func TestWebhookService_FireForUser_SkipsSuspendedSubscriptions(t *testing.T) {
	userID := uuid.New()
	suspended := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://a.example.com/hook", Status: webhook.StatusSuspended}
	svc, _, _, enqueuer := newServiceForTest(t, suspended)

	svc.FireForUser(context.Background(), userID, "any.event", []byte(`{}`))

	if enqueuer.count() != 0 {
		t.Errorf("expected no delivery for a suspended subscription, got %d", enqueuer.count())
	}
}

func TestWebhookService_FireForUser_UsesDeliveryIDAsIdempotencyKey(t *testing.T) {
	userID := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: userID, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	svc, _, _, enqueuer := newServiceForTest(t, wh)

	svc.FireForUser(context.Background(), userID, "x", []byte(`{}`))

	if enqueuer.count() != 1 {
		t.Fatalf("expected 1 enqueue call, got %d", enqueuer.count())
	}
	call := enqueuer.calls[0]
	if call.jobType != service.WebhookDeliveryJobType {
		t.Errorf("job type = %q, want %q", call.jobType, service.WebhookDeliveryJobType)
	}
	if call.payload.DeliveryID == uuid.Nil {
		t.Error("expected a non-nil delivery id")
	}
}

func TestWebhookService_ListDeliveries_RejectsNonOwner(t *testing.T) {
	owner := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: owner, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	svc, _, deliveries, _ := newServiceForTest(t, wh)
	_ = deliveries.Log(context.Background(), &webhook.Delivery{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Outcome: webhook.DeliverySucceeded})

	_, err := svc.ListDeliveries(context.Background(), uuid.New(), wh.ID, 50)
	if err == nil {
		t.Fatal("expected an error when a non-owner requests another user's delivery log")
	}
}

func TestWebhookService_ListDeliveries_ReturnsOwnersLog(t *testing.T) {
	owner := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: owner, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	svc, _, deliveries, _ := newServiceForTest(t, wh)
	_ = deliveries.Log(context.Background(), &webhook.Delivery{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Outcome: webhook.DeliverySucceeded})

	got, err := svc.ListDeliveries(context.Background(), owner, wh.ID, 50)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(got))
	}
}

func TestWebhookService_Redeliver_EnqueuesWithFreshDeliveryID(t *testing.T) {
	owner := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: owner, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	svc, _, deliveries, enqueuer := newServiceForTest(t, wh)

	originalDeliveryID := uuid.New()
	original := &webhook.Delivery{WebhookID: wh.ID, DeliveryID: originalDeliveryID, EventType: "goal.milestone.50", Payload: []byte(`{"a":1}`), Outcome: webhook.DeliveryDeadLetter}
	_ = deliveries.Log(context.Background(), original)

	if err := svc.Redeliver(context.Background(), owner, original.ID); err != nil {
		t.Fatalf("Redeliver: %v", err)
	}
	if enqueuer.count() != 1 {
		t.Fatalf("expected 1 enqueue call, got %d", enqueuer.count())
	}
	call := enqueuer.calls[0]
	if call.payload.DeliveryID == originalDeliveryID {
		t.Error("expected a fresh delivery id for a manual redelivery, not the original's")
	}
	if call.payload.EventType != "goal.milestone.50" {
		t.Errorf("event type = %q, want the original delivery's event type", call.payload.EventType)
	}
}

func TestWebhookService_Redeliver_RejectsNonOwner(t *testing.T) {
	owner := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: owner, URL: "https://a.example.com/hook", Status: webhook.StatusActive}
	svc, _, deliveries, _ := newServiceForTest(t, wh)
	original := &webhook.Delivery{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Outcome: webhook.DeliveryDeadLetter}
	_ = deliveries.Log(context.Background(), original)

	if err := svc.Redeliver(context.Background(), uuid.New(), original.ID); err == nil {
		t.Fatal("expected an error when a non-owner attempts redelivery")
	}
}

func TestWebhookService_Redeliver_RejectsSuspendedSubscription(t *testing.T) {
	owner := uuid.New()
	wh := webhook.Webhook{ID: uuid.New(), UserID: owner, URL: "https://a.example.com/hook", Status: webhook.StatusSuspended}
	svc, _, deliveries, enqueuer := newServiceForTest(t, wh)
	original := &webhook.Delivery{WebhookID: wh.ID, DeliveryID: uuid.New(), EventType: "x", Outcome: webhook.DeliveryDeadLetter}
	_ = deliveries.Log(context.Background(), original)

	if err := svc.Redeliver(context.Background(), owner, original.ID); err == nil {
		t.Fatal("expected an error when redelivering to a suspended subscription")
	}
	if enqueuer.count() != 0 {
		t.Error("expected no enqueue for a suspended subscription's redelivery")
	}
}

func TestBuildWebhookPayload_ContainsRequiredFields(t *testing.T) {
	goalID := uuid.New()
	userID := uuid.New()
	payload, err := service.BuildWebhookPayload("goal.milestone.75", goalID, userID, 75, "75.0", "100.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{`"event"`, `"goal_id"`, `"user_id"`, `"milestone_pct"`, `"current_amount"`, `"target_amount"`, `"timestamp"`} {
		if !strings.Contains(string(payload), want) {
			t.Errorf("payload missing field %s: %s", want, payload)
		}
	}
}
