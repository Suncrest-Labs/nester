package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/goalnotification"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// GoalDigestStore is the persistence seam the digest job needs to find due
// preferences and flush their queued notifications.
type GoalDigestStore interface {
	ListDue(ctx context.Context, now time.Time) ([]goalnotification.Preference, error)
	ListQueuedItems(ctx context.Context, goalID uuid.UUID) ([]goalnotification.DigestItem, error)
	ClearQueuedItems(ctx context.Context, goalID uuid.UUID, itemIDs []uuid.UUID) error
	MarkDigestSent(ctx context.Context, goalID uuid.UUID, sentAt time.Time) error
}

// GoalNotificationDigestConfig controls the digest flush loop.
type GoalNotificationDigestConfig struct {
	Enabled  bool
	Interval time.Duration
}

const defaultGoalDigestInterval = time.Hour

// GoalNotificationDigestJob periodically flushes queued per-goal
// notifications into a single batched notification for goals whose
// preference is "daily" or "weekly" rather than "immediate".
type GoalNotificationDigestJob struct {
	cfg        GoalNotificationDigestConfig
	store      GoalDigestStore
	dispatcher *notifications.Dispatcher
	logger     *slog.Logger
	clock      func() time.Time
}

func NewGoalNotificationDigestJob(
	cfg GoalNotificationDigestConfig,
	store GoalDigestStore,
	dispatcher *notifications.Dispatcher,
	logger *slog.Logger,
) *GoalNotificationDigestJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultGoalDigestInterval
	}
	return &GoalNotificationDigestJob{
		cfg:        cfg,
		store:      store,
		dispatcher: dispatcher,
		logger:     logger,
		clock:      func() time.Time { return time.Now().UTC() },
	}
}

func (j *GoalNotificationDigestJob) SetClock(clock func() time.Time) {
	j.clock = clock
}

// Run drives the loop until ctx is cancelled.
func (j *GoalNotificationDigestJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("goal notification digest job disabled; not starting")
		return
	}
	j.logger.Info("goal notification digest job starting", "interval", j.cfg.Interval)

	j.Tick(ctx)

	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("goal notification digest job stopping")
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

// Tick runs a single pass over all due preferences. Exported for tests.
func (j *GoalNotificationDigestJob) Tick(ctx context.Context) {
	now := j.clock()
	due, err := j.store.ListDue(ctx, now)
	if err != nil {
		j.logger.Error("goal notification digest job: list due failed", "error", err)
		return
	}
	for _, pref := range due {
		j.flush(ctx, pref, now)
	}
}

func (j *GoalNotificationDigestJob) flush(ctx context.Context, pref goalnotification.Preference, now time.Time) {
	items, err := j.store.ListQueuedItems(ctx, pref.GoalID)
	if err != nil {
		j.logger.Warn("goal notification digest job: list queued items failed", "goal_id", pref.GoalID, "error", err)
		return
	}
	if len(items) == 0 {
		return
	}

	ids := make([]uuid.UUID, 0, len(items))
	body := fmt.Sprintf("%d update(s) on your savings goal:\n", len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
		body += fmt.Sprintf("- %s\n", item.Body)
	}

	if j.dispatcher != nil {
		if err := j.dispatcher.Send(ctx, pref.UserID, notifications.EventGoalMilestone, "Savings goal digest", body, map[string]any{
			"goal_id": pref.GoalID.String(),
			"count":   len(items),
		}); err != nil {
			j.logger.Warn("goal notification digest job: send failed", "goal_id", pref.GoalID, "error", err)
		}
	}

	if err := j.store.ClearQueuedItems(ctx, pref.GoalID, ids); err != nil {
		j.logger.Warn("goal notification digest job: clear queue failed", "goal_id", pref.GoalID, "error", err)
		return
	}
	if err := j.store.MarkDigestSent(ctx, pref.GoalID, now); err != nil {
		j.logger.Warn("goal notification digest job: mark sent failed", "goal_id", pref.GoalID, "error", err)
	}
}
