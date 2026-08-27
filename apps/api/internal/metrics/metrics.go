// Package metrics owns the Prometheus instrumentation for the API: the
// registry every collector attaches to, the metric definitions themselves,
// and the HTTP handler that exposes them.
//
// Cardinality policy (see docs/observability/metrics.md): every label must
// have a bounded, enumerable set of values and an operational reason to
// exist. Raw request paths, user IDs, wallet addresses, transaction hashes,
// request IDs, query parameters, and arbitrary error strings are never
// labels. A series count that grows with traffic eventually takes down the
// metrics backend, so new labels are justified individually.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Namespace prefixes every metric this package defines, so a scrape from a
// mixed environment can be attributed to the Go API without guessing.
const Namespace = "nester"

// requestDurationBuckets covers roughly 10ms to 10s. Library defaults top out
// at 10s but concentrate resolution below 1s, which is the wrong shape here:
// handlers that call Soroban RPC, Horizon, or the Anthropic relay routinely
// land in the 250ms-5s range, and that is exactly the band where an SLO is
// won or lost. The tail bucket at 10s plus +Inf keeps timeouts visible.
var requestDurationBuckets = []float64{
	0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// dependencyDurationBuckets are shared by the Redis and outbound-HTTP
// histograms. Redis commands are sub-millisecond when healthy, so the low end
// starts finer than the request histogram; the high end still reaches 10s
// because an outbound call to an external API can stall until its timeout.
var dependencyDurationBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// Metrics holds every collector the API records into. It is constructed once
// at startup and threaded to the instrumentation points; nothing in the
// request path ever creates a collector, because registration takes a lock
// and allocates, and doing it per request is both slow and a cardinality
// leak waiting to happen.
type Metrics struct {
	registry *prometheus.Registry

	// Request-level instrumentation.
	requestsTotal    *prometheus.CounterVec
	requestDuration  *prometheus.HistogramVec
	requestsInFlight prometheus.Gauge

	// Redis instrumentation.
	redisCommandsTotal   *prometheus.CounterVec
	redisCommandDuration *prometheus.HistogramVec
	redisErrorsTotal     *prometheus.CounterVec

	// Outbound HTTP instrumentation.
	outboundRequestsTotal *prometheus.CounterVec
	outboundDuration      *prometheus.HistogramVec
	outboundErrorsTotal   *prometheus.CounterVec

	// Service level indicators (nester#1056). Defined in slo.go; held here
	// so one scrape carries both the infrastructure and the product view.
	slo *sloCollectors
}

// New builds the registry and registers every collector on it.
//
// A dedicated registry rather than prometheus.DefaultRegisterer keeps the
// exposition free of collectors registered incidentally by dependencies, and
// lets tests construct an isolated Metrics without global state or the
// duplicate-registration panics that plague package-level metrics.
func New() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		registry: registry,

		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests handled, by route pattern, method, and status class.",
		}, []string{"route", "method", "status_class"}),

		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency in seconds, by route pattern and method.",
			Buckets:   requestDurationBuckets,
		}, []string{"route", "method"}),

		// Unlabelled on purpose: an in-flight gauge broken down by route
		// would need a series per route held for the process lifetime to
		// report a number that is almost always zero. The single value is
		// what saturation alerts actually read.
		requestsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: Namespace,
			Subsystem: "http",
			Name:      "requests_in_flight",
			Help:      "HTTP requests currently being served.",
		}),

		redisCommandsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "redis",
			Name:      "commands_total",
			Help:      "Total Redis commands issued, by command name.",
		}, []string{"command"}),

		redisCommandDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Subsystem: "redis",
			Name:      "command_duration_seconds",
			Help:      "Redis command latency in seconds, by command name.",
			Buckets:   dependencyDurationBuckets,
		}, []string{"command"}),

		redisErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "redis",
			Name:      "errors_total",
			Help:      "Total Redis commands that returned an error, by command name.",
		}, []string{"command"}),

		outboundRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "outbound",
			Name:      "requests_total",
			Help:      "Total outbound HTTP requests, by upstream, method, and status class.",
		}, []string{"upstream", "method", "status_class"}),

		outboundDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Subsystem: "outbound",
			Name:      "request_duration_seconds",
			Help:      "Outbound HTTP request latency in seconds, by upstream and method.",
			Buckets:   dependencyDurationBuckets,
		}, []string{"upstream", "method"}),

		// Transport-level failures (DNS, dial, TLS, timeout, context
		// cancellation) never produce a status code, so they cannot be
		// counted by status_class on requests_total. The kind label is a
		// closed set defined by classifyTransportError, never the raw
		// error string, which would be unbounded.
		outboundErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Subsystem: "outbound",
			Name:      "errors_total",
			Help:      "Total outbound HTTP requests that failed before a response, by upstream and error kind.",
		}, []string{"upstream", "kind"}),

		slo: newSLOCollectors(),
	}

	registry.MustRegister(
		m.requestsTotal,
		m.requestDuration,
		m.requestsInFlight,
		m.redisCommandsTotal,
		m.redisCommandDuration,
		m.redisErrorsTotal,
		m.outboundRequestsTotal,
		m.outboundDuration,
		m.outboundErrorsTotal,
	)

	registry.MustRegister(m.slo.collectors()...)

	// Process and Go runtime collectors: goroutine count, heap, GC pauses,
	// open file descriptors, CPU. Free to collect and the first thing anyone
	// looks at when the API misbehaves for reasons unrelated to a route.
	registry.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	return m
}

// Registry exposes the underlying registry so additional collectors — the
// pgxpool stats collector, for instance — can attach after construction.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// Handler returns the Prometheus exposition handler for this registry.
//
// The caller is responsible for placing it behind the internal listener; see
// Server in server.go. Registering it on the public router would publish
// internal route names and traffic volumes to anyone who asks.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		// A broken collector should degrade the scrape, not 500 the whole
		// endpoint and blind every other metric.
		ErrorHandling: promhttp.ContinueOnError,
		// Scrapes are cheap and infrequent; capping concurrency stops a
		// misconfigured Prometheus from amplifying load onto the process
		// collectors.
		MaxRequestsInFlight: 4,
	})
}
