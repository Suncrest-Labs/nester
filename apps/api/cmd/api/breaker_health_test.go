package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

func testBreakerConfig() breaker.Config {
	return breaker.Config{
		FailureRatio: 0.5,
		MinRequests:  4,
		Window:       time.Minute,
		OpenDuration: 15 * time.Second,
	}
}

func tripBreaker(t *testing.T, b *breaker.Breaker) {
	t.Helper()

	for i := 0; i < 200; i++ {
		if b.State() == breaker.StateOpen {
			return
		}
		permit, err := b.Allow()
		if err != nil {
			break
		}
		b.Record(permit, breaker.Failure)
	}
	if b.State() != breaker.StateOpen {
		t.Fatal("breaker did not open")
	}
}

// serveDetailedHealth runs the handler with no database and no reachable
// upstreams. The chain probes fail, which is fine: what is under test is the
// breaker section, and a failing probe alongside an open breaker is exactly
// the shape an operator sees during an outage.
func serveDetailedHealth(t *testing.T, breakers map[metrics.Upstream]*breaker.Breaker) detailedHealthResponse {
	t.Helper()

	var ready atomic.Bool
	ready.Store(true)

	handler := detailedHealthHandler(healthDeps{
		ready:        &ready,
		pingDB:       func(context.Context) error { return nil },
		probeTimeout: time.Millisecond,
		httpClient:   &http.Client{Timeout: time.Millisecond},
		horizonURL:   "http://127.0.0.1:1/horizon",
		rpcURL:       "http://127.0.0.1:1/rpc",
		startedAt:    time.Now(),
		environment:  "test",
		buildVersion: "test",
		breakers:     breakers,
	})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/health/detailed", nil))

	var resp detailedHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode health response: %v\nbody: %s", err, rec.Body.String())
	}
	return resp
}

// The acceptance criterion: breaker state appears in the readiness response,
// for both chain upstreams, in each of the three states.
func TestDetailedHealthReportsBreakerState(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	soroban := breaker.NewWithClock(string(metrics.UpstreamSorobanRPC), testBreakerConfig(), nil, clock)
	horizon := breaker.NewWithClock(string(metrics.UpstreamHorizon), testBreakerConfig(), nil, clock)
	breakers := map[metrics.Upstream]*breaker.Breaker{
		metrics.UpstreamSorobanRPC: soroban,
		metrics.UpstreamHorizon:    horizon,
	}

	// CLOSED
	resp := serveDetailedHealth(t, breakers)
	if got := resp.SorobanRPC.CircuitBreaker; got == nil || got.State != "closed" {
		t.Fatalf("soroban circuit_breaker = %+v, want state closed", got)
	}
	if got := resp.Horizon.CircuitBreaker; got == nil || got.State != "closed" {
		t.Fatalf("horizon circuit_breaker = %+v, want state closed", got)
	}

	// OPEN, and only for the upstream that failed.
	tripBreaker(t, soroban)

	resp = serveDetailedHealth(t, breakers)
	if got := resp.SorobanRPC.CircuitBreaker; got.State != "open" {
		t.Fatalf("soroban state = %q, want open", got.State)
	}
	if got := resp.Horizon.CircuitBreaker; got.State != "closed" {
		t.Fatalf("horizon state = %q, want closed: a Soroban outage moved Horizon's reported state", got.State)
	}
	if got := resp.SorobanRPC.CircuitBreaker.RetrySeconds; got != 15 {
		t.Fatalf("retry_in_seconds = %v, want 15", got)
	}
	if got := resp.SorobanRPC.CircuitBreaker.FailureRatio; got != 1 {
		t.Fatalf("failure_ratio = %v, want 1", got)
	}

	// HALF-OPEN
	now = now.Add(15 * time.Second)
	resp = serveDetailedHealth(t, breakers)
	if got := resp.SorobanRPC.CircuitBreaker.State; got != "half_open" {
		t.Fatalf("soroban state = %q, want half_open", got)
	}
}

// An open breaker degrades the reported status but must NOT make the endpoint
// return 503. Chain dependencies have never gated readiness here, and evicting
// the pod from its load balancer over an upstream outage would turn the
// partial failure into the total one this feature exists to prevent.
func TestOpenBreakerDegradesStatusWithoutFailingReadiness(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	soroban := breaker.NewWithClock(
		string(metrics.UpstreamSorobanRPC), testBreakerConfig(), nil,
		func() time.Time { return now },
	)
	tripBreaker(t, soroban)

	var ready atomic.Bool
	ready.Store(true)

	handler := detailedHealthHandler(healthDeps{
		ready:        &ready,
		pingDB:       func(context.Context) error { return nil },
		probeTimeout: time.Millisecond,
		httpClient:   &http.Client{Timeout: time.Millisecond},
		horizonURL:   "http://127.0.0.1:1/horizon",
		rpcURL:       "http://127.0.0.1:1/rpc",
		startedAt:    time.Now(),
		environment:  "test",
		buildVersion: "test",
		breakers: map[metrics.Upstream]*breaker.Breaker{
			metrics.UpstreamSorobanRPC: soroban,
		},
	})

	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/health/detailed", nil))

	var resp detailedHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "degraded" {
		t.Fatalf("status = %q, want degraded", resp.Status)
	}
}

// When the breakers are disabled the field is absent rather than reporting a
// closed breaker that does not exist. Absence means "not guarded".
func TestDetailedHealthOmitsBreakerWhenDisabled(t *testing.T) {
	resp := serveDetailedHealth(t, nil)

	if resp.SorobanRPC.CircuitBreaker != nil {
		t.Fatalf("soroban circuit_breaker = %+v, want absent", resp.SorobanRPC.CircuitBreaker)
	}
	if resp.Horizon.CircuitBreaker != nil {
		t.Fatalf("horizon circuit_breaker = %+v, want absent", resp.Horizon.CircuitBreaker)
	}
}

// The JSON contract, asserted on the wire rather than through the Go struct,
// because it is what an operator's tooling reads.
func TestBreakerStatusJSONShape(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	b := breaker.NewWithClock("soroban_rpc", testBreakerConfig(), nil, func() time.Time { return now })
	tripBreaker(t, b)

	encoded, err := json.Marshal(dependencyStatus{
		OK:             false,
		CircuitBreaker: newBreakerStatus(b),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cb, ok := decoded["circuit_breaker"].(map[string]any)
	if !ok {
		t.Fatalf("circuit_breaker missing from %s", encoded)
	}
	for _, field := range []string{"state", "failure_ratio", "observed_requests", "rejected_total", "retry_in_seconds"} {
		if _, ok := cb[field]; !ok {
			t.Errorf("circuit_breaker is missing %q: %s", field, encoded)
		}
	}
	if cb["state"] != "open" {
		t.Errorf("state = %v, want open", cb["state"])
	}
}

func TestBreakerDegraded(t *testing.T) {
	cases := map[string]struct {
		status *breakerStatus
		want   bool
	}{
		"absent is not degraded": {nil, false},
		"closed is not degraded": {&breakerStatus{State: "closed"}, false},
		"open is degraded":       {&breakerStatus{State: "open"}, true},
		"half-open is degraded":  {&breakerStatus{State: "half_open"}, true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := breakerDegraded(tc.status); got != tc.want {
				t.Fatalf("breakerDegraded() = %v, want %v", got, tc.want)
			}
		})
	}
}
