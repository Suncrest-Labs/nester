package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityHeaders_HSTSPreloadInProduction(t *testing.T) {
	handler := SecurityHeaders("production")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "https://api.nester.finance/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	wantHSTS := "max-age=63072000; includeSubDomains; preload"
	if got := rec.Header().Get("Strict-Transport-Security"); got != wantHSTS {
		t.Fatalf("Strict-Transport-Security = %q, want %q", got, wantHSTS)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
}

func TestSecurityHeaders_NoHSTSInDevelopment(t *testing.T) {
	handler := SecurityHeaders("development")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("expected empty Strict-Transport-Security in development, got %q", got)
	}
}
