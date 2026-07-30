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
	"github.com/suncrestlabs/nester/apps/api/internal/domain/session"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// --- stub AuthService ---

type stubAuthSvc struct {
	tokens         service.Tokens
	verifyErr      error
	refreshErr     error
	logoutErr      error
	revokeErr      error
	logoutAllCount int
	sessions       []session.Session
	revokedID      uuid.UUID
}

func (s *stubAuthSvc) GenerateChallenge(context.Context, string) (string, error) {
	return "stub-challenge", nil
}

func (s *stubAuthSvc) VerifyAndIssue(context.Context, string, string, string, service.SessionMetadata) (service.Tokens, error) {
	if s.verifyErr != nil {
		return service.Tokens{}, s.verifyErr
	}
	return s.tokens, nil
}

func (s *stubAuthSvc) Refresh(context.Context, string, service.SessionMetadata) (service.Tokens, error) {
	if s.refreshErr != nil {
		return service.Tokens{}, s.refreshErr
	}
	return s.tokens, nil
}

func (s *stubAuthSvc) Logout(_ context.Context, _, _ uuid.UUID) error {
	return s.logoutErr
}

func (s *stubAuthSvc) LogoutAll(context.Context, uuid.UUID) (int, error) {
	return s.logoutAllCount, nil
}

func (s *stubAuthSvc) ListSessions(context.Context, uuid.UUID) ([]session.Session, error) {
	return s.sessions, nil
}

func (s *stubAuthSvc) RevokeSession(_ context.Context, _, sessionID uuid.UUID) error {
	s.revokedID = sessionID
	return s.revokeErr
}

// --- helpers ---

func authedSessionRequest(method, target string, body []byte, userID, sessionID uuid.UUID) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	ctx := auth.NewContext(req.Context(), auth.User{ID: userID.String(), SessionID: sessionID.String()})
	return req.WithContext(ctx)
}

func decodeEnvelope(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode response: %v (%s)", err, body)
	}
	return out
}

// --- tests ---

func refreshRequest(deviceFingerprint, cookieValue string) *http.Request {
	body, _ := json.Marshal(map[string]string{"device_fingerprint": deviceFingerprint})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: "nester_refresh_token", Value: cookieValue})
	}
	return req
}

func TestAuthHandler_Refresh_ReturnsTokenEnvelope(t *testing.T) {
	svc := &stubAuthSvc{tokens: service.Tokens{AccessToken: "access-1", RefreshToken: "refresh-2", ExpiresIn: 300, RefreshExpiresIn: 604800, SessionID: uuid.New()}}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, refreshRequest("device-1", "old-refresh"))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeEnvelope(t, rec.Body.Bytes())
	data := out["data"].(map[string]any)
	if data["access_token"] != "access-1" {
		t.Errorf("unexpected access token: %v", data)
	}
	if _, leaked := data["refresh_token"]; leaked {
		t.Error("refresh_token must never appear in the JSON response body")
	}
	if data["token_type"] != "Bearer" {
		t.Errorf("expected token_type Bearer, got %v", data["token_type"])
	}

	// The rotated refresh token must be set as an httpOnly cookie instead.
	cookies := rec.Result().Cookies()
	var found *http.Cookie
	for _, c := range cookies {
		if c.Name == "nester_refresh_token" {
			found = c
		}
	}
	if found == nil {
		t.Fatal("expected nester_refresh_token cookie to be set")
	}
	if found.Value != "refresh-2" {
		t.Errorf("cookie value = %q, want the newly-rotated refresh token", found.Value)
	}
	if !found.HttpOnly {
		t.Error("refresh cookie must be HttpOnly")
	}
	if !found.Secure {
		t.Error("refresh cookie must be Secure when secureCookies is true")
	}
}

