// Package metrics provides Prometheus instrumentation for the API: HTTP RED
// metrics (rate, errors, duration), runtime gauges for the PostgreSQL pool,
// Redis availability, and the Soroban event-indexer lag.
//
// All metrics are registered on a single package-level registry exposed through
// Handler, which serves the Prometheus exposition format at /metrics.
package metrics

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const namespace = "nester"

// registry is the private registry backing the exposition handler. A dedicated
// registry (rather than the global default) keeps the exposed metric families
// deterministic and testable.
var registry = prometheus.NewRegistry()

var (
	// httpRequestsTotal counts HTTP requests partitioned by route, method and
	// status. It is the "rate" and "errors" of the RED method (errors are the
	// subset with a 5xx status).
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests, partitioned by route, method and status.",
		},
		[]string{"route", "method", "status"},
	)

	// httpRequestErrorsTotal counts HTTP requests that resulted in a 5xx
	// response, giving a directly scrapeable error counter per route and method.
	httpRequestErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_errors_total",
			Help:      "Total number of HTTP requests that resulted in a 5xx response, partitioned by route and method.",
		},
		[]string{"route", "method"},
	)

	// httpRequestsInFlight tracks the number of HTTP requests currently being
	// served.
	httpRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "Number of HTTP requests currently being served.",
		},
	)

	// httpRequestDuration is the request-duration histogram, the "duration" of
	// the RED method.
	httpRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration in seconds, partitioned by route, method and status.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"route", "method", "status"},
	)

	// dbConnections reports PostgreSQL pgx pool connection counts by state
	// (acquired, idle, total).
	dbConnections = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "db",
			Name:      "connections",
			Help:      "PostgreSQL pgx pool connections partitioned by state (acquired, idle, total).",
		},
		[]string{"state"},
	)

	// redisUp is 1 when Redis is reachable, 0 otherwise.
	redisUp = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "redis",
			Name:      "up",
			Help:      "Whether Redis is reachable (1) or not (0).",
		},
	)

	// indexerLagLedgers is the number of ledgers the event indexer is behind the
	// latest observed ledger (latest_ledger - last_indexed_ledger).
	indexerLagLedgers = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "event_indexer",
			Name:      "lag_ledgers",
			Help:      "Event indexer lag in ledgers (latest_ledger - last_indexed_ledger).",
		},
	)
)

func init() {
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequestsTotal,
		httpRequestErrorsTotal,
		httpRequestsInFlight,
		httpRequestDuration,
		dbConnections,
		redisUp,
		indexerLagLedgers,
	)
}

// Handler returns an http.Handler that serves the registered metrics in the
// Prometheus exposition format.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry})
}

// Registry exposes the underlying registry for tests and advanced gatherers.
func Registry() *prometheus.Registry {
	return registry
}

// ObserveHTTP records a single completed HTTP request against the RED metrics.
// route is the matched route pattern (falling back to the request path when no
// pattern is available), method is the HTTP method, status is the response code
// and duration is the wall-clock time spent serving the request.
func ObserveHTTP(route, method string, status int, durationSeconds float64) {
	statusText := statusLabel(status)
	httpRequestsTotal.WithLabelValues(route, method, statusText).Inc()
	httpRequestDuration.WithLabelValues(route, method, statusText).Observe(durationSeconds)
	if status >= http.StatusInternalServerError {
		httpRequestErrorsTotal.WithLabelValues(route, method).Inc()
	}
}

// IncInFlight increments the in-flight request gauge.
func IncInFlight() { httpRequestsInFlight.Inc() }

// DecInFlight decrements the in-flight request gauge.
func DecInFlight() { httpRequestsInFlight.Dec() }

// SetDBConnections records the current PostgreSQL pool connection counts.
func SetDBConnections(acquired, idle, total float64) {
	dbConnections.WithLabelValues("acquired").Set(acquired)
	dbConnections.WithLabelValues("idle").Set(idle)
	dbConnections.WithLabelValues("total").Set(total)
}

// SetRedisUp records Redis availability (true -> 1, false -> 0).
func SetRedisUp(up bool) {
	if up {
		redisUp.Set(1)
		return
	}
	redisUp.Set(0)
}

// SetIndexerLag records the event-indexer lag in ledgers.
func SetIndexerLag(lag float64) { indexerLagLedgers.Set(lag) }

// statusLabel renders an HTTP status code as its string label.
func statusLabel(status int) string {
	if status == 0 {
		status = http.StatusOK
	}
	return strconv.Itoa(status)
}
