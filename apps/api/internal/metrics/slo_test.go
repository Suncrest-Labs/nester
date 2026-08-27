package metrics

import (
	"testing"
	"time"
)

// A nil *Metrics must be safe on every recording path.
//
// The vault service holds an optional metrics pointer, so a service
// constructed without one — tests, tooling, or a wiring mistake — must behave
// exactly as before rather than panicking part-way through a deposit. Losing
// observability must never be able to lose a user's money, and this is the
// test that keeps that true as recording paths are added.
func TestNilMetricsIsSafeOnEveryPath(t *testing.T) {
	var m *Metrics

	m.RecordFlowAttempt(FlowDeposit, OutcomeSucceeded, time.Second)
	m.RecordFlowAttempt(FlowWithdrawal, OutcomeFailedChain, 0)
	m.SetIndexerLag(42)
	m.SetIndexerLagSampleAge(time.Minute)
	m.RecordIndexerLagSampleError()
	m.RecordReconcileRun(ReconcileCompleted)
	m.RecordReconcileRun(ReconcileFailed)
	m.RecordReconcileDivergence(DivergenceMismatch)
	m.SetReconcileLastRunAge(time.Minute)
	m.SetPendingSubmissions(3, time.Hour)
	m.SetPendingSubmissions(0, 0)
}

func TestFlowAttemptRecordsCounterAndHistogram(t *testing.T) {
	m := New()

	m.RecordFlowAttempt(FlowDeposit, OutcomeSucceeded, 6*time.Second)

	labels := map[string]string{"flow": "deposit", "outcome": "succeeded"}

	if got := counterValue(t, m.Registry(), "nester_flow_attempts_total", labels); got != 1 {
		t.Fatalf("attempts counter = %v, want 1", got)
	}
	if got := histogramCount(t, m.Registry(), "nester_flow_duration_seconds", labels); got != 1 {
		t.Fatalf("duration observations = %d, want 1", got)
	}
}

// A rejected attempt is counted but never timed.
//
// It terminates in microseconds without touching the chain, so including it in
// the latency histogram would drag every percentile toward zero and let the
// latency SLI report health during a chain stall — the SLI would look best
// exactly when the service was refusing everything.
func TestRejectedAttemptsAreCountedButNotTimed(t *testing.T) {
	m := New()

	m.RecordFlowAttempt(FlowDeposit, OutcomeRejected, 5*time.Second)

	labels := map[string]string{"flow": "deposit", "outcome": "rejected"}

	if got := counterValue(t, m.Registry(), "nester_flow_attempts_total", labels); got != 1 {
		t.Fatalf("rejected attempt was not counted: got %v, want 1", got)
	}
	if got := histogramCount(t, m.Registry(), "nester_flow_duration_seconds", labels); got != 0 {
		t.Fatalf("rejected attempt was timed: got %d observations, want 0", got)
	}
}

// A zero or negative duration is never observed. A caller that could not
// determine a start time must not be able to plant a 0s sample that makes the
// latency SLI look perfect.
func TestZeroDurationIsNotObserved(t *testing.T) {
	m := New()

	m.RecordFlowAttempt(FlowWithdrawal, OutcomeSucceeded, 0)

	labels := map[string]string{"flow": "withdrawal", "outcome": "succeeded"}

	if got := counterValue(t, m.Registry(), "nester_flow_attempts_total", labels); got != 1 {
		t.Fatalf("attempt was not counted: got %v, want 1", got)
	}
	if got := histogramCount(t, m.Registry(), "nester_flow_duration_seconds", labels); got != 0 {
		t.Fatalf("zero duration was observed: got %d, want 0", got)
	}
}

// Failures are timed as well as counted: how long a failure took to surface is
// operationally different from how long a success took, and the runbook reads
// both.
func TestFailedAttemptsAreTimed(t *testing.T) {
	m := New()

	m.RecordFlowAttempt(FlowDeposit, OutcomeFailedChain, 45*time.Second)

	labels := map[string]string{"flow": "deposit", "outcome": "failed_chain"}

	if got := histogramCount(t, m.Registry(), "nester_flow_duration_seconds", labels); got != 1 {
		t.Fatalf("failed attempt was not timed: got %d, want 1", got)
	}
}

// The label set is fixed at compile time. This asserts the series count cannot
// be moved by traffic or by a caller: five outcomes across two flows is the
// entire space, and it is reached only by outcomes the code actually emits.
func TestFlowLabelCardinalityIsBounded(t *testing.T) {
	m := New()

	flows := []Flow{FlowDeposit, FlowWithdrawal}
	outcomes := []FlowOutcome{
		OutcomeSucceeded,
		OutcomeRejected,
		OutcomeCancelled,
		OutcomeFailedChain,
		OutcomeFailedInternal,
	}

	for _, flow := range flows {
		for _, outcome := range outcomes {
			m.RecordFlowAttempt(flow, outcome, time.Second)
		}
	}

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, family := range families {
		if family.GetName() != "nester_flow_attempts_total" {
			continue
		}
		if got := len(family.GetMetric()); got != len(flows)*len(outcomes) {
			t.Fatalf("attempts series = %d, want %d", got, len(flows)*len(outcomes))
		}
		return
	}

	t.Fatal("nester_flow_attempts_total not found in registry")
}

