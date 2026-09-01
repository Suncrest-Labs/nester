package reconciliation

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

type fakeRunnerMetrics struct {
	runs        []metrics.ReconcileOutcome
	divergences []metrics.DivergenceKind
}

func (f *fakeRunnerMetrics) RecordBalanceReconcileRun(outcome metrics.ReconcileOutcome) {
	f.runs = append(f.runs, outcome)
}

func (f *fakeRunnerMetrics) RecordReconcileDivergence(kind metrics.DivergenceKind) {
	f.divergences = append(f.divergences, kind)
}

type fakeLeader struct{ leader bool }

func (f fakeLeader) IsLeader() bool { return f.leader }

// mismatchFinding builds a classified vault-balance mismatch for tests.
func mismatchFinding(t *testing.T, recorded, onChain string) Finding {
	t.Helper()
	rec := decimal.RequireFromString(recorded)
	chain := decimal.RequireFromString(onChain)
	return NewClassifier(Classifier{}).Classify(FindingInput{
		Level:         LevelBalance,
		Type:          TypeMismatch,
		EntityType:    "vault",
		EntityID:      "vault-1",
		RecordedValue: &rec,
		OnChainValue:  &chain,
	}, time.Now())
}

func TestRunnerDisabledDoesNotStart(t *testing.T) {
	repo := &fakeRepo{}
	runner := NewRunner(RunnerConfig{Enabled: false}, repo, []Comparator{fakeComparator{
		result: ComparisonResult{Checked: 1},
	}}, nil, slog.New(slog.DiscardHandler))

	// Run must return immediately rather than block on the ticker.
	runner.Run(context.Background())

	if len(repo.runs) != 0 {
		t.Fatalf("disabled runner created %d runs, want 0", len(repo.runs))
	}
	if _, emit := runner.AgeSample(); emit {
		t.Fatal("disabled runner must not emit a liveness sample")
	}
}

func TestRunnerTickRecordsFindingsAndMetrics(t *testing.T) {
	finding := mismatchFinding(t, "250", "100")
	repo := &fakeRepo{}
	rec := &fakeRunnerMetrics{}

	runner := NewRunner(RunnerConfig{Enabled: true}, repo, []Comparator{fakeComparator{
		result: ComparisonResult{Checked: 3, Findings: []Finding{finding}},
	}}, nil, slog.New(slog.DiscardHandler))
	runner.SetMetrics(rec)

	runner.Tick(context.Background())

	if len(repo.findings) != 1 {
		t.Fatalf("findings persisted = %d, want 1", len(repo.findings))
	}
	if len(rec.runs) != 1 || rec.runs[0] != metrics.ReconcileCompleted {
		t.Fatalf("run outcomes = %v, want [completed]", rec.runs)
	}
	if len(rec.divergences) != 1 || rec.divergences[0] != metrics.DivergenceMismatch {
		t.Fatalf("divergence kinds = %v, want [mismatch]", rec.divergences)
	}
	age, emit := runner.AgeSample()
	if !emit {
		t.Fatal("expected a liveness sample after a completed pass")
	}
	if age < 0 || age > time.Minute {
		t.Fatalf("age = %v, want a small positive duration", age)
	}
}

func TestRunnerTickComparatorErrorRecordsFailedRun(t *testing.T) {
	repo := &fakeRepo{}
	rec := &fakeRunnerMetrics{}

	runner := NewRunner(RunnerConfig{Enabled: true}, repo, []Comparator{fakeComparator{
		err: errors.New("chain read failed"),
	}}, nil, slog.New(slog.DiscardHandler))
	runner.SetMetrics(rec)

	runner.Tick(context.Background())

	if len(rec.runs) != 1 || rec.runs[0] != metrics.ReconcileFailed {
		t.Fatalf("run outcomes = %v, want [failed]", rec.runs)
	}
	if len(rec.divergences) != 0 {
		t.Fatalf("divergences = %v, want none", rec.divergences)
	}
	// The liveness anchor advances only on SUCCESS: a reconciler failing
	// every pass must read as a climbing age (and page via
	// BalanceReconciliationStalled), because one failed-run increment per
	// interval is too sparse for the rate-window ReconciliationFailing
	// alert to hold at balance cadence. With no prior success and Run never
	// started, there is no anchor at all.
	if _, emit := runner.AgeSample(); emit {
		t.Fatal("a failed pass must not advance the liveness anchor")
	}
}

