package outbox

import (
	"context"
	"log/slog"
	"time"
)

// Default retention windows. Dispatched events are history and go early;
// dead events are the evidence someone needs to work out what broke, so
// they outlive them by a wide margin.
const (
	DefaultDispatchedRetention = 7 * 24 * time.Hour
	DefaultDeadRetention       = 30 * 24 * time.Hour
)

// LeaderChecker gates the retention sweep to one instance. A nil checker
// means "always leader" (single-instance deployments and tests).
type LeaderChecker interface {
	IsLeader() bool
}

// RetentionConfig parameterizes the pruning job.
type RetentionConfig struct {
	// DispatchedRetention is how long delivered events are kept.
	DispatchedRetention time.Duration
	// DeadRetention is how long poison events are kept.
	DeadRetention time.Duration
}

func (c RetentionConfig) withDefaults() RetentionConfig {
	if c.DispatchedRetention <= 0 {
		c.DispatchedRetention = DefaultDispatchedRetention
	}
	if c.DeadRetention <= 0 {
		c.DeadRetention = DefaultDeadRetention
	}
	return c
}

// Pruner is the subset of Repository the retention job needs.
type Pruner interface {
	PruneTerminal(ctx context.Context, dispatchedBefore, deadBefore time.Time) (dispatched, dead int64, err error)
}

// RetentionJob periodically deletes terminal outbox rows past their
// retention window. Without it the table only ever grows: every side effect
// the system has ever produced, kept forever, on the write path of every
// domain transaction that inserts into it.
//
// Only terminal rows are ever touched. Pending and dispatching rows are
// undelivered work and are never pruned regardless of age — deleting one
// would be silently dropping the side effect the outbox exists to guarantee.
type RetentionJob struct {
	repo    Pruner
	cfg     RetentionConfig
	logger  *slog.Logger
	metrics Metrics
	clock   func() time.Time
	leader  LeaderChecker
}

// NewRetentionJob constructs the retention job. logger and metrics may be nil.
func NewRetentionJob(repo Pruner, cfg RetentionConfig, logger *slog.Logger, metrics Metrics) *RetentionJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if metrics == nil {
		metrics = NoopMetrics{}
	}
	return &RetentionJob{
		repo:    repo,
		cfg:     cfg.withDefaults(),
		logger:  logger,
		metrics: metrics,
		clock:   func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the job's clock (tests only).
func (j *RetentionJob) SetClock(clock func() time.Time) {
	if clock != nil {
		j.clock = clock
	}
}

// SetLeaderChecker wires leader election. Pruning from every instance at
// once is correct but wasteful, so it is gated the same way the other sweep
// jobs are.
func (j *RetentionJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *RetentionJob) isLeader() bool { return j.leader == nil || j.leader.IsLeader() }

// Run ticks every interval until ctx is cancelled, with one tick on start.
func (j *RetentionJob) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	j.Tick(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

// Tick runs a single prune pass and returns how many rows it removed.
// Exported for tests.
func (j *RetentionJob) Tick(ctx context.Context) int64 {
	if j.repo == nil || !j.isLeader() {
		return 0
	}
	now := j.clock()
	dispatched, dead, err := j.repo.PruneTerminal(ctx,
		now.Add(-j.cfg.DispatchedRetention),
		now.Add(-j.cfg.DeadRetention),
	)
	if err != nil {
		if ctx.Err() == nil {
			j.logger.Error("outbox retention: prune failed", "error", err)
		}
		return 0
	}
	if dispatched > 0 {
		j.metrics.IncPruned(string(StatusDispatched), dispatched)
	}
	if dead > 0 {
		j.metrics.IncPruned(string(StatusDead), dead)
	}
	if dispatched > 0 || dead > 0 {
		j.logger.Info("outbox retention: pruned terminal events",
			"dispatched", dispatched,
			"dead", dead,
			"dispatched_retention", j.cfg.DispatchedRetention,
			"dead_retention", j.cfg.DeadRetention,
		)
	}
	return dispatched + dead
}
