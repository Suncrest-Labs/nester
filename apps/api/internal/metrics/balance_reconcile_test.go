package metrics

import (
	"testing"
	"time"
)

// The balance reconciler shares the runs counter but must NOT touch the
// transaction poller's age gauge: each loop's liveness stands alone, or a
// dead poller could hide behind a healthy balance reconciler (nester#1082).
func TestBalanceReconcileRunDoesNotResetPollerAge(t *testing.T) {
	m := New()

	m.SetReconcileLastRunAge(900 * time.Second)
	m.RecordBalanceReconcileRun(ReconcileCompleted)
	m.RecordBalanceReconcileRun(ReconcileFailed)

	if got := counterValue(t, m.Registry(), "nester_reconcile_runs_total", map[string]string{"outcome": "completed"}); got != 1 {
		t.Fatalf("completed runs = %v, want 1", got)
	}
	if got := counterValue(t, m.Registry(), "nester_reconcile_runs_total", map[string]string{"outcome": "failed"}); got != 1 {
		t.Fatalf("failed runs = %v, want 1", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_reconcile_last_run_age_seconds"); got != 900 {
		t.Fatalf("poller age after balance run = %v, want 900 (untouched)", got)
	}
}

// balanceSeriesValue gathers the balance age series, reporting whether it was
// present at all — its absence is load-bearing (see the collector doc).
func balanceSeriesValue(t *testing.T, m *Metrics) (float64, bool) {
	t.Helper()
	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "nester_reconcile_balance_last_run_age_seconds" {
			continue
		}
		metricsInFamily := family.GetMetric()
		if len(metricsInFamily) != 1 {
			t.Fatalf("expected 1 series, got %d", len(metricsInFamily))
		}
		return metricsInFamily[0].GetGauge().GetValue(), true
	}
	return 0, false
}

func TestBalanceReconcileAgeIsDerivedAtScrapeTime(t *testing.T) {
	m := New()

	age := 90 * time.Second
	emit := true
	if err := m.RegisterBalanceReconcileAge(func() (time.Duration, bool) {
		return age, emit
	}); err != nil {
		t.Fatalf("RegisterBalanceReconcileAge() error = %v", err)
	}

	got, present := balanceSeriesValue(t, m)
	if !present {
		t.Fatal("expected the balance age series to be present")
	}
	if got != 90 {
		t.Fatalf("balance age = %v, want 90", got)
	}

	// The value moves per scrape — nothing of ours has to run for it to
	// change, which is the point of the pull collector.
	age = 2000 * time.Second
	got, _ = balanceSeriesValue(t, m)
	if got != 2000 {
		t.Fatalf("balance age after source change = %v, want 2000", got)
	}

	// A source that declines to emit (non-leader, never started) removes the
	// series rather than publishing zero — zero is the one value that always
	// looks perfect.
	emit = false
	if _, present := balanceSeriesValue(t, m); present {
		t.Fatal("expected the series to be absent when the source declines to emit")
	}
}

func TestRegisterBalanceReconcileAgeNilSourceIsNoop(t *testing.T) {
	m := New()
	if err := m.RegisterBalanceReconcileAge(nil); err != nil {
		t.Fatalf("nil source should be a no-op, got %v", err)
	}
	if _, present := balanceSeriesValue(t, m); present {
		t.Fatal("no source registered but series present")
	}
}
