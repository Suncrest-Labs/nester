package service_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// TestDeliverWebhook_SuccessOnFirstAttempt verifies a 2xx response stops retrying.
func TestDeliverWebhook_SuccessOnFirstAttempt(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload, _ := service.BuildWebhookPayload("goal.milestone.50", uuid.Nil, uuid.Nil, 50, "50", "100")
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL}
	service.DeliverWebhookForTest(wh, payload)

	if got := int(count.Load()); got != 1 {
		t.Errorf("expected 1 delivery attempt, got %d", got)
	}
}

// TestDeliverWebhook_RetriesUpTo3Times verifies persistent failure retries exactly 3 times.
func TestDeliverWebhook_RetriesUpTo3Times(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	payload, _ := service.BuildWebhookPayload("goal.milestone.50", uuid.Nil, uuid.Nil, 50, "50", "100")
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL}
	service.DeliverWebhookForTest(wh, payload)

	if got := int(count.Load()); got != 3 {
		t.Errorf("expected 3 delivery attempts, got %d", got)
	}
}

// TestDeliverWebhook_SucceedsOnSecondAttempt verifies retry stops on first success.
func TestDeliverWebhook_SucceedsOnSecondAttempt(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := count.Add(1)
		if n >= 2 {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()

	payload, _ := service.BuildWebhookPayload("goal.milestone.50", uuid.Nil, uuid.Nil, 50, "50", "100")
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL}
	service.DeliverWebhookForTest(wh, payload)

	if got := int(count.Load()); got != 2 {
		t.Errorf("expected 2 delivery attempts (success on 2nd), got %d", got)
	}
}

// TestDeliverWebhook_SetsHMACSignatureHeader verifies the X-Nester-Signature header is set.
func TestDeliverWebhook_SetsHMACSignatureHeader(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Nester-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := []byte(`{"event":"goal.milestone.50"}`)
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL, Secret: "mysecret"}
	service.DeliverWebhookForTest(wh, payload)

	if gotSig == "" {
		t.Fatal("expected X-Nester-Signature header, got empty string")
	}
	if len(gotSig) < 7 || gotSig[:7] != "sha256=" {
		t.Errorf("signature must start with sha256=, got %q", gotSig)
	}
}

// TestDeliverWebhook_NoSignatureHeaderWhenNoSecret verifies the header is absent without a secret.
func TestDeliverWebhook_NoSignatureHeaderWhenNoSecret(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Nester-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := []byte(`{"event":"goal.milestone.50"}`)
	wh := webhook.Webhook{ID: uuid.New(), UserID: uuid.New(), URL: srv.URL}
	service.DeliverWebhookForTest(wh, payload)

	if gotSig != "" {
		t.Errorf("expected no X-Nester-Signature when secret is empty, got %q", gotSig)
	}
}

// TestBuildWebhookPayload_ContainsRequiredFields verifies the JSON payload shape.
func TestBuildWebhookPayload_ContainsRequiredFields(t *testing.T) {
	goalID := uuid.New()
	userID := uuid.New()
	payload, err := service.BuildWebhookPayload("goal.milestone.75", goalID, userID, 75, "75.0", "100.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{`"event"`, `"goal_id"`, `"user_id"`, `"milestone_pct"`, `"current_amount"`, `"target_amount"`, `"timestamp"`} {
		if !contains(payload, want) {
			t.Errorf("payload missing field %s: %s", want, payload)
		}
	}
}

func contains(data []byte, sub string) bool {
	return len(data) > 0 && string(data) != "" && stringContains(string(data), sub)
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
