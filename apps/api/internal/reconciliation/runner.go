package reconciliation

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// Runner drives the reconciliation Engine on a schedule (nester#1082). The
// engine, comparators, and audit tables all predate it (nester#887) but had
// no production caller: nothing constructed them, so the database could
// silently diverge from the chain and nothing would notice. Construct with
// NewRunner, then call Run from a goroutine; Run returns when the context is
// cancelled.
type Runner struct {
	cfg     RunnerConfig
	engine  *Engine
	logger  *slog.Logger
	metrics RunnerMetricsRecorder
	leader  LeaderChecker
	now     func() time.Time

	// startedAt and lastPassEnd feed the liveness metric (AgeSample). Unix
	// nanos, zero until set — matching the transaction poller's lastTickEnd.
	// lastPassEnd advances only on SUCCESSFUL passes: a reconciler failing
	// every pass must read as stalled (age climbing), not alive — one
	// failed-run increment per interval is too sparse for the rate-window
	// ReconciliationFailing alert to hold at balance cadence, so the age
	// series is what pages when nothing is actually being compared.
	startedAt   atomic.Int64
	lastPassEnd atomic.Int64
	// wasLeader tracks the last observed leadership state so the liveness
	// anchor can be reset when leadership is GAINED. Without that, a
	// follower promoted after days of idling would emit age = now - boot
	// until its next pass — a multi-day spike that falsely pages
	// BalanceReconciliationStalled on every failover at intervals past ~10m.
	wasLeader atomic.Bool
}

// RunnerConfig is populated from RECONCILE_* environment configuration.
type RunnerConfig struct {
	// Enabled gates the loop; when false Run returns immediately so main.go
	// can call it unconditionally (transaction-poller convention).
	Enabled bool
	// Interval between passes. Defaults to the balance cadence in
	// DefaultCadenceConfig when non-positive.
	Interval time.Duration
	// DryRun performs the full read-and-compare pass but writes nothing:
	// findings go to the log instead of the audit tables, and no divergence
	// metric is emitted (a dry run must never page an operator). Liveness and
	// run-outcome metrics are still recorded — a dry-running reconciler is
	// alive, and its death must not read as clean silence.
	DryRun bool
}

// LeaderChecker gates passes to the elected leader. Declared locally rather
// than importing the scheduler package, following SubmissionReconciler's
// convention; a nil checker means "always leader" (single instance, tests).
type LeaderChecker interface {
	IsLeader() bool
}

// RunnerMetricsRecorder is the slice of metrics recording this runner needs.
// Declared here as an interface, matching the transaction poller's approach,
// so this package does not depend on Prometheus and tests can record calls
// without a registry.
type RunnerMetricsRecorder interface {
	RecordBalanceReconcileRun(outcome metrics.ReconcileOutcome)
	RecordReconcileDivergence(kind metrics.DivergenceKind)
}

type noopRunnerMetrics struct{}

func (noopRunnerMetrics) RecordBalanceReconcileRun(metrics.ReconcileOutcome) {}
func (noopRunnerMetrics) RecordReconcileDivergence(metrics.DivergenceKind)   {}

// NewRunner builds a scheduled reconciliation runner over the given audit
// repository, comparators, and alerter.
//
// In dry-run mode the repository is replaced with a log-only decorator and no
// divergence metrics are emitted; otherwise the repository is wrapped so every
// successfully recorded finding also increments the divergence counter — the
// metric and the audit row cannot disagree about what was found.
func NewRunner(cfg RunnerConfig, repo Repository, comparators []Comparator, alerter Alerter, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultCadenceConfig().Balance
	}

	r := &Runner{
		cfg:     cfg,
		logger:  logger,
		metrics: noopRunnerMetrics{},
		now:     func() time.Time { return time.Now().UTC() },
	}

	engineRepo := repo
	if cfg.DryRun {
		engineRepo = &dryRunRepository{logger: logger}
		// The dry-run repository already logs every finding with the fields
		// an alert would carry; dispatching to the alerter as well would log
		// each finding twice, and a rehearsal must not alert.
		alerter = nil
	} else {
		engineRepo = &meteredRepository{inner: repo, runner: r}
	}
	r.engine = NewEngine(engineRepo, comparators, alerter).WithLogger(logger)

	return r
}

// SetLeaderChecker installs the leadership gate. Call before Run.
func (r *Runner) SetLeaderChecker(leader LeaderChecker) {
	r.leader = leader
}

// SetMetrics wires metrics recording. Call before Run. A nil recorder leaves
// the runner unmetered rather than panicking.
func (r *Runner) SetMetrics(rec RunnerMetricsRecorder) {
	if rec == nil {
		return
	}
	r.metrics = rec
}