func TestRunnerSkipsWhenNotLeader(t *testing.T) {
	repo := &fakeRepo{}
	rec := &fakeRunnerMetrics{}

	runner := NewRunner(RunnerConfig{Enabled: true}, repo, []Comparator{fakeComparator{
		result: ComparisonResult{Checked: 1},
	}}, nil, slog.New(slog.DiscardHandler))
	runner.SetMetrics(rec)
	runner.SetLeaderChecker(fakeLeader{leader: false})

	runner.Tick(context.Background())

	if len(repo.runs) != 0 {
		t.Fatalf("non-leader created %d runs, want 0", len(repo.runs))
	}
	if len(rec.runs) != 0 {
		t.Fatalf("non-leader recorded %d run outcomes, want 0", len(rec.runs))
	}
	// A follower must not emit a liveness age: its climbing idle time would
	// page while the leader is reconciling on schedule.
	if _, emit := runner.AgeSample(); emit {
		t.Fatal("non-leader must not emit a liveness sample")
	}
}

func TestRunnerDryRunWritesAndAlertsNothing(t *testing.T) {
	finding := mismatchFinding(t, "250", "100")
	repo := &fakeRepo{}
	rec := &fakeRunnerMetrics{}
	alerter := &fakeAlerter{}
	var logs []string
	logger := slog.New(captureHandler{records: &logs})

	runner := NewRunner(RunnerConfig{Enabled: true, DryRun: true}, repo, []Comparator{fakeComparator{
		result: ComparisonResult{Checked: 1, Findings: []Finding{finding}},
	}}, alerter, logger)
	runner.SetMetrics(rec)

	runner.Tick(context.Background())

	if len(repo.runs) != 0 || len(repo.findings) != 0 {
		t.Fatalf("dry run wrote runs=%d findings=%d, want 0/0", len(repo.runs), len(repo.findings))
	}
	if alerter.critical != 0 || alerter.warning != 0 {
		t.Fatalf("dry run dispatched alerts: critical=%d warning=%d", alerter.critical, alerter.warning)
	}
	if len(rec.divergences) != 0 {
		t.Fatalf("dry run emitted divergence metrics: %v", rec.divergences)
	}
	// The pass itself still counts — a dry-running reconciler is alive and
	// its death must not read as clean silence.
	if len(rec.runs) != 1 || rec.runs[0] != metrics.ReconcileCompleted {
		t.Fatalf("run outcomes = %v, want [completed]", rec.runs)
	}
	if _, emit := runner.AgeSample(); !emit {
		t.Fatal("expected a liveness sample from a dry run")
	}

	var foundDivergenceLog bool
	for _, entry := range logs {
		if strings.Contains(entry, "dry-run: divergence found") &&
			strings.Contains(entry, "recorded_value=250") &&
			strings.Contains(entry, "on_chain_value=100") {
			foundDivergenceLog = true
		}
	}
	if !foundDivergenceLog {
		t.Fatalf("dry run did not log the divergence with both values; logs: %v", logs)
	}
}

// mutableLeader lets a test flip leadership mid-flight.
type mutableLeader struct{ leader bool }

func (m *mutableLeader) IsLeader() bool { return m.leader }