func TestAuthHandler_Refresh_MissingDeviceFingerprint_Returns400(t *testing.T) {
	svc := &stubAuthSvc{}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, refreshRequest("", "old-refresh"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestAuthHandler_Refresh_MissingCookie_Returns401(t *testing.T) {
	svc := &stubAuthSvc{}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, refreshRequest("device-1", ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when no refresh cookie is present, got %d", rec.Code)
	}
}

func TestAuthHandler_Refresh_FailureCollapsesToGenericUnauthorized(t *testing.T) {
	svc := &stubAuthSvc{refreshErr: service.ErrRefreshFailed}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, refreshRequest("device-1", "reused-token"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	out := decodeEnvelope(t, rec.Body.Bytes())
	msg, _ := out["error"].(map[string]any)["message"].(string)
	if msg == "" {
		t.Fatal("expected a generic error message")
	}
	// The response must not reveal *why* the refresh failed (reuse vs.
	// expired vs. device mismatch are internal-only distinctions).
	for _, leaky := range []string{"reused", "reuse", "device", "mismatch"} {
		if bytes.Contains([]byte(msg), []byte(leaky)) {
			t.Errorf("error message leaks internal failure reason %q: %q", leaky, msg)
		}
	}

	// The dead cookie must be cleared, not left pointing at a rejected token.
	cookies := rec.Result().Cookies()
	for _, c := range cookies {
		if c.Name == "nester_refresh_token" && c.MaxAge >= 0 {
			t.Errorf("expected refresh cookie to be cleared (MaxAge < 0), got MaxAge=%d", c.MaxAge)
		}
	}
}

func TestAuthHandler_Logout_RevokesCurrentSession(t *testing.T) {
	svc := &stubAuthSvc{}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	userID, sessionID := uuid.New(), uuid.New()
	req := authedSessionRequest(http.MethodPost, "/api/v1/auth/logout", nil, userID, sessionID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthHandler_Logout_WithoutAuthContext_Returns401(t *testing.T) {
	svc := &stubAuthSvc{}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthHandler_LogoutAll_ReturnsRevokedCount(t *testing.T) {
	svc := &stubAuthSvc{logoutAllCount: 3}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	userID, sessionID := uuid.New(), uuid.New()
	req := authedSessionRequest(http.MethodPost, "/api/v1/auth/logout-all", nil, userID, sessionID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeEnvelope(t, rec.Body.Bytes())
	data := out["data"].(map[string]any)
	if data["revoked_count"] != float64(3) {
		t.Errorf("expected revoked_count 3, got %v", data["revoked_count"])
	}
}

func TestAuthHandler_ListSessions_MarksCurrentSession(t *testing.T) {
	userID, currentSessionID := uuid.New(), uuid.New()
	otherSessionID := uuid.New()
	now := time.Now().UTC()

	svc := &stubAuthSvc{sessions: []session.Session{
		{ID: currentSessionID, UserID: userID, DeviceFingerprint: "device-a", CreatedAt: now, LastActiveAt: now, AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour)},
		{ID: otherSessionID, UserID: userID, DeviceFingerprint: "device-b", CreatedAt: now, LastActiveAt: now, AbsoluteExpiresAt: now.Add(30 * 24 * time.Hour)},
	}}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	req := authedSessionRequest(http.MethodGet, "/api/v1/auth/sessions", nil, userID, currentSessionID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	out := decodeEnvelope(t, rec.Body.Bytes())
	data := out["data"].(map[string]any)
	sessions := data["sessions"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	for _, raw := range sessions {
		s := raw.(map[string]any)
		wantCurrent := s["id"] == currentSessionID.String()
		if s["is_current"] != wantCurrent {
			t.Errorf("session %v: is_current = %v, want %v", s["id"], s["is_current"], wantCurrent)
		}
	}
}

func TestAuthHandler_RevokeSession_NotFoundReturns404(t *testing.T) {
	svc := &stubAuthSvc{revokeErr: session.ErrSessionNotFound}
	h := handler.NewAuthHandler(svc, true, nil, nil, nil)
	mux := http.NewServeMux()
	h.Register(mux)

	userID, sessionID := uuid.New(), uuid.New()
	targetID := uuid.New()
	req := authedSessionRequest(http.MethodDelete, "/api/v1/auth/sessions/"+targetID.String(), nil, userID, sessionID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if svc.revokedID != targetID {
		t.Errorf("expected RevokeSession called with %s, got %s", targetID, svc.revokedID)
	}
}
