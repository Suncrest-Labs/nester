package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
)

func noop(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func TestCORS_AllowedOrigin(t *testing.T) {
	handler := middleware.CORS([]string{"https://app.nester.finance"})(http.HandlerFunc(noop))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.nester.finance")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.nester.finance" {
		t.Errorf("Allow-Origin = %q, want %q", got, "https://app.nester.finance")
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want %q", got, "true")
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want %q", got, "Origin")
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	handler := middleware.CORS([]string{"https://app.nester.finance"})(http.HandlerFunc(noop))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for disallowed origin", got)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; request should still be served", rec.Code, http.StatusOK)
	}
}

func TestCORS_PreflightAllowed(t *testing.T) {
	handler := middleware.CORS([]string{"https://app.nester.finance"})(http.HandlerFunc(noop))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/vaults", nil)
	req.Header.Set("Origin", "https://app.nester.finance")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d for preflight", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("Allow-Methods should be set on preflight for allowed origin")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Allow-Headers should be set on preflight for allowed origin")
	}
	if got := rec.Header().Get("Access-Control-Max-Age"); got != "86400" {
		t.Errorf("Max-Age = %q, want %q", got, "86400")
	}
}

func TestCORS_PreflightDisallowed(t *testing.T) {
	handler := middleware.CORS([]string{"https://app.nester.finance"})(http.HandlerFunc(noop))

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/vaults", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; OPTIONS still returns 204", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for disallowed preflight", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "" {
		t.Errorf("Allow-Methods = %q, want empty for disallowed preflight", got)
	}
}

func TestCORS_NoOriginHeader(t *testing.T) {
	handler := middleware.CORS([]string{"https://app.nester.finance"})(http.HandlerFunc(noop))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty when no Origin sent", got)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d for same-origin request", rec.Code, http.StatusOK)
	}
}

func TestCORS_EmptyAllowlist(t *testing.T) {
	handler := middleware.CORS(nil)(http.HandlerFunc(noop))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.nester.finance")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty when allowlist is empty", got)
	}
}

func TestCORS_MultipleOrigins(t *testing.T) {
	origins := []string{"https://app.nester.finance", "https://nester.finance", "http://localhost:3000"}
	handler := middleware.CORS(origins)(http.HandlerFunc(noop))

	for _, origin := range origins {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("origin %q: Allow-Origin = %q, want %q", origin, got, origin)
		}
	}
}
