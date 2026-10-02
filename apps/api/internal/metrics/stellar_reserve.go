package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// StellarAccountBalanceSource reports the current XLM balance and safe reserve threshold
// of an operational or sponsored Stellar account.
type StellarAccountBalanceSource func() (balance float64, safeReserve float64, emit bool)

// stellarReserveCollector exposes operational Stellar account reserve levels as a scrape-time series.
type stellarReserveCollector struct {
	source      StellarAccountBalanceSource
	accountName string
	balance     *prometheus.Desc
	reserve     *prometheus.Desc
}

func newStellarReserveCollector(accountName string, source StellarAccountBalanceSource) *stellarReserveCollector {
	return &stellarReserveCollector{
		source:      source,
		accountName: accountName,
		balance: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "stellar", "account_balance_xlm"),
			"Current XLM balance of the operational Stellar account.",
			[]string{"account_name"}, nil,
		),
		reserve: prometheus.NewDesc(
			prometheus.BuildFQName(Namespace, "stellar", "account_safe_reserve_xlm"),
			"Safe XLM reserve threshold for the operational Stellar account.",
			[]string{"account_name"}, nil,
		),
	}
}

func (c *stellarReserveCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.balance
	ch <- c.reserve
}

func (c *stellarReserveCollector) Collect(ch chan<- prometheus.Metric) {
	if c.source == nil {
		return
	}
	balance, reserve, emit := c.source()
	if !emit {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.balance, prometheus.GaugeValue, balance, c.accountName)
	ch <- prometheus.MustNewConstMetric(c.reserve, prometheus.GaugeValue, reserve, c.accountName)
}

// RegisterStellarAccountReserve attaches a Stellar account balance and reserve monitor to the registry.
func (m *Metrics) RegisterStellarAccountReserve(accountName string, source StellarAccountBalanceSource) error {
	if m == nil || source == nil {
		return nil
	}
	return m.registry.Register(newStellarReserveCollector(accountName, source))
}
