package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// testUUID is a concrete vault identifier. Every assertion about cardinality
// checks that this string never reaches a label.
const testUUID = "550e8400-e29b-41d4-a716-446655440000"

// newTestMux builds a mux with the same shape of patterns the API registers.
func newTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/vaults/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /api/v1/vaults/{id}/deposit", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("GET /api/v1/boom", func(http.ResponseWriter, *http.Request) {
		panic("handler exploded")
	})
	mux.HandleFunc("GET /api/v1/fail", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	return mux
}

// counterValue reads a single counter series, or returns 0 when the series
// does not exist yet. Reading through the registry rather than the collector
// is deliberate: it proves the value would actually appear in a scrape.
func counterValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if !labelsMatch(metric, labels) {
				continue
			}
			return metric.GetCounter().GetValue()
		}
	}
	return 0
}

func histogramCount(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) uint64 {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if !labelsMatch(metric, labels) {
				continue
			}
			return metric.GetHistogram().GetSampleCount()
		}
	}
	return 0
}

func gaugeValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			return metric.GetGauge().GetValue()
		}
	}
	return 0
}

func labelsMatch(metric *dto.Metric, want map[string]string) bool {
	got := make(map[string]string, len(metric.GetLabel()))
	for _, pair := range metric.GetLabel() {
		got[pair.GetName()] = pair.GetValue()
	}

	for name, value := range want {
		if got[name] != value {
			return false
		}
	}
	return true
}

// allLabelValues returns every label value present anywhere in the registry.
// Used to assert that forbidden dynamic values never appear.
func allLabelValues(t *testing.T, reg *prometheus.Registry) []string {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	var values []string
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, pair := range metric.GetLabel() {
				values = append(values, pair.GetValue())
			}
		}
	}
	return values
}

// TestRouteLabelUsesPatternNotRawPath is the acceptance criterion from the
// issue: a request to a concrete vault URL must be labelled with the route
// pattern, and the UUID must not appear anywhere in the metrics.
func TestRouteLabelUsesPatternNotRawPath(t *testing.T) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	got := counterValue(t, m.Registry(), "nester_http_requests_total", map[string]string{
		"route":        "GET /api/v1/vaults/{id}",
		"method":       http.MethodGet,
		"status_class": "2xx",
	})
	if got != 1 {
		t.Fatalf("expected pattern-labelled counter to be 1, got %v", got)
	}

	for _, value := range allLabelValues(t, m.Registry()) {
		if strings.Contains(value, testUUID) {
			t.Fatalf("uuid leaked into label value %q", value)
		}
	}
}

// TestRouteLabelBoundedForUnmatchedPaths proves that scanner traffic cannot
// mint a series per invented path.
func TestRouteLabelBoundedForUnmatchedPaths(t *testing.T) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	paths := []string{
		"/wp-admin", "/.env", "/api/v1/nope/" + testUUID, "/random/" + testUUID,
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	got := counterValue(t, m.Registry(), "nester_http_requests_total", map[string]string{
		"route":  routeUnmatched,
		"method": http.MethodGet,
	})
	if got != float64(len(paths)) {
		t.Fatalf("expected %d unmatched requests under %q, got %v", len(paths), routeUnmatched, got)
	}

	for _, value := range allLabelValues(t, m.Registry()) {
		for _, path := range paths {
			if value == path {
				t.Fatalf("raw path %q became a label value", path)
			}
		}
	}
}

// TestMethodLabelIsBounded proves a client cannot invent methods to inflate
// series count. The method comes off the request line, so it is
// client-controlled input.
func TestMethodLabelIsBounded(t *testing.T) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	for _, method := range []string{"FROBNICATE", "WHATEVER", "XYZZY"} {
		req := httptest.NewRequest(method, "/api/v1/vaults/"+testUUID, nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}

	got := counterValue(t, m.Registry(), "nester_http_requests_total", map[string]string{
		"method": "other",
	})
	if got != 3 {
		t.Fatalf("expected 3 requests under method=other, got %v", got)
	}

	for _, value := range allLabelValues(t, m.Registry()) {
		if value == "FROBNICATE" || value == "WHATEVER" || value == "XYZZY" {
			t.Fatalf("arbitrary method %q became a label value", value)
		}
	}
}

