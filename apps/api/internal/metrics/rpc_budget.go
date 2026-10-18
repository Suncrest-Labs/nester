package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// RPCProviderBudgetSample is the scrape-time snapshot of a provider's outbound
// RPC rate-limit token bucket (#1560).
type RPCProviderBudgetSample struct {
	Provider         string
	UtilizationRatio float64
	AvailableTokens  float64
	AllowedTotal     uint64
	ShedByPriority   map[string]uint64
}

// RPCProviderBudgetSource returns the current token-bucket budget snapshot for
// one or more Soroban RPC providers.
type RPCProviderBudgetSource func() []RPCProviderBudgetSample

type rpcProviderBudgetCollector struct {
	source           RPCProviderBudgetSource
	utilizationRatio *prometheus.Desc
	availableTokens  *prometheus.Desc
	allowedTotal     *prometheus.Desc
	shedTotal        *prometheus.Desc
}

func newRPCProviderBudgetCollector(source RPCProviderBudgetSource) *rpcProviderBudgetCollector {
	const subsystem = "soroban_provider_budget"
	name := func(n string) string {
		return prometheus.BuildFQName(Namespace, subsystem, n)
	}

	return &rpcProviderBudgetCollector{
		source: source,
		utilizationRatio: prometheus.NewDesc(
			name("utilization_ratio"),
			"Current token-bucket budget utilization ratio (0.0 to 1.0) per Soroban RPC provider.",
			[]string{"provider"}, nil,
		),
		availableTokens: prometheus.NewDesc(
			name("available_tokens"),
			"Remaining available burst tokens in the Soroban RPC provider token bucket.",
			[]string{"provider"}, nil,
		),
		allowedTotal: prometheus.NewDesc(
			name("allowed_total"),
			"Total outbound RPC calls admitted by the provider token bucket.",
			[]string{"provider"}, nil,
		),
		shedTotal: prometheus.NewDesc(
			name("shed_total"),
			"Total outbound RPC calls shed due to provider budget exhaustion, by provider and priority class.",
			[]string{"provider", "priority"}, nil,
		),
	}
}

func (c *rpcProviderBudgetCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.utilizationRatio
	ch <- c.availableTokens
	ch <- c.allowedTotal
	ch <- c.shedTotal
}

func (c *rpcProviderBudgetCollector) Collect(ch chan<- prometheus.Metric) {
	if c.source == nil {
		return
	}
	for _, sample := range c.source() {
		if sample.Provider == "" {
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.utilizationRatio, prometheus.GaugeValue, sample.UtilizationRatio, sample.Provider)
		ch <- prometheus.MustNewConstMetric(c.availableTokens, prometheus.GaugeValue, sample.AvailableTokens, sample.Provider)
		ch <- prometheus.MustNewConstMetric(c.allowedTotal, prometheus.CounterValue, float64(sample.AllowedTotal), sample.Provider)
		for _, priority := range []string{"user_simulation", "keeper", "indexer"} {
			count := sample.ShedByPriority[priority]
			ch <- prometheus.MustNewConstMetric(c.shedTotal, prometheus.CounterValue, float64(count), sample.Provider, priority)
		}
	}
}

// RegisterRPCProviderBudgets attaches a scrape-time collector for per-provider
// outbound RPC token-bucket budget utilization and priority shedding (#1560).
func (m *Metrics) RegisterRPCProviderBudgets(source RPCProviderBudgetSource) error {
	if m == nil || source == nil {
		return nil
	}
	return m.registry.Register(newRPCProviderBudgetCollector(source))
}
