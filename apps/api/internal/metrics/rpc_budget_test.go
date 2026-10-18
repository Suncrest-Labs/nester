package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func labeledGaugeValue(t *testing.T, reg *prometheus.Registry, name string, wantLabels map[string]string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			matched := true
			for k, v := range wantLabels {
				found := false
				for _, lp := range m.GetLabel() {
					if lp.GetName() == k && lp.GetValue() == v {
						found = true
						break
					}
				}
				if !found {
					matched = false
					break
				}
			}
			if matched && m.GetGauge() != nil {
				return m.GetGauge().GetValue()
			}
		}
	}
	t.Fatalf("gauge %s with labels %v not found", name, wantLabels)
	return 0
}

func TestRPCProviderBudgetCollector_ExposesUtilizationAndShedCounts(t *testing.T) {
	m := New()

	err := m.RegisterRPCProviderBudgets(func() []RPCProviderBudgetSample {
		return []RPCProviderBudgetSample{
			{
				Provider:         "soroban_rpc",
				UtilizationRatio: 0.85,
				AvailableTokens:  15,
				AllowedTotal:     420,
				ShedByPriority: map[string]uint64{
					"user_simulation": 37,
					"keeper":          2,
					"indexer":         0,
				},
			},
		}
	})
	if err != nil {
		t.Fatalf("RegisterRPCProviderBudgets: %v", err)
	}

	providerLabels := map[string]string{"provider": "soroban_rpc"}
	if got := labeledGaugeValue(t, m.Registry(), "nester_soroban_provider_budget_utilization_ratio", providerLabels); got != 0.85 {
		t.Errorf("utilization_ratio = %v, want 0.85", got)
	}
	if got := labeledGaugeValue(t, m.Registry(), "nester_soroban_provider_budget_available_tokens", providerLabels); got != 15 {
		t.Errorf("available_tokens = %v, want 15", got)
	}
	if got := counterValue(t, m.Registry(), "nester_soroban_provider_budget_allowed_total", providerLabels); got != 420 {
		t.Errorf("allowed_total = %v, want 420", got)
	}

	simLabels := map[string]string{"provider": "soroban_rpc", "priority": "user_simulation"}
	if got := counterValue(t, m.Registry(), "nester_soroban_provider_budget_shed_total", simLabels); got != 37 {
		t.Errorf("shed_total[user_simulation] = %v, want 37", got)
	}
}