// SetClock overrides the time source for tests.
func (r *Runner) SetClock(now func() time.Time) {
	r.now = now
	r.engine.SetClock(now)
}

// Run drives the loop until ctx is cancelled. When Config.Enabled is false,
// Run returns immediately so main.go can call it unconditionally.
func (r *Runner) Run(ctx context.Context) {
	if !r.cfg.Enabled {
		r.logger.Info("balance reconciliation disabled; not starting")
		return
	}
	r.startedAt.Store(r.now().UnixNano())
	r.logger.Info("balance reconciliation starting",
		"interval", r.cfg.Interval,
		"dry_run", r.cfg.DryRun,
	)

	// Run once immediately so a restart re-establishes the integrity signal
	// without waiting a full interval.
	r.Tick(ctx)

	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("balance reconciliation stopping")
			return
		case <-ticker.C:
			r.Tick(ctx)
		}
	}
}

// Tick runs a single reconciliation pass. Exported for tests.
//
// A non-leader replica skips the pass without touching lastPassEnd: its
// liveness series is suppressed entirely (see AgeSample), so only the replica
// actually doing the work reports an age.
//
// Leadership is checked once per pass, matching the submission reconciler.
// The narrow overlap on a handover mid-pass can at worst duplicate audit
// rows and finding counts for one pass — reconciliation moves no money, so
// the leadership doc's re-check-before-money-moving rule does not bite here.
func (r *Runner) Tick(ctx context.Context) {
	if !r.observeLeadership() {
		return
	}

	stats, err := r.engine.Run(ctx, Scope{FullSweep: true, StartedAt: r.now()})
	if err != nil {
		// A pass aborted by shutdown is not a failing reconciler: recording
		// it as failed would tick the ReconciliationFailing series on every
		// deploy. The engine has already recorded the aborted run row.
		if ctx.Err() != nil {
			r.logger.Info("balance reconciliation pass cancelled by shutdown")
			return
		}
		// Recorded as a failed run rather than left silent: a pass that
		// aborted found no divergences because it stopped checking, which
		// must not read as a clean result (nester#1108 doctrine). The
		// liveness anchor deliberately does NOT advance — persistent
		// failure reads as a climbing age and pages via
		// BalanceReconciliationStalled.
		r.logger.Error("balance reconciliation pass failed", "error", err)
		r.metrics.RecordBalanceReconcileRun(metrics.ReconcileFailed)
		return
	}

	r.lastPassEnd.Store(r.now().UnixNano())
	r.metrics.RecordBalanceReconcileRun(metrics.ReconcileCompleted)
	r.logger.Info("balance reconciliation pass completed",
		"checked", stats.Checked,
		"findings", stats.Findings,
		"critical", stats.Critical,
		"dry_run", r.cfg.DryRun,
	)
}

// observeLeadership reports whether this replica currently leads, resetting
// the liveness anchor on the moment leadership is GAINED. Both Tick and
// AgeSample observe through here, so a freshly promoted follower re-anchors
// on its first scrape or tick — whichever comes first — and never emits the
// stale boot-time age it accumulated while following. The atomics make the
// worst concurrent interleaving a harmless double re-anchor.
func (r *Runner) observeLeadership() bool {
	if r.leader == nil {
		return true
	}
	isLeader := r.leader.IsLeader()
	if isLeader && !r.wasLeader.Swap(true) {
		// Anchor only a runner that is actually running: a disabled runner
		// must stay absent from the liveness series, not emit a fresh age
		// because a scrape observed it holding leadership.
		if r.startedAt.Load() != 0 {
			r.lastPassEnd.Store(r.now().UnixNano())
		}
	}
	if !isLeader {
		r.wasLeader.Store(false)
	}
	return isLeader
}

// AgeSample reports how long ago this runner last completed a successful
// pass, for the scrape-time liveness collector
// (metrics.RegisterBalanceReconcileAge).
//
// The boolean gates emission:
//   - a runner that never started (disabled) emits nothing, so the series is
//     absent and the absent() alert — not a frozen healthy value — covers it;
//   - a non-leader replica emits nothing, so a healthy follower's climbing
//     idle time cannot page while the leader is reconciling on schedule. If
//     no replica holds leadership the series disappears cluster-wide, which
//     the absent() alert reports — leaderless is exactly as dark as stopped.
//
// The anchor is reset when leadership is gained (see observeLeadership) and
// otherwise advances only on successful passes, so both a hanging first pass
// and a permanently failing loop read as a climbing age.
func (r *Runner) AgeSample() (time.Duration, bool) {
	if !r.observeLeadership() {
		return 0, false
	}
	anchor := r.lastPassEnd.Load()
	if anchor == 0 {
		anchor = r.startedAt.Load()
	}
	if anchor == 0 {
		return 0, false
	}
	return r.now().Sub(time.Unix(0, anchor)), true
}