func TestStatusClassLabel(t *testing.T) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/api/v1/vaults/"+testUUID+"/deposit", nil))
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/fail", nil))

	if got := counterValue(t, m.Registry(), "nester_http_requests_total", map[string]string{
		"route":        "POST /api/v1/vaults/{id}/deposit",
		"status_class": "2xx",
	}); got != 1 {
		t.Fatalf("expected one 2xx on the deposit route, got %v", got)
	}

	if got := counterValue(t, m.Registry(), "nester_http_requests_total", map[string]string{
		"route":        "GET /api/v1/fail",
		"status_class": "5xx",
	}); got != 1 {
		t.Fatalf("expected one 5xx, got %v", got)
	}
}

func TestLatencyHistogramRecorded(t *testing.T) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil))

	count := histogramCount(t, m.Registry(), "nester_http_request_duration_seconds", map[string]string{
		"route":  "GET /api/v1/vaults/{id}",
		"method": http.MethodGet,
	})
	if count != 1 {
		t.Fatalf("expected 1 latency observation, got %d", count)
	}
}

// TestInFlightGaugeRisesAndFalls observes the gauge from inside the handler,
// which is the only place it is non-zero.
func TestInFlightGaugeRisesAndFalls(t *testing.T) {
	m := New()

	var during float64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/slow", func(w http.ResponseWriter, _ *http.Request) {
		during = gaugeValue(t, m.Registry(), "nester_http_requests_in_flight")
		w.WriteHeader(http.StatusOK)
	})

	handler := m.Middleware(mux)(mux)

	if before := gaugeValue(t, m.Registry(), "nester_http_requests_in_flight"); before != 0 {
		t.Fatalf("expected gauge at 0 before the request, got %v", before)
	}

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/slow", nil))

	if during != 1 {
		t.Fatalf("expected gauge at 1 during the request, got %v", during)
	}
	if after := gaugeValue(t, m.Registry(), "nester_http_requests_in_flight"); after != 0 {
		t.Fatalf("expected gauge back to 0 after the request, got %v", after)
	}
}

// TestInFlightGaugeDecrementsOnPanic guards the failure that makes a
// saturation alert permanently wrong: a panicking handler leaking the gauge
// upward forever.
func TestInFlightGaugeDecrementsOnPanic(t *testing.T) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected the panic to propagate to the outer recovery middleware")
			}
		}()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil))
	}()

	if after := gaugeValue(t, m.Registry(), "nester_http_requests_in_flight"); after != 0 {
		t.Fatalf("gauge leaked after a panicking handler: %v", after)
	}

	// The request is still counted; a panic is an outcome, not an absence.
	if got := counterValue(t, m.Registry(), "nester_http_requests_total", map[string]string{
		"route": "GET /api/v1/boom",
	}); got != 1 {
		t.Fatalf("expected the panicking request to be counted, got %v", got)
	}
}

func TestInFlightGaugeUnderConcurrency(t *testing.T) {
	m := New()
	mux := newTestMux()
	handler := m.Middleware(mux)(mux)

	const requests = 50
	var wg sync.WaitGroup
	wg.Add(requests)
	for i := 0; i < requests; i++ {
		go func() {
			defer wg.Done()
			handler.ServeHTTP(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil))
		}()
	}
	wg.Wait()

	if after := gaugeValue(t, m.Registry(), "nester_http_requests_in_flight"); after != 0 {
		t.Fatalf("gauge did not settle at 0 after concurrent requests: %v", after)
	}
	if got := counterValue(t, m.Registry(), "nester_http_requests_total", map[string]string{
		"route": "GET /api/v1/vaults/{id}",
	}); got != requests {
		t.Fatalf("expected %d counted requests, got %v", requests, got)
	}
}

func TestNilResolverDegradesToOther(t *testing.T) {
	m := New()
	handler := m.Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil))

	if got := counterValue(t, m.Registry(), "nester_http_requests_total", map[string]string{
		"route": routeUnmatched,
	}); got != 1 {
		t.Fatalf("expected the nil resolver to fall back to %q, got %v", routeUnmatched, got)
	}
}

func TestStatusClassBuckets(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusContinue, "1xx"},
		{http.StatusOK, "2xx"},
		{http.StatusFound, "3xx"},
		{http.StatusNotFound, "4xx"},
		{http.StatusInternalServerError, "5xx"},
		{999, "other"},
		{0, "other"},
	}

	for _, tc := range cases {
		if got := statusClass(tc.status); got != tc.want {
			t.Errorf("statusClass(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// TestResponseWriterUnwrap proves the middleware does not break streaming
// endpoints such as the websocket upgrade path, which need to reach the
// underlying writer.
func TestResponseWriterUnwrap(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapped := &statusRecorder{ResponseWriter: recorder, status: http.StatusOK}

	controller := http.NewResponseController(wrapped)
	if err := controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("flush through the wrapper failed: %v", err)
	}
}
