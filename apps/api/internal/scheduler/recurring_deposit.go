package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// DepositRecorder records a scheduled deposit against a vault ledger.
type DepositRecorder interface {
	RecordScheduledDeposit(ctx context.Context, userID, vaultID uuid.UUID, amount decimal.Decimal, scheduleID uuid.UUID) error
}

// GoalProgressChecker returns whether a savings goal has been completed.
type GoalProgressChecker interface {
	IsGoalCompleted(ctx context.Context, goalID, userID uuid.UUID) (bool, string, error)
}

// ScheduleStore loads and updates recurring deposit schedules.
type ScheduleStore interface {
	ListDue(ctx context.Context, now time.Time) ([]savingsschedule.SavingsSchedule, error)
	UpdateAfterRun(ctx context.Context, id uuid.UUID, lastRunAt, nextRunAt time.Time) error
	Deactivate(ctx context.Context, id uuid.UUID) error
}

// DepositNotifier emits a user notification after a successful scheduled deposit.
type DepositNotifier interface {
	NotifyScheduledDeposit(ctx context.Context, userID uuid.UUID, amount decimal.Decimal, currency, goalName string) error
}

// RecurringDepositConfig controls the hourly recurring deposit loop.
type RecurringDepositConfig struct {
	Enabled  bool
	Interval time.Duration
}

const defaultRecurringDepositInterval = time.Hour

// RecurringDepositJob processes due savings schedules and records deposits.
type RecurringDepositJob struct {
	cfg      RecurringDepositConfig
	schedules ScheduleStore
	deposits DepositRecorder
	goals    GoalProgressChecker
	notify   DepositNotifier
	logger   *slog.Logger
	clock    func() time.Time
	lastTickEnd atomic.Int64
}

func NewRecurringDepositJob(
	cfg RecurringDepositConfig,
	schedules ScheduleStore,
	deposits DepositRecorder,
	goals GoalProgressChecker,
	notify DepositNotifier,
	logger *slog.Logger,
) *RecurringDepositJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultRecurringDepositInterval
	}
	if notify == nil {
		notify = noopDepositNotifier{}
	}
	return &RecurringDepositJob{
		cfg:       cfg,
		schedules: schedules,
		deposits:  deposits,
		goals:     goals,
		notify:    notify,
		logger:    logger,
		clock:     func() time.Time { return time.Now().UTC() },
	}
}

func (j *RecurringDepositJob) SetClock(clock func() time.Time) {
	j.clock = clock
}

// Run drives the loop until ctx is cancelled.
func (j *RecurringDepositJob) Run(ctx context.Context) {
	if !j.cfg.Enabled {
		j.logger.Info("recurring deposit job disabled; not starting")
		return
	}
	j.logger.Info("recurring deposit job starting", "interval", j.cfg.Interval)

	j.Tick(ctx)

	ticker := time.NewTicker(j.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			j.logger.Info("recurring deposit job stopping")
			return
		case <-ticker.C:
			j.Tick(ctx)
		}
	}
}

// Tick runs a single pass over all due schedules. Exported for tests.
func (j *RecurringDepositJob) Tick(ctx context.Context) {
	defer j.lastTickEnd.Store(j.clock().UnixNano())

	now := j.clock()
	due, err := j.schedules.ListDue(ctx, now)
	if err != nil {
		j.logger.Error("recurring deposit job: list due schedules failed", "error", err)
		return
	}

	for _, schedule := range due {
		j.processSchedule(ctx, schedule, now)
	}
}

func (j *RecurringDepositJob) processSchedule(ctx context.Context, schedule savingsschedule.SavingsSchedule, now time.Time) {
	completed, goalName, err := j.goals.IsGoalCompleted(ctx, schedule.GoalID, schedule.UserID)
	if err != nil {
		j.logger.Warn("recurring deposit job: goal check failed",
			"schedule_id", schedule.ID,
			"goal_id", schedule.GoalID,
			"error", err,
		)
		return
	}
	if completed {
		if err := j.schedules.Deactivate(ctx, schedule.ID); err != nil {
			j.logger.Warn("recurring deposit job: deactivate completed goal schedule failed",
				"schedule_id", schedule.ID,
				"error", err,
			)
		}
		return
	}

	if err := j.deposits.RecordScheduledDeposit(ctx, schedule.UserID, schedule.VaultID, schedule.Amount, schedule.ID); err != nil {
		j.logger.Warn("recurring deposit job: record deposit failed",
			"schedule_id", schedule.ID,
			"vault_id", schedule.VaultID,
			"error", err,
		)
		return
	}

	nextRun := NextRunAt(now, string(schedule.Frequency))
	if err := j.schedules.UpdateAfterRun(ctx, schedule.ID, now, nextRun); err != nil {
		j.logger.Warn("recurring deposit job: update schedule failed",
			"schedule_id", schedule.ID,
			"error", err,
		)
		return
	}

	if err := j.notify.NotifyScheduledDeposit(ctx, schedule.UserID, schedule.Amount, schedule.Currency, goalName); err != nil {
		j.logger.Warn("recurring deposit job: notification failed",
			"schedule_id", schedule.ID,
			"user_id", schedule.UserID,
			"error", err,
		)
	}

	j.logger.Info("recurring deposit job: deposit recorded",
		"schedule_id", schedule.ID,
		"goal_id", schedule.GoalID,
		"amount", schedule.Amount.String(),
	)
}

// LastTickEnd returns the wall-clock time of the last completed tick.
func (j *RecurringDepositJob) LastTickEnd() time.Time {
	v := j.lastTickEnd.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

type noopDepositNotifier struct{}

func (noopDepositNotifier) NotifyScheduledDeposit(context.Context, uuid.UUID, decimal.Decimal, string, string) error {
	return nil
}

// NotificationDepositNotifier adapts notifications.Dispatcher for scheduled deposits.
type NotificationDepositNotifier struct {
	Dispatcher *notifications.Dispatcher
}

func (n NotificationDepositNotifier) NotifyScheduledDeposit(
	ctx context.Context,
	userID uuid.UUID,
	amount decimal.Decimal,
	currency, goalName string,
) error {
	if n.Dispatcher == nil {
		return nil
	}
	body := fmt.Sprintf("Your scheduled deposit of $%s %s toward %s was completed.", amount.StringFixed(2), currency, goalName)
	return n.Dispatcher.Send(ctx, userID, notifications.EventScheduledDepositCompleted,
		"Scheduled deposit completed", body, map[string]any{
			"amount":    amount.String(),
			"currency":  currency,
			"goal_name": goalName,
		})
}
