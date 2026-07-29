package reconciliation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type Comparator interface {
	Name() string
	Level() Level
	Reconcile(ctx context.Context, scope Scope) (ComparisonResult, error)
}

type ComparisonResult struct {
	Checked  int
	Findings []Finding
}

type Repository interface {
	CreateRun(ctx context.Context, run Run) (Run, error)
	AddFinding(ctx context.Context, finding Finding) (Finding, error)
	CompleteRun(ctx context.Context, runID uuid.UUID, stats Stats) error
	FailRun(ctx context.Context, runID uuid.UUID, errText string) error
	GetCheckpoint(ctx context.Context, key string) (string, bool, error)
	SetCheckpoint(ctx context.Context, key, value string) error
	RecordCorrection(ctx context.Context, findingID uuid.UUID, reason string) error
}

type Alerter interface {
	CriticalFinding(ctx context.Context, finding Finding) error
	WarningFinding(ctx context.Context, finding Finding) error
}

type Engine struct {
	repo        Repository
	comparators []Comparator
	alerter     Alerter
	logger      *slog.Logger
	now         func() time.Time
}

func NewEngine(repo Repository, comparators []Comparator, alerter Alerter) *Engine {
	return &Engine{
		repo:        repo,
		comparators: comparators,
		alerter:     alerter,
		logger:      slog.Default(),
		now:         func() time.Time { return time.Now().UTC() },
	}
}

func (e *Engine) WithLogger(logger *slog.Logger) *Engine {
	e.logger = logger
	return e
}

func (e *Engine) SetClock(now func() time.Time) {
	e.now = now
}

func (e *Engine) Run(ctx context.Context, scope Scope) (Stats, error) {
	if scope.StartedAt.IsZero() {
		scope.StartedAt = e.now()
	}

	var total Stats
	for _, comparator := range e.comparators {
		stats, err := e.runComparator(ctx, comparator, scope)
		total.Checked += stats.Checked
		total.Findings += stats.Findings
		total.Critical += stats.Critical
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func (e *Engine) runComparator(ctx context.Context, comparator Comparator, scope Scope) (Stats, error) {
	run, err := e.repo.CreateRun(ctx, Run{
		ID:         uuid.New(),
		Level:      comparator.Level(),
		Comparator: comparator.Name(),
		Status:     RunStatusRunning,
		Scope:      scope,
		StartedAt:  scope.StartedAt,
	})
	if err != nil {
		return Stats{}, fmt.Errorf("reconciliation: create run: %w", err)
	}

	result, err := comparator.Reconcile(ctx, scope)
	if err != nil {
		_ = e.repo.FailRun(ctx, run.ID, err.Error())
		return Stats{}, fmt.Errorf("reconciliation: %s: %w", comparator.Name(), err)
	}

	stats := Stats{Checked: result.Checked, Findings: len(result.Findings)}
	for _, finding := range result.Findings {
		finding.RunID = run.ID
		finding.Level = comparator.Level()
		if finding.ID == uuid.Nil {
			finding.ID = uuid.New()
		}
		if finding.ResolutionState == "" {
			finding.ResolutionState = ResolutionOpen
		}

		stored, err := e.repo.AddFinding(ctx, finding)
		if err != nil {
			_ = e.repo.FailRun(ctx, run.ID, err.Error())
			return stats, fmt.Errorf("reconciliation: store finding: %w", err)
		}
		if stored.Severity == SeverityCritical {
			stats.Critical++
		}
		if err := e.dispatchAlert(ctx, stored); err != nil {
			e.logger.Error("reconciliation alert failed", "finding_id", stored.ID, "error", err)
		}
	}

	if err := e.repo.CompleteRun(ctx, run.ID, stats); err != nil {
		return stats, fmt.Errorf("reconciliation: complete run: %w", err)
	}
	return stats, nil
}

func (e *Engine) dispatchAlert(ctx context.Context, finding Finding) error {
	if e.alerter == nil {
		return nil
	}
	switch finding.Severity {
	case SeverityCritical:
		return e.alerter.CriticalFinding(ctx, finding)
	case SeverityWarning:
		return e.alerter.WarningFinding(ctx, finding)
	default:
		return nil
	}
}
