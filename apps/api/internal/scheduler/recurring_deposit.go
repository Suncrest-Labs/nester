package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// RecurringDepositJobType is the job-queue type recurring-deposit
// occurrences are enqueued under (#846).
const RecurringDepositJobType = "recurring_deposit"

// DepositRecorder records a scheduled deposit against a vault ledger. It is
// invoked from RecurringDepositJobHandler (async, via the job queue), not
// directly from the RecurringDepositJob sweep loop.
//
// occurrenceAt is the due timestamp of THIS occurrence (the schedule's
// NextRunAt at the time it was picked up) — not "now". Implementations MUST
// fold it into whatever uniquely identifies the on-ledger record (e.g. the
// transaction hash) because a recurring schedule fires many times over its
// life; scheduleID alone identifies the SCHEDULE, not a single occurrence,
// and reusing it verbatim across occurrences collides against the
// vault_transactions.transaction_hash UNIQUE index. That was the #846 bug:
// internal/service/scheduled_deposit_adapters.go previously built txHash
// from scheduleID alone, so only a schedule's first-ever occurrence could
// ever be recorded — every later occurrence hit the unique constraint,
// logged an error, and (since UpdateAfterRun never ran on failure) retried
// and failed forever.
type DepositRecorder interface {
	RecordScheduledDeposit(ctx context.Context, userID, vaultID uuid.UUID, amount decimal.Decimal, scheduleID uuid.UUID, occurrenceAt time.Time) error
}

// GoalProgressChecker returns whether a savings goal has been completed.
type GoalProgressChecker interface {
	IsGoalCompleted(ctx context.Context, goalID, userID uuid.UUID) (bool, string, error)
	IsGoalPausedOrArchived(ctx context.Context, goalID, userID uuid.UUID) (bool, error)
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

// Enqueuer submits durable jobs. *jobqueue.Client satisfies it. Defined
// locally (rather than importing harvest.Enqueuer) so this package doesn't
// depend on the harvest package for an identical one-method interface.
type Enqueuer interface {
	Enqueue(ctx context.Context, in jobqueue.EnqueueInput) (jobqueue.Job, error)
}

// RecurringDepositConfig controls the hourly recurring deposit sweep loop.
type RecurringDepositConfig struct {
	Enabled  bool
	Interval time.Duration
	// Window buckets the job-queue idempotency key so repeated sweep ticks
	// hitting the same still-due occurrence (the schedule's NextRunAt hasn't
	// advanced yet because the async job hasn't completed) collapse to a
	// single enqueued job instead of piling up duplicates. Mirrors
	// harvest.Config.Window. Zero uses defaultRecurringDepositInterval.
	Window time.Duration
}

const defaultRecurringDepositInterval = time.Hour

func (c RecurringDepositConfig) window() time.Duration {
	if c.Window <= 0 {
		return defaultRecurringDepositInterval
	}
	return c.Window
}

// RecurringDepositJobPayload is the job-queue payload for a single
// recurring-deposit occurrence.
type RecurringDepositJobPayload struct {
	ScheduleID   uuid.UUID       `json:"schedule_id"`
	UserID       uuid.UUID       `json:"user_id"`
	VaultID      uuid.UUID       `json:"vault_id"`
	GoalID       uuid.UUID       `json:"goal_id"`
	Amount       decimal.Decimal `json:"amount"`
	Currency     string          `json:"currency"`
	Frequency    string          `json:"frequency"`
	GoalName     string          `json:"goal_name"`
	// OccurrenceAt is the schedule's NextRunAt at enqueue time: the due
	// timestamp this specific occurrence represents. Used by the handler
	// both to derive a stable per-occurrence transaction hash and to compute
	// the next NextRunAt after a successful deposit.
	OccurrenceAt time.Time `json:"occurrence_at"`
}

// RecurringDepositJob processes due savings schedules and enqueues durable
// deposit-recording jobs. Classified SINGLETON (#846): it moves money, so it
// must run on exactly one instance — see SetLeaderChecker.
type RecurringDepositJob struct {
	cfg         RecurringDepositConfig
	schedules   ScheduleStore
	goals       GoalProgressChecker
	queue       Enqueuer
	logger      *slog.Logger
	clock       func() time.Time
	lastTickEnd atomic.Int64
	leader      LeaderChecker
}

func NewRecurringDepositJob(
	cfg RecurringDepositConfig,
	schedules ScheduleStore,
	queue Enqueuer,
	goals GoalProgressChecker,
	logger *slog.Logger,
) *RecurringDepositJob {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultRecurringDepositInterval
	}
	return &RecurringDepositJob{
		cfg:       cfg,
		schedules: schedules,
		goals:     goals,
		queue:     queue,
		logger:    logger,
		clock:     func() time.Time { return time.Now().UTC() },
	}
}

