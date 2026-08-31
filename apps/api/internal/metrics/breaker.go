package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/suncrestlabs/nester/apps/api/internal/breaker"
)

// BreakerReader reports one circuit breaker's current state. Implemented by
// *breaker.Breaker.
type BreakerReader interface {
	Snapshot() breaker.Snapshot
}

// BreakerCollector exposes circuit breaker state for the chain upstreams
// (nester#1087).
//
// A pull collector, like PoolCollector and FreshnessCollector, and for a
// reason specific to this state machine: an open breaker becomes half-open
// when its open period elapses, which is a function of the clock rather than
// of anything happening. A pushed gauge would keep reporting "open" until the
// next call arrived to move it, so the metric would disagree with what the
// breaker would actually do. Reading at scrape time cannot drift.
//
// The upstream label is the bounded Upstream constant set, never a URL or a
// host: a redirect or a per-environment endpoint would otherwise mint series,
// and a URL can carry credentials in its userinfo.
type BreakerCollector struct {
	readers map[Upstream]BreakerReader

	state        *prometheus.Desc
	failureRatio *prometheus.Desc
	rejected     *prometheus.Desc
}

// NewBreakerCollector builds a collector over the given breakers.
func NewBreakerCollector(readers map[Upstream]BreakerReader) *BreakerCollector {
	const subsystem = "circuit_breaker"

	name := func(n string) string {
		return prometheus.BuildFQName(Namespace, subsystem, n)
	}
	labels := []string{"upstream"}

	return &BreakerCollector{
		readers: readers,

		// 0 = closed, 1 = half_open, 2 = open. Ascending with severity, so
		// `> 0` reads as "not fully healthy" and max() across replicas is the
		// worst state rather than an arbitrary one.
		state: prometheus.NewDesc(name("state"),
			"Circuit breaker state for a chain upstream: 0 closed, 1 half-open, 2 open.",
			labels, nil),

		// The number the state is derived from. During an incident the useful
		// question is not only "has it tripped" but "how close is it", and
		// after recovery this is what shows the window draining.
		failureRatio: prometheus.NewDesc(name("failure_ratio"),
			"Observed failure ratio within the breaker's rolling window.",
			labels, nil),

		// Calls the breaker refused without touching the network. This is the
		// load actually shed, and it is why rejections are not logged
		// individually.
		rejected: prometheus.NewDesc(name("rejected_total"),
			"Requests rejected without contacting the upstream because the breaker was not admitting calls.",
			labels, nil),
	}
}

// Describe implements prometheus.Collector.
func (c *BreakerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.state
	ch <- c.failureRatio
	ch <- c.rejected
}

// Collect implements prometheus.Collector.
func (c *BreakerCollector) Collect(ch chan<- prometheus.Metric) {
	for upstream, reader := range c.readers {
		if reader == nil {
			continue
		}

		snapshot := reader.Snapshot()
		label := string(upstream)

		ch <- prometheus.MustNewConstMetric(c.state, prometheus.GaugeValue, float64(snapshot.State), label)
		ch <- prometheus.MustNewConstMetric(c.failureRatio, prometheus.GaugeValue, snapshot.FailureRatio, label)
		ch <- prometheus.MustNewConstMetric(c.rejected, prometheus.CounterValue, float64(snapshot.Rejected), label)
	}
}

// RegisterBreakers attaches a breaker collector to the registry.
//
// An empty or nil map is a no-op so callers need not branch when the breaker
// is disabled.
func (m *Metrics) RegisterBreakers(readers map[Upstream]BreakerReader) error {
	if m == nil || len(readers) == 0 {
		return nil
	}
	return m.registry.Register(NewBreakerCollector(readers))
}
