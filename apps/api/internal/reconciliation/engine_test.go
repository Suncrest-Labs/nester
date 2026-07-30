package reconciliation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type fakeComparator struct {
	result ComparisonResult
}

func (c fakeComparator) Name() string { return "fake_balance" }
func (c fakeComparator) Level() Level { return LevelBalance }
func (c fakeComparator) Reconcile(ctx context.Context, scope Scope) (ComparisonResult, error) {
	return c.result, nil
}

type fakeRepo struct {
	runs        []Run
	findings    []Finding
	corrections int
	completed   Stats
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
	return nil
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
