package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestHandlerServesExposition verifies the handler responds with 200 and emits
// the Prometheus exposition format containing every application metric family.
func TestHandlerServesExposition(t *testing.T) {
	// Touch each metric so the label-bearing families appear in the output.
	ObserveHTTP("GET /health", http.MethodGet, http.StatusOK, 0.01)
	SetDBConnections(1, 2, 3)
	SetRedisUp(true)
	SetIndexerLag(0)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d, want %d", rec.Code, http.StatusOK)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := string(body)

	families := []string{
		"nester_http_requests_total",
		"nester_http_request_errors_total",
		"nester_http_requests_in_flight",
		"nester_http_request_duration_seconds",
		"nester_db_connections",
		"nester_redis_up",
		"nester_event_indexer_lag_ledgers",
	}
	for _, family := range families {
		if !strings.Contains(out, family) {
			t.Errorf("metrics output missing family %q", family)
		}
	}
}

// TestObserveHTTPCountsRequests verifies the request counter increments for the
// observed route/method/status label set.
func TestObserveHTTPCountsRequests(t *testing.T) {
	const (
		route  = "GET /test/counts"
		method = http.MethodGet
	)

	before := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(route, method, "200"))
	ObserveHTTP(route, method, http.StatusOK, 0.02)
	after := testutil.ToFloat64(httpRequestsTotal.WithLabelValues(route, method, "200"))

	if after-before != 1 {
		t.Fatalf("request counter delta: got %v, want 1", after-before)
	}
}

// TestObserveHTTPCountsErrors verifies the dedicated error counter only
// increments for 5xx responses.
func TestObserveHTTPCountsErrors(t *testing.T) {
	const (
		route  = "GET /test/errors"
		method = http.MethodGet
	)

	before := testutil.ToFloat64(httpRequestErrorsTotal.WithLabelValues(route, method))

	// A 4xx must not increment the error counter.
	ObserveHTTP(route, method, http.StatusBadRequest, 0.02)
	if got := testutil.ToFloat64(httpRequestErrorsTotal.WithLabelValues(route, method)); got != before {
		t.Fatalf("error counter changed on 4xx: got %v, want %v", got, before)
	}

	// A 5xx must increment the error counter.
	ObserveHTTP(route, method, http.StatusInternalServerError, 0.02)
	if got := testutil.ToFloat64(httpRequestErrorsTotal.WithLabelValues(route, method)); got-before != 1 {
		t.Fatalf("error counter delta on 5xx: got %v, want 1", got-before)
	}
}

// TestSetRedisUp verifies the gauge maps availability to 1/0.
func TestSetRedisUp(t *testing.T) {
	SetRedisUp(true)
	if got := testutil.ToFloat64(redisUp); got != 1 {
		t.Errorf("redis up gauge: got %v, want 1", got)
	}

	SetRedisUp(false)
	if got := testutil.ToFloat64(redisUp); got != 0 {
		t.Errorf("redis down gauge: got %v, want 0", got)
	}
}

// TestStatusLabel verifies status codes render as their numeric label and that
// a zero status defaults to 200.
func TestStatusLabel(t *testing.T) {
	cases := map[int]string{
		0:                            "200",
		http.StatusOK:                "200",
		http.StatusNotFound:          "404",
		http.StatusInternalServerError: "500",
	}
	for status, want := range cases {
		if got := statusLabel(status); got != want {
			t.Errorf("statusLabel(%d): got %q, want %q", status, got, want)
		}
	}
}
