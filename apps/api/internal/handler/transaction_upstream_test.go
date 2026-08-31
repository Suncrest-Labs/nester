package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
	"github.com/suncrestlabs/nester/apps/api/internal/retry"
)

// Confirming a transaction reaches Horizon through the breaker-wrapped client,
// so an upstream outage surfaces as a domain error here. It must answer 503,
// not 500: the request is retriable and nothing about it was wrong, and a 500
// sends the operator hunting for an API bug during an upstream incident.
func TestTransactionHandlerMapsUpstreamFailuresTo503(t *testing.T) {
	h := &TransactionHandler{}

	cases := map[string]struct {
		err            error
		wantRetryAfter string
	}{
		"breaker open":       {breaker.ErrOpen, "1"},
		"retry exhausted":    {retry.ErrExhausted, "1"},
		"open with cooldown": {&breaker.OpenError{RetryIn: 12 * time.Second}, "12"},
		"wrapped": {
			fmt.Errorf("confirm transaction: %w", &breaker.OpenError{RetryIn: 5 * time.Second}),
			"5",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/x", nil)

			h.writeDomainError(rec, req, tc.err)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", rec.Code)
			}
			if got := rec.Header().Get("Retry-After"); got != tc.wantRetryAfter {
				t.Fatalf("Retry-After = %q, want %q", got, tc.wantRetryAfter)
			}
		})
	}
}

// An error that is not an upstream rejection must still be a 500, otherwise
// the case above would be satisfied by answering 503 to everything and real
// bugs would be reported as upstream outages.
func TestTransactionHandlerStillReturns500ForRealFailures(t *testing.T) {
	h := &TransactionHandler{}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/transactions/x", nil)

	h.writeDomainError(rec, req, fmt.Errorf("column \"foo\" does not exist"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "" {
		t.Fatalf("Retry-After = %q, want it unset on a genuine failure", got)
	}
}