// LastPassEnd returns the wall-clock time the last successful pass finished
// (or leadership was gained), or zero if neither has happened yet.
func (r *Runner) LastPassEnd() time.Time {
	v := r.lastPassEnd.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// ── repository decorators ────────────────────────────────────────────────────

// meteredRepository counts every successfully recorded finding on the
// divergence metric. Wrapping the repository rather than walking engine stats
// means the counter and the audit table are fed by the same event: a finding
// that failed to persist is not counted, and one that persisted cannot be
// missed.
type meteredRepository struct {
	inner  Repository
	runner *Runner
}

func (m *meteredRepository) CreateRun(ctx context.Context, run Run) (Run, error) {
	return m.inner.CreateRun(ctx, run)
}

func (m *meteredRepository) AddFinding(ctx context.Context, finding Finding) (Finding, error) {
	stored, err := m.inner.AddFinding(ctx, finding)
	if err != nil {
		return stored, err
	}
	// DiscrepancyType and metrics.DivergenceKind are deliberately the same
	// closed vocabulary (see metrics/slo.go), so the conversion is a cast.
	m.runner.metrics.RecordReconcileDivergence(metrics.DivergenceKind(stored.Type))
	return stored, nil
}

func (m *meteredRepository) CompleteRun(ctx context.Context, runID uuid.UUID, stats Stats) error {
	return m.inner.CompleteRun(ctx, runID, stats)
}

func (m *meteredRepository) FailRun(ctx context.Context, runID uuid.UUID, errText string) error {
	return m.inner.FailRun(ctx, runID, errText)
}

func (m *meteredRepository) GetCheckpoint(ctx context.Context, key string) (string, bool, error) {
	return m.inner.GetCheckpoint(ctx, key)
}

func (m *meteredRepository) SetCheckpoint(ctx context.Context, key, value string) error {
	return m.inner.SetCheckpoint(ctx, key, value)
}

func (m *meteredRepository) RecordCorrection(ctx context.Context, findingID uuid.UUID, reason string) error {
	return m.inner.RecordCorrection(ctx, findingID, reason)
}

// dryRunRepository satisfies Repository without writing anything. Findings
// are logged at Warn with the same fields the audit row would carry, so an
// operator can rehearse a reconciliation pass against production data and
// read exactly what a live run would have recorded — with no rows written, no
// metrics emitted, and no page fired.
type dryRunRepository struct {
	logger *slog.Logger
}

func (d *dryRunRepository) CreateRun(ctx context.Context, run Run) (Run, error) {
	d.logger.InfoContext(ctx, "dry-run: reconciliation run started (not recorded)",
		"run_id", run.ID.String(),
		"level", string(run.Level),
		"comparator", run.Comparator,
	)
	return run, nil
}

func (d *dryRunRepository) AddFinding(ctx context.Context, finding Finding) (Finding, error) {
	attrs := []any{
		"run_id", finding.RunID.String(),
		"level", string(finding.Level),
		"type", string(finding.Type),
		"severity", string(finding.Severity),
		"entity_type", finding.EntityType,
		"entity_id", finding.EntityID,
	}
	if finding.RecordedValue != nil {
		attrs = append(attrs, "recorded_value", finding.RecordedValue.String())
	}
	if finding.OnChainValue != nil {
		attrs = append(attrs, "on_chain_value", finding.OnChainValue.String())
	}
	if finding.Difference != nil {
		attrs = append(attrs, "difference", finding.Difference.String())
	}
	d.logger.WarnContext(ctx, "dry-run: divergence found (not recorded)", attrs...)
	return finding, nil
}

func (d *dryRunRepository) CompleteRun(ctx context.Context, runID uuid.UUID, stats Stats) error {
	d.logger.InfoContext(ctx, "dry-run: reconciliation run completed (not recorded)",
		"run_id", runID.String(),
		"checked", stats.Checked,
		"findings", stats.Findings,
		"critical", stats.Critical,
	)
	return nil
}

func (d *dryRunRepository) FailRun(ctx context.Context, runID uuid.UUID, errText string) error {
	d.logger.WarnContext(ctx, "dry-run: reconciliation run failed (not recorded)",
		"run_id", runID.String(),
		"error", errText,
	)
	return nil
}

func (d *dryRunRepository) GetCheckpoint(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (d *dryRunRepository) SetCheckpoint(context.Context, string, string) error { return nil }

func (d *dryRunRepository) RecordCorrection(context.Context, uuid.UUID, string) error { return nil }