func TestIndexerLagGauges(t *testing.T) {
	m := New()

	m.SetIndexerLag(37)
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_ledgers"); got != 37 {
		t.Fatalf("lag gauge = %v, want 37", got)
	}

	// A successful sample resets the staleness gauge: the reading is current.
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_last_sample_age_seconds"); got != 0 {
		t.Fatalf("sample age after a successful sample = %v, want 0", got)
	}

	m.SetIndexerLagSampleAge(90 * time.Second)
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_last_sample_age_seconds"); got != 90 {
		t.Fatalf("sample age = %v, want 90", got)
	}
}

// The staleness gauge is what distinguishes "lag is genuinely low" from "the
// sampler died and the value is frozen at a healthy number". Without it the
// balance-freshness SLI fails in its most dangerous direction: reporting
// perfect health. This asserts the lag value and its age are independent, so
// an ageing sample cannot be masked by a stale-but-low lag reading.
func TestStalenessIsIndependentOfLagValue(t *testing.T) {
	m := New()

	m.SetIndexerLag(3)
	m.SetIndexerLagSampleAge(600 * time.Second)

	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_ledgers"); got != 3 {
		t.Fatalf("lag gauge = %v, want 3 (a healthy-looking value)", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_indexer_lag_last_sample_age_seconds"); got != 600 {
		t.Fatalf("sample age = %v, want 600 (stale despite the healthy lag)", got)
	}
}

func TestIndexerLagSampleErrorsAreCounted(t *testing.T) {
	m := New()

	m.RecordIndexerLagSampleError()
	m.RecordIndexerLagSampleError()

	if got := counterValue(t, m.Registry(), "nester_indexer_lag_sample_errors_total", nil); got != 2 {
		t.Fatalf("sample errors = %v, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// Reconciliation and pending submissions (nester#1108)
// ---------------------------------------------------------------------------

// A pass that could not list its work must be counted separately from one that
// ran and found nothing. Collapsing them would let a permanently broken
// reconciler report a clean divergence count indefinitely, which is the one
// reading an operator must never get wrong.
func TestReconcileRunOutcomesAreCountedSeparately(t *testing.T) {
	m := New()

	m.RecordReconcileRun(ReconcileCompleted)
	m.RecordReconcileRun(ReconcileCompleted)
	m.RecordReconcileRun(ReconcileFailed)

	completed := counterValue(t, m.Registry(), "nester_reconcile_runs_total", map[string]string{"outcome": "completed"})
	if completed != 2 {
		t.Fatalf("completed runs = %v, want 2", completed)
	}
	failed := counterValue(t, m.Registry(), "nester_reconcile_runs_total", map[string]string{"outcome": "failed"})
	if failed != 1 {
		t.Fatalf("failed runs = %v, want 1", failed)
	}
}

// Divergences are counted per finding and per kind, so an alert can treat a
// mismatch (a wrong balance) differently from a stuck submission.
func TestReconcileDivergencesAreCountedByKind(t *testing.T) {
	m := New()

	m.RecordReconcileDivergence(DivergenceMismatch)
	m.RecordReconcileDivergence(DivergenceStuck)
	m.RecordReconcileDivergence(DivergenceStuck)

	if got := counterValue(t, m.Registry(), "nester_reconcile_divergences_total", map[string]string{"kind": "mismatch"}); got != 1 {
		t.Fatalf("mismatch divergences = %v, want 1", got)
	}
	if got := counterValue(t, m.Registry(), "nester_reconcile_divergences_total", map[string]string{"kind": "stuck"}); got != 2 {
		t.Fatalf("stuck divergences = %v, want 2", got)
	}
}

// A completed pass resets the age gauge, so the "reconciler is alive" signal
// cannot be satisfied by a pass that never finished.
func TestReconcileRunResetsLastRunAge(t *testing.T) {
	m := New()

	m.SetReconcileLastRunAge(900 * time.Second)
	if got := gaugeValue(t, m.Registry(), "nester_reconcile_last_run_age_seconds"); got != 900 {
		t.Fatalf("pre-run age = %v, want 900", got)
	}

	m.RecordReconcileRun(ReconcileCompleted)

	if got := gaugeValue(t, m.Registry(), "nester_reconcile_last_run_age_seconds"); got != 0 {
		t.Fatalf("post-run age = %v, want 0", got)
	}
}

// Depth and oldest-age are published together because depth alone is
// ambiguous: a large queue that drains is healthy, a small one that never
// drains is not.
func TestPendingSubmissionsPublishesDepthAndOldestAge(t *testing.T) {
	m := New()

	m.SetPendingSubmissions(4, 90*time.Second)

	if got := gaugeValue(t, m.Registry(), "nester_submission_pending_count"); got != 4 {
		t.Fatalf("pending count = %v, want 4", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_submission_pending_oldest_age_seconds"); got != 90 {
		t.Fatalf("oldest age = %v, want 90", got)
	}
}

// An empty queue must report zero age, not the last non-empty reading. A
// carried-over age would keep the pending-submission alert firing after the
// backlog had actually drained.
func TestEmptyPendingQueueZeroesOldestAge(t *testing.T) {
	m := New()

	m.SetPendingSubmissions(4, 90*time.Second)
	m.SetPendingSubmissions(0, 0)

	if got := gaugeValue(t, m.Registry(), "nester_submission_pending_count"); got != 0 {
		t.Fatalf("pending count = %v, want 0", got)
	}
	if got := gaugeValue(t, m.Registry(), "nester_submission_pending_oldest_age_seconds"); got != 0 {
		t.Fatalf("oldest age = %v, want 0 for a drained queue", got)
	}
}
