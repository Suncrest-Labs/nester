package metrics

import (
	"net/http"
	"time"
)

// routeUnmatched is the label used for requests that no registered route
// matches — 404s, probes, and scanner traffic. Without it, every unmatched
// path a scanner invents would mint a new series, which is the exact
// cardinality explosion this package exists to prevent.
const routeUnmatched = "other"

// PatternResolver maps a request to the route pattern that will serve it.
//
// http.ServeMux implements this via its Handler method, which performs the
// same match ServeHTTP will and returns the registered pattern (for example
// "GET /api/v1/vaults/{id}") without consuming the request. That matters
// because middleware wraps the mux from the outside, where r.Pattern is
// still empty: the mux has not matched yet, and it only populates r.Pattern
// on the request it passes down to the handler.
type PatternResolver interface {
	Handler(r *http.Request) (h http.Handler, pattern string)
}

// statusRecorder captures the response status for the status_class label.
//
// A handler that writes a body without calling WriteHeader implicitly sends
// 200, which is why status starts there rather than at zero.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer, so
// wrapping a handler in this middleware does not silently break flushing or
// deadline control for streaming endpoints.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Middleware returns HTTP middleware that records request count, latency, and
// in-flight count.
//
// resolver supplies the route pattern; pass the *http.ServeMux the request
// will ultimately reach. A nil resolver degrades to the "other" route label
// rather than panicking, so a partially wired test server still serves
// traffic.
func (m *Metrics) Middleware(resolver PatternResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := resolveRoute(resolver, r)
			method := normalizeMethod(r.Method)

			m.requestsInFlight.Inc()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			startedAt := time.Now()

			// Deferred so the gauge is decremented and the observation
			// recorded even when a downstream handler panics. Without this,
			// a single panicking route leaks the gauge upward forever and
			// the saturation signal becomes permanently wrong. The panic
			// still propagates to RecoverPanic, which owns the response.
			defer func() {
				m.requestsInFlight.Dec()
				duration := time.Since(startedAt).Seconds()
				m.requestDuration.WithLabelValues(route, method).Observe(duration)
				m.requestsTotal.WithLabelValues(route, method, statusClass(recorder.status)).Inc()
			}()

			next.ServeHTTP(recorder, r)
		})
	}
}

// resolveRoute returns the bounded route label for a request.
//
// The returned value is always either a pattern registered at startup or the
// constant "other", so the label's value set is fixed by the route table and
// cannot be influenced by the client.
//
// This costs a second route match per request (~700ns against handlers that
// spend milliseconds in the database and on the network). The alternative —
// reading r.Pattern from a shim wrapped around each handler inside the mux —
// avoids the match but has to be applied at all ~93 registration sites, and a
// new route that forgets it is silently mislabelled as "other" rather than
// failing loudly. Resolving from the mux is correct by default for every
// route present and future, which is worth the sub-microsecond.
func resolveRoute(resolver PatternResolver, r *http.Request) string {
	if resolver == nil {
		return routeUnmatched
	}

	_, pattern := resolver.Handler(r)
	if pattern == "" {
		return routeUnmatched
	}

	return pattern
}

// normalizeMethod bounds the method label to the set the API actually serves.
//
// The method comes straight off the request line, so a client is free to send
// an arbitrary token there. Without this, "method" is attacker-controlled and
// unbounded — the same class of bug as labelling by raw path.
func normalizeMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodConnect,
		http.MethodTrace:
		return method
	default:
		return "other"
	}
}

// statusClass buckets a status code into 1xx-5xx.
//
// Five values instead of one per code: the exact code is available in logs
// and traces, whereas alerting reads error rate by class. This divides the
// series count on requests_total by roughly an order of magnitude.
//
// Returns one of these constants rather than building the string, so the hot
// path does not allocate on every request.
func statusClass(status int) string {
	switch status / 100 {
	case 1:
		return "1xx"
	case 2:
		return "2xx"
	case 3:
		return "3xx"
	case 4:
		return "4xx"
	case 5:
		return "5xx"
	default:
		// A handler that writes a nonsense code should not mint a series.
		return "other"
	}
}
