package metrics

import (
	"testing"
)

func stellarSeriesValues(t *testing.T, m *Metrics) (balance float64, reserve float64, present bool) {
	t.Helper()
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var foundBalance, foundReserve bool
	for _, family := range families {
		if family.GetName() == "nester_stellar_account_balance_xlm" {
			metricsInFamily := family.GetMetric()
			if len(metricsInFamily) == 1 {
				balance = metricsInFamily[0].GetGauge().GetValue()
				foundBalance = true
			}
		}
		if family.GetName() == "nester_stellar_account_safe_reserve_xlm" {
			metricsInFamily := family.GetMetric()
			if len(metricsInFamily) == 1 {
				reserve = metricsInFamily[0].GetGauge().GetValue()
				foundReserve = true
			}
		}
	}
	if foundBalance && foundReserve {
		return balance, reserve, true
	}
	return 0, 0, false
}

func TestStellarReserveCollector(t *testing.T) {
	m := New()

	balance := 500.0
	reserve := 100.0
	emit := true

	err := m.RegisterStellarAccountReserve("operational", func() (float64, float64, bool) {
		return balance, reserve, emit
	})
	if err != nil {
		t.Fatalf("RegisterStellarAccountReserve error = %v", err)
	}

	gotBal, gotRes, present := stellarSeriesValues(t, m)
	if !present {
		fatalErr := "expected stellar reserve metrics to be present"
		t.Fatal(fatalErr)
	}
	if gotBal != 500.0 || gotRes != 100.0 {
		t.Fatalf("got balance=%v, reserve=%v, want 500 and 100", gotBal, gotRes)
	}

	emit = false
	if _, _, present := stellarSeriesValues(t, m); present {
		t.Fatal("expected metrics to be absent when emit is false")
	}
}
