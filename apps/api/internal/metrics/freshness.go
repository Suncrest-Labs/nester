package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
)

// FreshnessCollector exposes the balance-freshness SLI: how far behind the
// chain the event indexer is, in ledgers and in seconds (nester#1088).
//
// It is a pull collector, following PoolCollector, and that is the substance
// of this file rather than a stylistic preference. These series were
// previously gauges the indexer pushed on each successful poll, which meant a
// dead indexer left every one of them frozen at its last healthy value: lag 0,
// sample age 0, alerts silent, dashboards green. Deriving them at scrape time
// from freshness.Tracker instead means the numbers keep moving on the clock,
// so an indexer that stops — wedged, panicked, or never started — is visible
// without anything of ours still having to run.
//
// No metric here carries a label. The freshness of the indexed view is a
// single process-wide fact, so there is nothing to break it down by and no way
// for traffic to move the series count.
type FreshnessCollector struct {
	reader freshness.Reader

	lagLedgers   *prometheus.Desc
	lagSeconds   *prometheus.Desc
	sampleAge    *prometheus.Desc
	budget       *prometheus.Desc
	sampleErrors *prometheus.Desc
}

// NewFreshnessCollector builds a collector over the given freshness source.
func NewFreshnessCollector(reader freshness.Reader) *FreshnessCollector {
	const subsystem = "indexer"

	name := func(n string) string {
		return prometheus.BuildFQName(Namespace, subsystem, n)
	}

	return &FreshnessCollector{
		reader: reader,

		lagLedgers: prometheus.NewDesc(name("lag_ledgers"),
			"Network ledger tip minus last successfully indexed ledger.", nil, nil),

		// The seconds view of the same lag, and the one the staleness budget
		// and the API contract are stated in. It includes the age of the last
		// sample, so it climbs while the indexer is not reporting rather than
		// holding at the last observed value.
		lagSeconds: prometheus.NewDesc(name("lag_seconds"),
			"Age of the indexed view of the chain in seconds: time since the last freshness sample plus that sample's ledger lag.", nil, nil),

		sampleAge: prometheus.NewDesc(name("lag_last_sample_age_seconds"),
			"Seconds since the indexer last published a freshness sample.", nil, nil),

		// Published so alert rules can compare lag against the budget the
		// application is actually enforcing, instead of restating the number
		// in PromQL where it can drift from the API's answer.
		budget: prometheus.NewDesc(name("staleness_budget_seconds"),
			"Configured staleness budget: indexed data older than this is served and alerted on as stale.", nil, nil),

		sampleErrors: prometheus.NewDesc(name("lag_sample_errors_total"),
			"Failed attempts to sample indexer lag.", nil, nil),
	}
}

// Describe implements prometheus.Collector.
func (c *FreshnessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.lagLedgers
	ch <- c.lagSeconds
	ch <- c.sampleAge
	ch <- c.budget
	ch <- c.sampleErrors
}

// Collect implements prometheus.Collector.
func (c *FreshnessCollector) Collect(ch chan<- prometheus.Metric) {
	if c.reader == nil {
		return
	}

	snapshot := c.reader.Snapshot()

	gauge := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
	}

	gauge(c.lagSeconds, snapshot.Lag.Seconds())
	gauge(c.sampleAge, snapshot.SampleAge.Seconds())
	gauge(c.budget, snapshot.Budget.Seconds())
	ch <- prometheus.MustNewConstMetric(c.sampleErrors, prometheus.CounterValue, float64(snapshot.SampleErrors))

	// Ledger lag is omitted until the indexer has published a position.
	// Emitting zero before the first sample would claim the indexer is exactly
	// at the tip on every cold start — the most dangerous value this gauge can
	// hold, because it is the one that looks perfect. The absence is safe:
	// lag_seconds above is always present, keeps climbing, and is what pages.
	if snapshot.Sampled {
		gauge(c.lagLedgers, float64(snapshot.LagLedgers))
	}
}

// RegisterFreshness attaches a freshness collector to the registry.
//
// A nil reader is a no-op so callers do not need to branch; the API can boot
// without an indexer in some tooling paths.
func (m *Metrics) RegisterFreshness(reader freshness.Reader) error {
	if m == nil || reader == nil {
		return nil
	}
	return m.registry.Register(NewFreshnessCollector(reader))
}