// A follower promoted to leader must not emit the stale age it accumulated
// while following — without re-anchoring, a replica up for two days would
// report a two-day "last pass age" on its first post-promotion scrape and
// falsely page BalanceReconciliationStalled on every failover.
func TestRunnerReanchorsOnLeadershipGain(t *testing.T) {
	current := time.Unix(1_000_000, 0)
	runner := NewRunner(RunnerConfig{Enabled: true}, &fakeRepo{}, nil, nil, slog.New(slog.DiscardHandler))
	runner.SetClock(func() time.Time { return current })

	leader := &mutableLeader{leader: false}
	runner.SetLeaderChecker(leader)

	// Simulate a replica that started Run() two days ago and has been a
	// follower ever since.
	runner.startedAt.Store(current.Add(-48 * time.Hour).UnixNano())

	if _, emit := runner.AgeSample(); emit {
		t.Fatal("a follower must not emit a liveness sample")
	}

	// Promotion: the first observation after gaining leadership re-anchors,
	// so the emitted age is measured from promotion, not from boot.
	leader.leader = true
	age, emit := runner.AgeSample()
	if !emit {
		t.Fatal("expected a liveness sample after gaining leadership")
	}
	if age != 0 {
		t.Fatalf("age at promotion = %v, want 0 (re-anchored), not the 48h since boot", age)
	}

	// Losing and regaining leadership later re-anchors again.
	leader.leader = false
	if _, emit := runner.AgeSample(); emit {
		t.Fatal("a demoted replica must stop emitting")
	}
	current = current.Add(3 * time.Hour)
	leader.leader = true
	age, emit = runner.AgeSample()
	if !emit || age != 0 {
		t.Fatalf("age after regaining leadership = %v emit=%v, want 0/true", age, emit)
	}
}

// A pass aborted by shutdown is not a failing reconciler: recording it as
// failed would tick ReconciliationFailing on every deploy.
func TestRunnerShutdownCancellationIsNotAFailedRun(t *testing.T) {
	rec := &fakeRunnerMetrics{}
	runner := NewRunner(RunnerConfig{Enabled: true}, &fakeRepo{}, []Comparator{fakeComparator{
		err: context.Canceled,
	}}, nil, slog.New(slog.DiscardHandler))
	runner.SetMetrics(rec)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.Tick(ctx)

	if len(rec.runs) != 0 {
		t.Fatalf("run outcomes = %v, want none for a shutdown-cancelled pass", rec.runs)
	}
}

// countingComparator counts passes with an atomic so a test can observe the
// ticker loop from another goroutine without racing the fake repository.
type countingComparator struct{ passes *atomic.Int32 }

func (c countingComparator) Name() string { return "counting" }
func (c countingComparator) Level() Level { return LevelBalance }
func (c countingComparator) Reconcile(context.Context, Scope) (ComparisonResult, error) {
	c.passes.Add(1)
	return ComparisonResult{}, nil
}

// The "on a schedule" half of criterion 1: Run must keep re-running passes on
// the configured interval, not just fire the immediate first tick.
func TestRunnerRunRecursOnInterval(t *testing.T) {
	var passes atomic.Int32
	runner := NewRunner(
		RunnerConfig{Enabled: true, Interval: 5 * time.Millisecond},
		&fakeRepo{},
		[]Comparator{countingComparator{passes: &passes}},
		nil,
		slog.New(slog.DiscardHandler),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	deadline := time.After(10 * time.Second)
	for passes.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("only %d passes within the deadline — the ticker is not recurring", passes.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	<-done
}

func TestRunnerDefaultsIntervalToBalanceCadence(t *testing.T) {
	runner := NewRunner(RunnerConfig{Enabled: true}, &fakeRepo{}, nil, nil, nil)
	if runner.cfg.Interval != DefaultCadenceConfig().Balance {
		t.Fatalf("interval = %v, want %v", runner.cfg.Interval, DefaultCadenceConfig().Balance)
	}
}

func TestRunnerRunStopsOnContextCancel(t *testing.T) {
	repo := &fakeRepo{}
	runner := NewRunner(RunnerConfig{Enabled: true, Interval: time.Hour}, repo, []Comparator{fakeComparator{
		result: ComparisonResult{Checked: 1},
	}}, nil, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	// The immediate first tick happens before the ticker wait; cancel and the
	// loop must exit promptly.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
	if len(repo.runs) != 1 {
		t.Fatalf("expected exactly the immediate first pass, got %d runs", len(repo.runs))
	}
}
