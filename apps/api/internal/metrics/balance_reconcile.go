package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// BalanceReconcileAgeSource reports how long ago the vault-balance reconciler
// finished a pass, and whether the series should be emitted at all. It returns
// false on a replica that is not the elected leader (only the instance doing
// the work reports an age) and on a runner that was never started.
type BalanceReconcileAgeSource func() (age time.Duration, emit bool)

// balanceReconcileCollector exposes the vault-balance reconciler's liveness
// (nester#1082) as a scrape-time series, following FreshnessCollector: a
// pushed gauge would freeze at its last healthy value when the reconciler
// dies, which is exactly the failure mode a liveness metric exists to expose.
// Deriving the age at scrape time from the runner's last-pass timestamp means
// the number keeps climbing on the clock even when nothing of ours runs.
//
// When the source declines to emit — non-leader replica, runner never started
// — the series is absent rather than zero. Absent is safe: emitting zero
// would claim "just reconciled", the one value that always looks perfect, and
// the BalanceReconciliationMetricsAbsent alert covers the dark case.
type balanceReconcileCollector struct {
	source BalanceReconcileAgeSource
	age    *prometheus.Desc
}

func newBalanceReconcileCollector(source BalanceReconcileAgeSource) *balanceReconcileCollector {
	return &balanceReconcileCollector{
		source: source,
		age: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "reconcile", "balance_last_run_age_seconds"),
			"Seconds since the vault-balance reconciler finished a pass, derived at scrape time. Absent on non-leader replicas and when the reconciler is not running.",
			nil, nil,
		),
	}
}

// Describe implements prometheus.Collector.
func (c *balanceReconcileCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.age
}

// Collect implements prometheus.Collector.
func (c *balanceReconcileCollector) Collect(ch chan<- prometheus.Metric) {
	if c.source == nil {
		return
	}
	age, emit := c.source()
	if !emit {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.age, prometheus.GaugeValue, age.Seconds())
}

// RegisterBalanceReconcileAge attaches the vault-balance reconciler liveness
// collector to the registry. A nil source is a no-op so callers do not need
// to branch when the reconciler is not constructed.
func (m *Metrics) RegisterBalanceReconcileAge(source BalanceReconcileAgeSource) error {
	if m == nil || source == nil {
		return nil
	}
	return m.registry.Register(newBalanceReconcileCollector(source))
}
