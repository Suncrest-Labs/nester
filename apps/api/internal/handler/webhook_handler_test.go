package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// --- stub WebhookService ---

type stubWebhookSvc struct {
	hooks  []webhook.Webhook
	err    error
	called int
}

func (s *stubWebhookSvc) Register(_ context.Context, userID uuid.UUID, in service.RegisterWebhookInput) (webhook.Webhook, error) {
	if s.err != nil {
		return webhook.Webhook{}, s.err
	}
	wh := webhook.Webhook{
		ID:        uuid.New(),
		UserID:    userID,
		URL:       in.URL,
		Secret:    in.Secret,
		CreatedAt: time.Now().UTC(),
	}
	s.hooks = append(s.hooks, wh)
	return wh, nil
}

func (s *stubWebhookSvc) List(_ context.Context, _ uuid.UUID) ([]webhook.Webhook, error) {
	return s.hooks, s.err
}

func (s *stubWebhookSvc) Delete(_ context.Context, _, id uuid.UUID) error {
	s.called++
	if s.err != nil {
		return s.err
	}
	for i, wh := range s.hooks {
		if wh.ID == id {
			s.hooks = append(s.hooks[:i], s.hooks[i+1:]...)
			return nil
		}
	}
	return webhook.ErrWebhookNotFound
}

// --- helpers ---

func authedRequest(method, target string, body []byte, userID uuid.UUID) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	ctx := auth.NewContext(req.Context(), auth.User{ID: userID.String()})
	return req.WithContext(ctx)
}

// --- tests ---

func TestWebhookHandler_Create_Returns201(t *testing.T) {
	svc := &stubWebhookSvc{}
	h := handler.NewWebhookHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	userID := uuid.New()
	body, _ := json.Marshal(map[string]string{"url": "https://example.com/hook", "secret": "s3cr3t"})
	req := authedRequest(http.MethodPost, "/api/v1/webhooks", body, userID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["success"] != true {
		t.Errorf("expected success=true, got %v", out["success"])
	}
	data := out["data"].(map[string]any)
	if data["url"] != "https://example.com/hook" {
		t.Errorf("unexpected url: %v", data["url"])
	}
}

func TestWebhookHandler_Create_MissingURL_Returns400(t *testing.T) {
	svc := &stubWebhookSvc{}
	h := handler.NewWebhookHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	userID := uuid.New()
	body, _ := json.Marshal(map[string]string{"url": "", "secret": ""})
	req := authedRequest(http.MethodPost, "/api/v1/webhooks", body, userID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestWebhookHandler_List_ReturnsWebhooks(t *testing.T) {
	userID := uuid.New()
	svc := &stubWebhookSvc{
		hooks: []webhook.Webhook{
			{ID: uuid.New(), UserID: userID, URL: "https://example.com/hook", CreatedAt: time.Now()},
		},
	}
	h := handler.NewWebhookHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := authedRequest(http.MethodGet, "/api/v1/webhooks", nil, userID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	data := out["data"].([]any)
	if len(data) != 1 {
		t.Errorf("expected 1 webhook, got %d", len(data))
	}
}

func TestWebhookHandler_Delete_Returns204(t *testing.T) {
	userID := uuid.New()
	whID := uuid.New()
	svc := &stubWebhookSvc{
		hooks: []webhook.Webhook{
			{ID: whID, UserID: userID, URL: "https://example.com/hook", CreatedAt: time.Now()},
		},
	}
	h := handler.NewWebhookHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := authedRequest(http.MethodDelete, "/api/v1/webhooks/"+whID.String(), nil, userID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWebhookHandler_Delete_NotFound_Returns404(t *testing.T) {
	svc := &stubWebhookSvc{err: webhook.ErrWebhookNotFound}
	h := handler.NewWebhookHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	userID := uuid.New()
	req := authedRequest(http.MethodDelete, "/api/v1/webhooks/"+uuid.New().String(), nil, userID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestWebhookHandler_Unauthenticated_Returns401(t *testing.T) {
	svc := &stubWebhookSvc{}
	h := handler.NewWebhookHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/webhooks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
