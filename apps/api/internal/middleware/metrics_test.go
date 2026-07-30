package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// TestMetricsMiddlewareRecordsRequest drives a request through a ServeMux so the
// matched pattern becomes the route label, then asserts the request counter for
// that label set incremented by one.
func TestMetricsMiddlewareRecordsRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /widgets/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := Metrics()(mux)

	const route = "GET /widgets/{id}"
	before := requestCount(t, route, http.MethodGet, "200")

	req := httptest.NewRequest(http.MethodGet, "/widgets/42", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d, want %d", rec.Code, http.StatusOK)
	}

	after := requestCount(t, route, http.MethodGet, "200")
	if after-before != 1 {
		t.Fatalf("request counter delta: got %v, want 1", after-before)
	}
}

// TestMetricsMiddlewareRecordsError verifies a 5xx response served through the
// middleware increments the error counter for the matched route.
func TestMetricsMiddlewareRecordsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	handler := Metrics()(mux)

	const route = "GET /boom"
	before := errorCount(t, route, http.MethodGet)

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: got %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	after := errorCount(t, route, http.MethodGet)
	if after-before != 1 {
		t.Fatalf("error counter delta: got %v, want 1", after-before)
	}
}

// requestCount reads the current value of nester_http_requests_total for a
// specific label set from the metrics registry.
func requestCount(t *testing.T, route, method, status string) float64 {
	t.Helper()
	return counterValue(t, "nester_http_requests_total", map[string]string{
		"route":  route,
		"method": method,
		"status": status,
	})
}

// errorCount reads the current value of nester_http_request_errors_total for a
// specific label set from the metrics registry.
func errorCount(t *testing.T, route, method string) float64 {
	t.Helper()
	return counterValue(t, "nester_http_request_errors_total", map[string]string{
		"route":  route,
		"method": method,
	})
}

// counterValue gathers the metrics registry and returns the counter value for
// the family/label combination, or 0 when the series does not yet exist.
func counterValue(t *testing.T, family string, labels map[string]string) float64 {
	t.Helper()

	metricFamilies, err := metrics.Registry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, mf := range metricFamilies {
		if mf.GetName() != family {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// labelsMatch reports whether the gathered label pairs contain every wanted
// label with the expected value.
func labelsMatch(pairs []*dto.LabelPair, want map[string]string) bool {
	got := make(map[string]string, len(pairs))
	for _, p := range pairs {
		got[p.GetName()] = p.GetValue()
	}
	for name, value := range want {
		if got[name] != value {
			return false
		}
	}
	return true
}
