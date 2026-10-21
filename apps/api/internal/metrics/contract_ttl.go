package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type contractTTLCollectors struct {
	ledgersRemaining *prometheus.GaugeVec
}

func newContractTTLCollectors() *contractTTLCollectors {
	return &contractTTLCollectors{
		ledgersRemaining: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      "contract_entry_ttl_ledgers_remaining",
			Help:      "Remaining ledgers before a Soroban contract instance or storage entry expires, by contract and entry kind.",
		}, []string{"contract", "kind"}),
	}
}

func (c *contractTTLCollectors) collectors() []prometheus.Collector {
	return []prometheus.Collector{c.ledgersRemaining}
}

// SetContractEntryTTLLedgersRemaining records the remaining TTL in ledgers for a contract entry.
func (m *Metrics) SetContractEntryTTLLedgersRemaining(contract, kind string, ledgersRemaining float64) {
	if m == nil || m.contractTTL == nil {
		return
	}
	m.contractTTL.ledgersRemaining.WithLabelValues(contract, kind).Set(ledgersRemaining)
}