func (j *RecurringDepositJob) SetClock(clock func() time.Time) {
	j.clock = clock
}

// SetLeaderChecker wires leader election (#846). Money-moving: classified
// SINGLETON. A nil checker means "always leader" (single-instance
// deployments, existing tests).
func (j *RecurringDepositJob) SetLeaderChecker(l LeaderChecker) { j.leader = l }

func (j *RecurringDepositJob) isLeader() bool {
	return j.leader == nil || j.leader.IsLeader()
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

	if !j.isLeader() {
		j.logger.Debug("recurring deposit job: skipping tick, not leader")
		return
	}

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
	paused, err := j.goals.IsGoalPausedOrArchived(ctx, schedule.GoalID, schedule.UserID)
	if err != nil {
		j.logger.Warn("recurring deposit job: goal status check failed",
			"schedule_id", schedule.ID,
			"goal_id", schedule.GoalID,
			"error", err,
		)
		return
	}
	if paused {
		j.logger.Info("recurring deposit job: skipping paused or archived goal",
			"schedule_id", schedule.ID,
			"goal_id", schedule.GoalID,
		)
		return
	}

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

	// Execution-time recheck (#846 split-brain guard): a tick sweeping many
	// schedules can take long enough for leadership, true at the top of
	// Tick, to be lost partway through. Recheck immediately before the
	// actual money-moving action (the enqueue) so a demoted instance never
	// submits.
	if !j.isLeader() {
		j.logger.Debug("recurring deposit job: leadership lost mid-tick, skipping enqueue", "schedule_id", schedule.ID)
		return
	}

	payload, err := json.Marshal(RecurringDepositJobPayload{
		ScheduleID:   schedule.ID,
		UserID:       schedule.UserID,
		VaultID:      schedule.VaultID,
		GoalID:       schedule.GoalID,
		Amount:       schedule.Amount,
		Currency:     schedule.Currency,
		Frequency:    string(schedule.Frequency),
		GoalName:     goalName,
		OccurrenceAt: schedule.NextRunAt,
	})
	if err != nil {
		j.logger.Error("recurring deposit job: marshal payload failed", "schedule_id", schedule.ID, "error", err)
		return
	}

	if _, err := j.queue.Enqueue(ctx, jobqueue.EnqueueInput{
		Type:           RecurringDepositJobType,
		Payload:        payload,
		IdempotencyKey: recurringDepositIdempotencyKey(schedule.ID, schedule.NextRunAt, j.cfg.window()),
		CorrelationID:  schedule.ID.String(),
	}); err != nil {
		j.logger.Warn("recurring deposit job: enqueue failed",
			"schedule_id", schedule.ID,
			"vault_id", schedule.VaultID,
			"error", err,
		)
		return
	}

	j.logger.Info("recurring deposit job: occurrence enqueued",
		"schedule_id", schedule.ID,
		"goal_id", schedule.GoalID,
		"amount", schedule.Amount.String(),
	)
}

