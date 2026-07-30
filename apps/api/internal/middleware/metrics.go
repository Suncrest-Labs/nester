package middleware

import (
	"net/http"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// Metrics records RED (rate, errors, duration) metrics for every HTTP request:
// an in-flight gauge held for the duration of the request, and a request
// counter plus duration histogram observed on completion. Metrics are labelled
// by matched route, HTTP method and response status.
func Metrics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metrics.IncInFlight()
			defer metrics.DecInFlight()

			startedAt := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			metrics.ObserveHTTP(routeLabel(r), r.Method, recorder.status, time.Since(startedAt).Seconds())
		})
	}
}

// routeLabel resolves a low-cardinality route label for a request. It prefers
// the matched ServeMux pattern (e.g. "GET /api/v1/vaults/{id}") so path
// parameters do not explode label cardinality, falling back to the request path
// when no pattern matched (e.g. 404s).
func routeLabel(r *http.Request) string {
	if pattern := r.Pattern; pattern != "" {
		return pattern
	}
	return r.URL.Path
}
