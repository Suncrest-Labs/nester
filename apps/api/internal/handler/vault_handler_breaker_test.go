package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
	"github.com/suncrestlabs/nester/apps/api/internal/retry"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// A chain call the breaker declined is a known, temporary upstream condition
// with a known retry time — not a fault in this service. Reporting it as 500
// tells a client to treat it as a bug rather than to back off, and buries a
// deliberate load-shed among genuine internal errors.
func TestOpenBreakerBecomesServiceUnavailable(t *testing.T) {
	h := &VaultHandler{}

	// Wrapped the way http.Client.Do wraps a transport error, because that is
	// the shape the error actually arrives in.
	err := fmt.Errorf("simulate contract: %w", &breaker.OpenError{
		Name:    "soroban_rpc",
		RetryIn: 12 * time.Second,
	})

	rec := httptest.NewRecorder()
	h.writeDomainError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vaults/x", nil), err)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "12" {
		t.Fatalf("Retry-After = %q, want \"12\"", got)
	}

	var body response.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Success {
		t.Fatal("success = true on a rejected chain call")
	}
	if body.Error == nil || body.Error.Code != "UPSTREAM_UNAVAILABLE" {
		t.Fatalf("error = %+v, want code UPSTREAM_UNAVAILABLE", body.Error)
	}

	// The upstream's identity is an internal detail; the client is told what
	// to do, not which dependency broke.
	if got := rec.Body.String(); contains(got, "soroban_rpc") {
		t.Fatalf("response leaks the upstream name: %s", got)
	}
}

// A read retried to exhaustion is the same class of failure as a breaker
// rejection — the chain never answered — and must produce the same typed 503
// rather than a generic 500 (nester#1086).
func TestRetryExhaustionBecomesServiceUnavailable(t *testing.T) {
	h := &VaultHandler{}

	// Wrapped the way the RPC client wraps it, so the test exercises the
	// error as it actually arrives at the handler.
	err := fmt.Errorf("rpc simulateTransaction: %w", &retry.Error{
		Attempts: 3,
		Elapsed:  4 * time.Second,
		Err:      errors.New("connection reset by peer"),
	})

	rec := httptest.NewRecorder()
	h.writeDomainError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vaults/x", nil), err)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var body response.Response
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == nil || body.Error.Code != "UPSTREAM_UNAVAILABLE" {
		t.Fatalf("error = %+v, want code UPSTREAM_UNAVAILABLE", body.Error)
	}

	// The retry loop carries no cooldown of its own, so the client is still
	// given a floor rather than an absent header.
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want \"1\"", got)
	}
}

// A bare ErrOpen with no duration still yields a usable hint rather than an
// absent or zero header, which some clients treat as "retry immediately".
func TestRetryAfterFallsBackWhenNoDurationIsCarried(t *testing.T) {
	h := &VaultHandler{}

	rec := httptest.NewRecorder()
	h.writeDomainError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vaults/x", nil), breaker.ErrOpen)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want \"1\"", got)
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	cases := map[string]struct {
		err  error
		want string
	}{
		"whole seconds":      {&breaker.OpenError{RetryIn: 15 * time.Second}, "15"},
		"rounds up":          {&breaker.OpenError{RetryIn: 1500 * time.Millisecond}, "2"},
		"elapsed":            {&breaker.OpenError{RetryIn: 0}, "1"},
		"unrelated error":    {errors.New("boom"), "1"},
		"wrapped open error": {fmt.Errorf("call: %w", &breaker.OpenError{RetryIn: 8 * time.Second}), "8"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := upstreamRetryAfterSeconds(tc.err); got != tc.want {
				t.Fatalf("upstreamRetryAfterSeconds() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Errors that are not breaker rejections keep their existing mapping. The new
// case must not have widened into a catch-all.
func TestUnrelatedErrorsStillMapToInternalError(t *testing.T) {
	h := &VaultHandler{}

	rec := httptest.NewRecorder()
	h.writeDomainError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vaults/x", nil), errors.New("boom"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q on an unrelated error, want none", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
