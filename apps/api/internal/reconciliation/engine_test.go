package reconciliation

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type fakeComparator struct {
	result ComparisonResult
	err    error
}

func (c fakeComparator) Name() string { return "fake_balance" }
func (c fakeComparator) Level() Level { return LevelBalance }
func (c fakeComparator) Reconcile(ctx context.Context, scope Scope) (ComparisonResult, error) {
	if c.err != nil {
		return ComparisonResult{}, c.err
	}
	return c.result, nil
}

type fakeRepo struct {
	runs        []Run
	findings    []Finding
	corrections int
	completed   Stats
	// failRunErr, when set, makes FailRun itself fail — simulating the
	// "error path of the error path" nester#1194 is about.
	failRunErr error
}

func (r *fakeRepo) CreateRun(ctx context.Context, run Run) (Run, error) {
	r.runs = append(r.runs, run)
	return run, nil
}

func (r *fakeRepo) AddFinding(ctx context.Context, finding Finding) (Finding, error) {
	r.findings = append(r.findings, finding)
	return finding, nil
}

func (r *fakeRepo) CompleteRun(ctx context.Context, runID uuid.UUID, stats Stats) error {
	r.completed = stats
	return nil
}

func (r *fakeRepo) FailRun(ctx context.Context, runID uuid.UUID, errText string) error {
	return r.failRunErr
}

func (r *fakeRepo) GetCheckpoint(ctx context.Context, key string) (string, bool, error) {
	return "", false, nil
}

func (r *fakeRepo) SetCheckpoint(ctx context.Context, key, value string) error {
	return nil
}

func (r *fakeRepo) RecordCorrection(ctx context.Context, findingID uuid.UUID, reason string) error {
	r.corrections++
	return nil
}

type fakeAlerter struct {
	critical int
	warning  int
}

func (a *fakeAlerter) CriticalFinding(ctx context.Context, finding Finding) error {
	a.critical++
	return nil
}

func (a *fakeAlerter) WarningFinding(ctx context.Context, finding Finding) error {
	a.warning++
	return nil
}

func TestEngineRecordsFindingAndDoesNotAutoCorrect(t *testing.T) {
	recorded := decimal.RequireFromString("250")
	onChain := decimal.RequireFromString("100")
	finding := NewClassifier(Classifier{
		CriticalThreshold: decimal.RequireFromString("10"),
	}).Classify(FindingInput{
		Level:         LevelBalance,
		Type:          TypeMismatch,
		EntityType:    "vault",
		EntityID:      "vault-1",
		RecordedValue: &recorded,
		OnChainValue:  &onChain,
	}, time.Now())

	repo := &fakeRepo{}
	alerter := &fakeAlerter{}
	engine := NewEngine(repo, []Comparator{fakeComparator{
		result: ComparisonResult{Checked: 1, Findings: []Finding{finding}},
	}}, alerter)

	stats, err := engine.Run(context.Background(), Scope{FullSweep: true})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stats.Checked != 1 || stats.Findings != 1 || stats.Critical != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if len(repo.findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(repo.findings))
	}
	if repo.corrections != 0 {
		t.Fatalf("corrections = %d, want 0", repo.corrections)
	}
	if alerter.critical != 1 {
		t.Fatalf("critical alerts = %d, want 1", alerter.critical)
	}
}

// captureHandler is a minimal slog.Handler that records emitted records as
// plain strings, so a test can assert on the logged fields without pulling
// in a full logging test-helper library.
type captureHandler struct {
	records *[]string
}

func (h captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h captureHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	*h.records = append(*h.records, sb.String())
	return nil
}
func (h captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h captureHandler) WithGroup(string) slog.Handler      { return h }

func TestEngineLogsOriginalErrorWhenFailRunItselfFails(t *testing.T) {
	reconcileErr := errors.New("upstream comparator exploded")
	failRunErr := errors.New("db connection reset")

	repo := &fakeRepo{failRunErr: failRunErr}
	alerter := &fakeAlerter{}
	var logs []string
	logger := slog.New(captureHandler{records: &logs})

	engine := NewEngine(repo, []Comparator{fakeComparator{err: reconcileErr}}, alerter).
		WithLogger(logger)

	_, err := engine.Run(context.Background(), Scope{FullSweep: true})
	if err == nil {
		t.Fatal("Run() error = nil, want the original reconcile error")
	}
	if !errors.Is(err, reconcileErr) {
		t.Fatalf("Run() error = %v, want it to wrap %v", err, reconcileErr)
	}

	if len(logs) != 1 {
		t.Fatalf("expected exactly one log record, got %d: %v", len(logs), logs)
	}
	entry := logs[0]
	if !strings.Contains(entry, reconcileErr.Error()) {
		t.Fatalf("log entry missing the original error: %q", entry)
	}
	if !strings.Contains(entry, failRunErr.Error()) {
		t.Fatalf("log entry missing the FailRun error: %q", entry)
	}
}

func TestEngineDoesNotLogWhenFailRunSucceeds(t *testing.T) {
	reconcileErr := errors.New("upstream comparator exploded")

	repo := &fakeRepo{} // FailRun succeeds (failRunErr is nil)
	alerter := &fakeAlerter{}
	var logs []string
	logger := slog.New(captureHandler{records: &logs})

	engine := NewEngine(repo, []Comparator{fakeComparator{err: reconcileErr}}, alerter).
		WithLogger(logger)

	_, err := engine.Run(context.Background(), Scope{FullSweep: true})
	if err == nil {
		t.Fatal("Run() error = nil, want the original reconcile error")
	}
	if len(logs) != 0 {
		t.Fatalf("expected no log records when FailRun succeeds, got %d: %v", len(logs), logs)
	}
}
