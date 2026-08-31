package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/audit"
)

// DataRetentionAuditLogger is the narrow subset of service.AuditLogger this
// job needs — deletion is itself audit-logged (nester#1226), and depending
// only on the domain package (not the service package) avoids
// scheduler -> service appearing in the import graph.
type DataRetentionAuditLogger interface {
	Log(ctx context.Context, entry audit.Entry) error
}

// DataRetentionRepository is the subset of the two repositories this job
// needs. Both DeleteOlderThan methods issue a plain DELETE ... WHERE <ts> <
// cutoff and return the row count removed. Erasure, not anonymisation: both
// tables hold only operational event history with no fields the retention
// policy classifies as requiring anonymised retention (see
// docs/data-retention.md).
type DataRetentionRepository interface {
	// DeleteOlderThan removes activity_events rows older than cutoff.
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// NudgeRetentionRepository is the nudge-history half of the job. A separate
// interface (rather than folding into DataRetentionRepository) because the
// method name legitimately differs per table and Go interfaces don't let two
// embedded methods share a name with different receivers cleanly — this
// keeps each call site's intent explicit at the call rather than behind a
// generic name.
type NudgeRetentionRepository interface {
	// DeleteDispatchesOlderThan removes nudge_dispatch_log rows (and their
	// cascaded nudge_outcomes) older than cutoff.
	DeleteDispatchesOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}

// DataRetentionConfig controls the sweep. See docs/data-retention.md for the
// policy that sets these defaults and the reasoning behind each one.
type DataRetentionConfig struct {
	// ActivityEventsRetention is how long activity_events rows are kept.
	// Default 180 days (docs/data-retention.md §activity_events).
	ActivityEventsRetention time.Duration
	// NudgeDispatchRetention is how long nudge_dispatch_log rows (and their
	// cascaded nudge_outcomes) are kept. Must stay well past
	// postgres.effectivenessWindow (90 days) or the nudge-ranking engine's
	// effectiveness stats silently starve. Default 180 days.
	NudgeDispatchRetention time.Duration
	Now                    func() time.Time
}

func (c DataRetentionConfig) withDefaults() DataRetentionConfig {
	if c.ActivityEventsRetention <= 0 {
		c.ActivityEventsRetention = 180 * 24 * time.Hour
	}
	if c.NudgeDispatchRetention <= 0 {
		c.NudgeDispatchRetention = 180 * 24 * time.Hour
	}
	if c.Now == nil {
		c.Now = func() time.Time { return time.Now().UTC() }
	}
	return c
}

// DataRetentionJob periodically hard-deletes rows past their retention
// window from tables the policy in docs/data-retention.md classifies as
// erasable operational history (nester#1226). Every table this job touches
// carries no legal/audit retention requirement — audit_logs, KYC records,
// and processed_events are explicitly OUT of scope for this job; see the
// policy doc for why each of those is handled (or deliberately not handled)
// differently.
type DataRetentionJob struct {
	activity DataRetentionRepository
	nudges   NudgeRetentionRepository
	auditLog DataRetentionAuditLogger
	logger   *slog.Logger
	cfg      DataRetentionConfig
	leader   LeaderChecker
}

// NewDataRetentionJob constructs the retention job. logger may be nil.
// auditLog may be nil (deletion then simply isn't audit-logged; used only
// when a Postgres connection to write audit_logs isn't available — mirrors
// service.NoopAuditLogger's fallback reasoning).
func NewDataRetentionJob(
	activity DataRetentionRepository,
	nudges NudgeRetentionRepository,
	auditLog DataRetentionAuditLogger,
	cfg DataRetentionConfig,
	logger *slog.Logger,
) *DataRetentionJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &DataRetentionJob{
		activity: activity,
		nudges:   nudges,
		auditLog: auditLog,
		logger:   logger,
		cfg:      cfg.withDefaults(),
	}
}

// SetLeaderChecker wires leader election. Running the sweep from multiple
// instances is harmless-but-wasteful (each DELETE is idempotent — a second
// run just finds nothing left to delete), but it's still classified with the
// other sweep jobs for the same reason SavingsGoalPurgeJob is: a nil checker
// means "always leader" (single-instance deployments, existing tests).
func (j *DataRetentionJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *DataRetentionJob) isLeader() bool {
	return j.leader == nil || j.leader.IsLeader()
}

// Run ticks every `interval` until ctx is cancelled, sweeping expired rows on
// each tick. A tick also fires once immediately on start.
func (j *DataRetentionJob) Run(ctx context.Context, interval time.Duration) {
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

// Tick runs a single retention sweep. Exported for tests. Each table's
// deletion (and its audit log entry) is independent — a failure on one does
// not block the other, since neither depends on the other's cutoff.
func (j *DataRetentionJob) Tick(ctx context.Context) {
	if !j.isLeader() {
		return
	}

	now := j.cfg.Now()

	if j.activity != nil {
		j.sweepActivityEvents(ctx, now)
	}
	if j.nudges != nil {
		j.sweepNudgeDispatches(ctx, now)
	}
}

func (j *DataRetentionJob) sweepActivityEvents(ctx context.Context, now time.Time) {
	cutoff := now.Add(-j.cfg.ActivityEventsRetention)
	deleted, err := j.activity.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		j.logger.Error("data retention: activity_events delete failed", "error", err.Error(), "cutoff", cutoff)
		return
	}
	if deleted == 0 {
		return
	}
	j.logger.Info("data retention: activity_events swept", "deleted", deleted, "cutoff", cutoff)
	j.audit(ctx, "data_retention.delete", "activity_events", deleted, cutoff)
}

func (j *DataRetentionJob) sweepNudgeDispatches(ctx context.Context, now time.Time) {
	cutoff := now.Add(-j.cfg.NudgeDispatchRetention)
	deleted, err := j.nudges.DeleteDispatchesOlderThan(ctx, cutoff)
	if err != nil {
		j.logger.Error("data retention: nudge_dispatch_log delete failed", "error", err.Error(), "cutoff", cutoff)
		return
	}
	if deleted == 0 {
		return
	}
	j.logger.Info("data retention: nudge_dispatch_log swept (nudge_outcomes cascaded)", "deleted", deleted, "cutoff", cutoff)
	j.audit(ctx, "data_retention.delete", "nudge_dispatch_log", deleted, cutoff)
}

// audit records the deletion itself (nester#1226 acceptance criterion:
// "Deletion is itself audit-logged"). No per-row entity IDs — a retention
// sweep can remove thousands of rows in one pass, and the identity of an
// individual deleted operational-log row is not the information an operator
// reviewing this trail needs; the count and cutoff are. UserID is nil: this
// is a system action, not attributable to any one user's account activity.
func (j *DataRetentionJob) audit(ctx context.Context, action, entityType string, deletedCount int64, cutoff time.Time) {
	if j.auditLog == nil {
		return
	}
	entry := audit.Entry{
		Action:     action,
		EntityType: entityType,
		EntityID:   uuid.Nil,
		NewValue: map[string]any{
			"deleted_count": deletedCount,
			"cutoff":        cutoff.Format(time.RFC3339),
		},
	}
	if err := j.auditLog.Log(ctx, entry); err != nil {
		j.logger.Warn("data retention: audit log write failed", "error", err.Error(), "entity_type", entityType)
	}
}
