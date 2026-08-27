package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// Stater is the subset of *pgxpool.Pool this collector needs. Narrowing to an
// interface keeps the collector testable without a live database.
type Stater interface {
	Stat() *pgxpool.Stat
}

// PoolCollector exposes pgxpool statistics.
//
// It is a pull collector rather than a set of gauges updated on a ticker:
// pgxpool already maintains these counters internally, so the values are read
// at scrape time straight from pool.Stat(). Nothing is sampled between
// scrapes, no goroutine is spawned, and the numbers can never be stale
// relative to the scrape that reports them.
//
// No label carries a query, a DSN, or any connection detail — this reports
// pool shape only, so it cannot leak SQL parameters or credentials.
type PoolCollector struct {
	pool Stater

	acquiredConns      *prometheus.Desc
	idleConns          *prometheus.Desc
	totalConns         *prometheus.Desc
	maxConns           *prometheus.Desc
	constructingConns  *prometheus.Desc
	newConnsTotal      *prometheus.Desc
	acquireCount       *prometheus.Desc
	acquireDuration    *prometheus.Desc
	emptyAcquire       *prometheus.Desc
	canceledAcquire    *prometheus.Desc
	maxLifetimeDestroy *prometheus.Desc
	maxIdleDestroy     *prometheus.Desc
}

// NewPoolCollector builds a collector for the given pool.
func NewPoolCollector(pool Stater) *PoolCollector {
	const subsystem = "db_pool"

	name := func(n string) string {
		return prometheus.BuildFQName(Namespace, subsystem, n)
	}

	return &PoolCollector{
		pool: pool,

		acquiredConns: prometheus.NewDesc(name("acquired_connections"),
			"Connections currently checked out of the pool.", nil, nil),
		idleConns: prometheus.NewDesc(name("idle_connections"),
			"Connections currently idle in the pool.", nil, nil),
		totalConns: prometheus.NewDesc(name("total_connections"),
			"Connections currently owned by the pool, idle and in use.", nil, nil),
		maxConns: prometheus.NewDesc(name("max_connections"),
			"Maximum connections the pool is configured to open.", nil, nil),
		constructingConns: prometheus.NewDesc(name("constructing_connections"),
			"Connections currently being established.", nil, nil),

		newConnsTotal: prometheus.NewDesc(name("new_connections_total"),
			"Total connections the pool has opened.", nil, nil),
		acquireCount: prometheus.NewDesc(name("acquires_total"),
			"Total successful connection acquisitions.", nil, nil),

		// The two saturation signals. empty_acquire_waits_total counts
		// acquisitions that had to wait because no connection was free;
		// acquire_wait_seconds_total is the time spent in those waits. Both
		// stay flat on a healthy pool and climb the moment it saturates,
		// which is precisely the failure this issue calls out as invisible
		// today. Rate them together for mean wait per blocked acquire.
		emptyAcquire: prometheus.NewDesc(name("empty_acquire_waits_total"),
			"Total acquisitions that had to wait for a connection to be returned.", nil, nil),
		acquireDuration: prometheus.NewDesc(name("acquire_wait_seconds_total"),
			"Cumulative seconds spent waiting to acquire a connection.", nil, nil),

		canceledAcquire: prometheus.NewDesc(name("canceled_acquires_total"),
			"Total acquisitions aborted by context cancellation.", nil, nil),
		maxLifetimeDestroy: prometheus.NewDesc(name("max_lifetime_destroys_total"),
			"Total connections closed because they exceeded max lifetime.", nil, nil),
		maxIdleDestroy: prometheus.NewDesc(name("max_idle_destroys_total"),
			"Total connections closed because they exceeded max idle time.", nil, nil),
	}
}

// Describe implements prometheus.Collector.
func (c *PoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.acquiredConns
	ch <- c.idleConns
	ch <- c.totalConns
	ch <- c.maxConns
	ch <- c.constructingConns
	ch <- c.newConnsTotal
	ch <- c.acquireCount
	ch <- c.acquireDuration
	ch <- c.emptyAcquire
	ch <- c.canceledAcquire
	ch <- c.maxLifetimeDestroy
	ch <- c.maxIdleDestroy
}

// Collect implements prometheus.Collector.
func (c *PoolCollector) Collect(ch chan<- prometheus.Metric) {
	if c.pool == nil {
		return
	}

	stat := c.pool.Stat()
	if stat == nil {
		return
	}

	gauge := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, v)
	}
	counter := func(d *prometheus.Desc, v float64) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.CounterValue, v)
	}

	gauge(c.acquiredConns, float64(stat.AcquiredConns()))
	gauge(c.idleConns, float64(stat.IdleConns()))
	gauge(c.totalConns, float64(stat.TotalConns()))
	gauge(c.maxConns, float64(stat.MaxConns()))
	gauge(c.constructingConns, float64(stat.ConstructingConns()))

	counter(c.newConnsTotal, float64(stat.NewConnsCount()))
	counter(c.acquireCount, float64(stat.AcquireCount()))
	counter(c.acquireDuration, stat.AcquireDuration().Seconds())
	counter(c.emptyAcquire, float64(stat.EmptyAcquireCount()))
	counter(c.canceledAcquire, float64(stat.CanceledAcquireCount()))
	counter(c.maxLifetimeDestroy, float64(stat.MaxLifetimeDestroyCount()))
	counter(c.maxIdleDestroy, float64(stat.MaxIdleDestroyCount()))
}

// RegisterPool attaches a pool collector to the registry.
//
// A nil pool is a no-op so callers do not need to branch; the API can boot
// without a database in some test and tooling paths.
func (m *Metrics) RegisterPool(pool Stater) error {
	if pool == nil {
		return nil
	}
	return m.registry.Register(NewPoolCollector(pool))
}
