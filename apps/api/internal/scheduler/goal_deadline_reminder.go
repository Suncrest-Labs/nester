package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// GoalDeadlineRepository fetches goals approaching their deadlines and updates sent reminders.
type GoalDeadlineRepository interface {
	ListActiveApproachingDeadline(ctx context.Context, maxDays int) ([]savingsgoal.SavingsGoal, error)
	UpdateDeadlineReminders(ctx context.Context, goalID uuid.UUID, reminders []int) error
}

// GoalProgressResolver gets the current progress of a goal.
type GoalProgressResolver interface {
	EnrichProgress(ctx context.Context, goal savingsgoal.SavingsGoal) (savingsgoal.SavingsGoal, error)
}

// DeadlineNotifier sends the deadline reminder notification.
type DeadlineNotifier interface {
	Send(ctx context.Context, userID uuid.UUID, evt notifications.EventType, title, body string, payload map[string]any) error
}

// GoalDeadlineReminderConfig controls the deadline reminder job.
type GoalDeadlineReminderConfig struct {
	Enabled  bool
	Interval time.Duration
}

// GoalDeadlineReminderJob runs daily and sends notifications at 7, 3, and 1 days before deadline.
type GoalDeadlineReminderJob struct {
	cfg       GoalDeadlineReminderConfig
	repo      GoalDeadlineRepository
	progress  GoalProgressResolver
	notifier  DeadlineNotifier
	logger    *slog.Logger
	clock     func() time.Time
	lastTickEnd atomic.Int64
}

// NewGoalDeadlineReminderJob constructs the job.
func NewGoalDeadlineReminderJob(
	cfg GoalDeadlineReminderConfig,
	repo GoalDeadlineRepository,
	progress GoalProgressResolver,
	notifier DeadlineNotifier,
	logger *slog.Logger,
) *GoalDeadlineReminderJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return &GoalDeadlineReminderJob{
		cfg:      cfg,
		repo:     repo,
		progress: progress,
		notifier: notifier,
		logger:   logger,
		clock:    func() time.Time { return time.Now().UTC() },
	}
}

func (j *GoalDeadlineReminderJob) SetClock(clock func() time.Time) {
	j.clock = clock
}

// Run drives the check loop until ctx is cancelled.
func (j *GoalDeadlineReminderJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("goal-deadline-reminder: disabled; not starting")
		return
	}
	j.logger.Info("goal-deadline-reminder: starting", "interval", j.cfg.Interval)

	j.Tick(ctx)

	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("goal-deadline-reminder: stopping")
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

// Tick runs a single pass over all goals approaching their deadlines. Exported for tests.
func (j *GoalDeadlineReminderJob) Tick(ctx context.Context) {
	defer j.lastTickEnd.Store(j.clock().UnixNano())

	goals, err := j.repo.ListActiveApproachingDeadline(ctx, 7)
	if err != nil {
		j.logger.Error("goal-deadline-reminder: list active approaching deadline failed", "error", err)
		return
	}

	for _, goal := range goals {
		j.processGoal(ctx, goal)
	}
}

func (j *GoalDeadlineReminderJob) processGoal(ctx context.Context, goal savingsgoal.SavingsGoal) {
	enriched, err := j.progress.EnrichProgress(ctx, goal)
	if err != nil {
		j.logger.Error("goal-deadline-reminder: get progress failed", "goal_id", goal.ID, "error", err)
		return
	}

	if enriched.ProgressPct >= 100 {
		return
	}

	now := j.clock()
	daysLeft := int(math.Ceil(enriched.Deadline.Sub(now).Hours() / 24.0))

	var threshold int
	if daysLeft <= 1 {
		threshold = 1
	} else if daysLeft <= 3 {
		threshold = 3
	} else if daysLeft <= 7 {
		threshold = 7
	} else {
		return
	}

	for _, sent := range enriched.DeadlineRemindersSent {
		if sent == threshold {
			return
		}
	}

	var title, body string
	goalName := enriched.Name
	if goalName == "" {
		goalName = "your goal"
	}

	switch threshold {
	case 7:
		title = "7 days left!"
		body = fmt.Sprintf("You have 7 days to reach %s. You're at %.0f%%.", goalName, enriched.ProgressPct)
	case 3:
		title = "3 days remaining"
		body = fmt.Sprintf("Only 3 days left on %s. Deposit now to stay on track.", goalName)
	case 1:
		title = "Last day!"
		body = fmt.Sprintf("Today is your last day to fund %s.", goalName)
	}

	if err := j.notifier.Send(ctx, enriched.UserID, notifications.EventGoalMilestone, title, body, map[string]any{
		"goal_id": enriched.ID.String(),
		"days_left": threshold,
		"progress_pct": enriched.ProgressPct,
	}); err != nil {
		j.logger.Error("goal-deadline-reminder: send notification failed", "goal_id", enriched.ID, "error", err)
		return
	}

	newReminders := append(enriched.DeadlineRemindersSent, threshold)
	if err := j.repo.UpdateDeadlineReminders(ctx, enriched.ID, newReminders); err != nil {
		j.logger.Error("goal-deadline-reminder: update reminders failed", "goal_id", enriched.ID, "error", err)
	}
}

// LastTickEnd returns the wall-clock time of the last completed tick.
func (j *GoalDeadlineReminderJob) LastTickEnd() time.Time {
	v := j.lastTickEnd.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}
