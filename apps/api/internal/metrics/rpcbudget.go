package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/suncrestlabs/nester/apps/api/internal/rpcbudget"
)

// RPCBudgetReader reports one provider's token-bucket limiter state.
// Implemented by *rpcbudget.Limiter.
type RPCBudgetReader interface {
	Snapshot() rpcbudget.Snapshot
}

// RPCBudgetCollector exposes per-provider outbound RPC budget utilisation and
// priority-class admission/shedding counters.
//
// Implemented as a pull collector so utilisation reflects token refill at
// scrape time and cannot drift stale when traffic stops.
type RPCBudgetCollector struct {
	readers map[Upstream]RPCBudgetReader

	utilization *prometheus.Desc
	available   *prometheus.Desc
	admitted    *prometheus.Desc
	shed        *prometheus.Desc
}

// NewRPCBudgetCollector builds a collector over the given provider limiters.
func NewRPCBudgetCollector(readers map[Upstream]RPCBudgetReader) *RPCBudgetCollector {
	const subsystem = "rpc_budget"

	name := func(n string) string {
		return prometheus.BuildFQName(Namespace, subsystem, n)
	}
	upstreamLabels := []string{"upstream"}
	priorityLabels := []string{"upstream", "priority"}

	return &RPCBudgetCollector{
		readers: readers,

		utilization: prometheus.NewDesc(name("utilization"),
			"Current fraction [0,1] of the outbound RPC token bucket consumed for a provider.",
			upstreamLabels, nil),

		available: prometheus.NewDesc(name("tokens_available"),
			"Current available tokens in the outbound RPC token bucket for a provider.",
			upstreamLabels, nil),

		admitted: prometheus.NewDesc(name("admitted_total"),
			"Outbound RPC calls admitted by the provider token bucket, by upstream and priority.",
			priorityLabels, nil),

		shed: prometheus.NewDesc(name("shed_total"),
			"Outbound RPC calls shed locally due to provider budget exhaustion, by upstream and priority.",
			priorityLabels, nil),
	}
}

// Describe implements prometheus.Collector.
func (c *RPCBudgetCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.utilization
	ch <- c.available
	ch <- c.admitted
	ch <- c.shed
}

// Collect implements prometheus.Collector.
func (c *RPCBudgetCollector) Collect(ch chan<- prometheus.Metric) {
	priorities := rpcbudget.AllPriorities()

	for upstream, reader := range c.readers {
		if reader == nil {
			continue
		}

		snap := reader.Snapshot()
		label := string(upstream)

		ch <- prometheus.MustNewConstMetric(c.utilization, prometheus.GaugeValue, snap.Utilization, label)
		ch <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, snap.Available, label)

		for _, p := range priorities {
			pLabel := p.String()
			ch <- prometheus.MustNewConstMetric(c.admitted, prometheus.CounterValue, float64(snap.Admitted[p]), label, pLabel)
			ch <- prometheus.MustNewConstMetric(c.shed, prometheus.CounterValue, float64(snap.Shed[p]), label, pLabel)
		}
	}
}

// RegisterRPCBudgets attaches an RPC budget collector to the registry.
// An empty or nil map is a no-op so callers need not branch when disabled.
func (m *Metrics) RegisterRPCBudgets(readers map[Upstream]RPCBudgetReader) error {
	if m == nil || len(readers) == 0 {
		return nil
	}
	return m.registry.Register(NewRPCBudgetCollector(readers))
}