// recurringDepositIdempotencyKey collapses every enqueue attempt for the
// SAME occurrence (same schedule, same due timestamp bucket) to a single
// job — mirroring harvest.Engine.idempotencyKey's "{entity}:{bucket}"
// pattern. Distinct occurrences (a later NextRunAt, once the schedule
// actually advances) get distinct keys, which is what lets the same
// schedule legitimately fire again next week/month.
func recurringDepositIdempotencyKey(scheduleID uuid.UUID, occurrenceAt time.Time, window time.Duration) string {
	bucket := occurrenceAt.Truncate(window).Unix()
	return fmt.Sprintf("%s:%d", scheduleID, bucket)
}

// LastTickEnd returns the wall-clock time of the last completed tick.
func (j *RecurringDepositJob) LastTickEnd() time.Time {
	v := j.lastTickEnd.Load()
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// RecurringDepositJobHandler adapts DepositRecorder/ScheduleStore/
// DepositNotifier to the job-queue Handler interface, executing one
// recurring-deposit occurrence. Registered on the shared jobqueue.Worker
// alongside the harvest handler (see harvest.JobHandler for the pattern
// this mirrors).
type RecurringDepositJobHandler struct {
	deposits  DepositRecorder
	schedules ScheduleStore
	notify    DepositNotifier
	logger    *slog.Logger
}

// NewRecurringDepositJobHandler constructs a RecurringDepositJobHandler.
// logger may be nil; notify may be nil (defaults to a no-op).
func NewRecurringDepositJobHandler(
	deposits DepositRecorder,
	schedules ScheduleStore,
	notify DepositNotifier,
	logger *slog.Logger,
) *RecurringDepositJobHandler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	if notify == nil {
		notify = noopDepositNotifier{}
	}
	return &RecurringDepositJobHandler{deposits: deposits, schedules: schedules, notify: notify, logger: logger}
}

// Handle decodes the payload, records the deposit, advances the schedule,
// and notifies the user. A malformed payload is a permanent failure
// (dead-lettered immediately); a transient failure recording the deposit or
// advancing the schedule is returned as-is so the queue retries with
// backoff — safe because RecordScheduledDeposit is idempotent per occurrence
// (see DepositRecorder) and vault.ErrDuplicateTransaction from a retried,
// already-applied deposit is treated as success rather than an error.
func (h *RecurringDepositJobHandler) Handle(ctx context.Context, job jobqueue.Job) error {
	var p RecurringDepositJobPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return jobqueue.Permanent(err)
	}

	if err := h.deposits.RecordScheduledDeposit(ctx, p.UserID, p.VaultID, p.Amount, p.ScheduleID, p.OccurrenceAt); err != nil {
		if !errors.Is(err, vault.ErrDuplicateTransaction) {
			return err
		}
		// A retried delivery of a job whose deposit already landed (e.g. the
		// previous attempt recorded the deposit but crashed/timed out before
		// UpdateAfterRun). The unique transaction_hash — built from
		// scheduleID + OccurrenceAt — makes this detectable and safe to
		// treat as already-applied rather than fail the whole job.
		h.logger.Info("recurring deposit job: deposit already recorded, continuing", "schedule_id", p.ScheduleID)
	}

	nextRun := NextRunAt(p.OccurrenceAt, p.Frequency)
	if err := h.schedules.UpdateAfterRun(ctx, p.ScheduleID, p.OccurrenceAt, nextRun); err != nil {
		return err
	}

	if err := h.notify.NotifyScheduledDeposit(ctx, p.UserID, p.Amount, p.Currency, p.GoalName); err != nil {
		h.logger.Warn("recurring deposit job: notification failed",
			"schedule_id", p.ScheduleID,
			"user_id", p.UserID,
			"error", err,
		)
	}

	h.logger.Info("recurring deposit job: deposit recorded",
		"schedule_id", p.ScheduleID,
		"goal_id", p.GoalID,
		"amount", p.Amount.String(),
	)
	return nil
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
