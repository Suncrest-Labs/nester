package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// SavingsGoalPurgeRepository is the subset of savingsgoal.Repository the
// purge job needs to find and permanently remove expired soft-deletes (#924).
type SavingsGoalPurgeRepository interface {
	ListDeletedOlderThan(ctx context.Context, cutoff time.Time) ([]savingsgoal.SavingsGoal, error)
	HardDelete(ctx context.Context, id uuid.UUID) error
}

// SavingsGoalPurgeJob periodically hard-deletes savings goals whose
// deleted_at is older than savingsgoal.SavingsGoalRecoveryWindow (#924),
// permanently removing rows the recovery window has expired for. Mirrors
// GoalCoachingScheduler's tick-on-a-timer shape (see goal_coaching_scheduler.go).
type SavingsGoalPurgeJob struct {
	repo   SavingsGoalPurgeRepository
	logger *slog.Logger
	clock  func() time.Time
	leader LeaderChecker
}

// NewSavingsGoalPurgeJob constructs the purge job. logger may be nil.
func NewSavingsGoalPurgeJob(repo SavingsGoalPurgeRepository, logger *slog.Logger) *SavingsGoalPurgeJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &SavingsGoalPurgeJob{
		repo:   repo,
		logger: logger,
		clock:  func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the job's clock (tests only).
func (j *SavingsGoalPurgeJob) SetClock(clock func() time.Time) { j.clock = clock }

// SetLeaderChecker wires leader election. Not money-moving, but running the
// sweep from multiple instances is still harmless-but-wasteful, so it's
// classified the same as the other sweep jobs: a nil checker means "always
// leader" (single-instance deployments, existing tests).
func (j *SavingsGoalPurgeJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *SavingsGoalPurgeJob) isLeader() bool {
	return j.leader == nil || j.leader.IsLeader()
}

// Run ticks every `interval` until ctx is cancelled, purging expired
// soft-deletes on each tick. A tick also fires once immediately on start.
func (j *SavingsGoalPurgeJob) Run(ctx context.Context, interval time.Duration) {
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

// Tick runs a single purge pass. Exported for tests.
func (j *SavingsGoalPurgeJob) Tick(ctx context.Context) {
	if j.repo == nil || !j.isLeader() {
		return
	}

	cutoff := j.clock().Add(-savingsgoal.SavingsGoalRecoveryWindow)
	expired, err := j.repo.ListDeletedOlderThan(ctx, cutoff)
	if err != nil {
		j.logger.Error("savings goal purge: list expired soft-deletes failed", "error", err.Error())
		return
	}

	for _, goal := range expired {
		if err := j.repo.HardDelete(ctx, goal.ID); err != nil {
			j.logger.Warn("savings goal purge: hard delete failed", "goal_id", goal.ID.String(), "error", err.Error())
			continue
		}
		j.logger.Info("savings goal purge: hard deleted expired soft-delete", "goal_id", goal.ID.String())
	}
}
