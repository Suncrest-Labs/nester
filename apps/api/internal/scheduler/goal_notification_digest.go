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

// Tick runs a single pass over all due preferences. Preferences are grouped
// by UserID and flushed together (nester#1340): a user with several goals
// all on "daily" digest, all due in the same pass, previously received one
// separate email per goal. Grouping here — rather than in the store/SQL
// layer — needs no schema or Repository interface change, since ListDue's
// results already carry UserID per preference.
//
// Exported for tests.
func (j *GoalNotificationDigestJob) Tick(ctx context.Context) {
	now := j.clock()
	due, err := j.store.ListDue(ctx, now)
	if err != nil {
		j.logger.Error("goal notification digest job: list due failed", "error", err)
		return
	}

	byUser := make(map[uuid.UUID][]goalnotification.Preference)
	order := make([]uuid.UUID, 0, len(due))
	for _, pref := range due {
		if _, seen := byUser[pref.UserID]; !seen {
			order = append(order, pref.UserID)
		}
		byUser[pref.UserID] = append(byUser[pref.UserID], pref)
	}

	for _, userID := range order {
		j.flushUser(ctx, userID, byUser[userID], now)
	}
}

// flushUser combines the queued items across every one of userID's due
// goals into a single dispatched notification, then clears the queue and
// marks each goal's digest as sent individually — clearing/marking stays
// per-goal because ClearQueuedItems and MarkDigestSent are scoped to a
// single goal_id in both the Repository interface and the underlying
// tables; only the outward-facing Send call is batched.
func (j *GoalNotificationDigestJob) flushUser(ctx context.Context, userID uuid.UUID, prefs []goalnotification.Preference, now time.Time) {
	type goalItems struct {
		goalID uuid.UUID
		items  []goalnotification.DigestItem
	}

	var perGoal []goalItems
	totalItems := 0
	goalIDs := make([]string, 0, len(prefs))
	for _, pref := range prefs {
		items, err := j.store.ListQueuedItems(ctx, pref.GoalID)
		if err != nil {
			j.logger.Warn("goal notification digest job: list queued items failed", "goal_id", pref.GoalID, "error", err)
			continue
		}
		if len(items) == 0 {
			continue
		}
		perGoal = append(perGoal, goalItems{goalID: pref.GoalID, items: items})
		totalItems += len(items)
		goalIDs = append(goalIDs, pref.GoalID.String())
	}
	if len(perGoal) == 0 {
		return
	}

	body := fmt.Sprintf("%d update(s) across %d of your savings goals:\n", totalItems, len(perGoal))
	for _, g := range perGoal {
		for _, item := range g.items {
			body += fmt.Sprintf("- %s\n", item.Body)
		}
	}

	if j.dispatcher != nil {
		if err := j.dispatcher.Send(ctx, userID, notifications.EventGoalMilestone, "Savings goal digest", body, map[string]any{
			"goal_ids": goalIDs,
			"count":    totalItems,
		}); err != nil {
			j.logger.Warn("goal notification digest job: send failed", "user_id", userID, "goal_ids", goalIDs, "error", err)
		}
	}

	for _, g := range perGoal {
		ids := make([]uuid.UUID, 0, len(g.items))
		for _, item := range g.items {
			ids = append(ids, item.ID)
		}
		if err := j.store.ClearQueuedItems(ctx, g.goalID, ids); err != nil {
			j.logger.Warn("goal notification digest job: clear queue failed", "goal_id", g.goalID, "error", err)
			continue
		}
		if err := j.store.MarkDigestSent(ctx, g.goalID, now); err != nil {
			j.logger.Warn("goal notification digest job: mark sent failed", "goal_id", g.goalID, "error", err)
		}
	}
}
