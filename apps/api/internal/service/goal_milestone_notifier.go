package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/goalnotification"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// GoalMilestoneNotifier delivers savings goal progress milestone notifications.
//
// dedupeKey identifies the milestone event itself and is stable across every
// redelivery of it (see MilestoneDedupeKey). Implementations MUST be
// idempotent with respect to it: since #1049 these notifications are driven
// by the transactional outbox and therefore by an at-least-once queue, so a
// notifier that simply sends whatever it is handed will eventually tell the
// user the same thing twice.
type GoalMilestoneNotifier interface {
	SendGoalMilestone(ctx context.Context, userID uuid.UUID, goal savingsgoal.SavingsGoal, milestone int, dedupeKey string)
}

// MilestoneDedupeKey derives the stable key for one goal reaching one
// milestone. Derived from the goal and the milestone rather than generated,
// so the same milestone always produces the same key — in this process and
// in the one that picks the event up after a restart.
func MilestoneDedupeKey(goalID uuid.UUID, milestone int) string {
	return fmt.Sprintf("savings_goal:%s:milestone:%d", goalID, milestone)
}

// milestoneDedupWindow is how long a repeat of the same milestone is
// suppressed. It only has to cover redelivery (queue backoff plus a relay
// outage), because a genuine second crossing of the same milestone is
// already prevented by notified_milestones.
const milestoneDedupWindow = 6 * time.Hour

type noopGoalMilestoneNotifier struct{}

func (noopGoalMilestoneNotifier) SendGoalMilestone(context.Context, uuid.UUID, savingsgoal.SavingsGoal, int, string) {
}

// GoalNotificationPreferenceReader is the read/write seam DispatcherGoalMilestoneNotifier
// uses to respect a goal's mute + digest-frequency preference (mute/frequency per goal).
type GoalNotificationPreferenceReader interface {
	Get(ctx context.Context, goalID uuid.UUID) (*goalnotification.Preference, error)
	EnqueueDigestItem(ctx context.Context, item goalnotification.DigestItem) error
}

// DispatcherGoalMilestoneNotifier sends milestone notifications via the notifications dispatcher.
type DispatcherGoalMilestoneNotifier struct {
	Dispatcher *notifications.Dispatcher
	// Preferences is optional; when nil every milestone is delivered
	// immediately (the pre-existing behavior). When set, a muted goal is
	// skipped entirely and a non-immediate frequency queues the notification
	// for later batched delivery instead of sending it now.
	Preferences GoalNotificationPreferenceReader
}

func (n DispatcherGoalMilestoneNotifier) SendGoalMilestone(
	ctx context.Context,
	userID uuid.UUID,
	goal savingsgoal.SavingsGoal,
	milestone int,
	dedupeKey string,
) {
	if n.Dispatcher == nil {
		return
	}
	title, body := milestoneNotificationContent(milestone, savingsgoal.GoalDisplayName(goal))
	payload := map[string]any{
		"goal_id":   goal.ID.String(),
		"milestone": milestone,
		"currency":  goal.Currency,
	}

	if n.Preferences != nil {
		pref, err := n.Preferences.Get(ctx, goal.ID)
		if err == nil && pref != nil {
			if pref.Muted {
				return
			}
			if pref.DigestFrequency != goalnotification.FrequencyImmediate {
				_ = n.Preferences.EnqueueDigestItem(ctx, goalnotification.DigestItem{
					ID:      uuid.New(),
					GoalID:  goal.ID,
					UserID:  userID,
					Title:   title,
					Body:    body,
					Payload: payload,
				})
				return
			}
		}
	}

	// SendWithOptions rather than Send: the dedupe key is what stops an
	// at-least-once redelivery of this milestone from being shown to the
	// user a second time. A suppressed repeat is still persisted, so the
	// redelivery is auditable rather than invisible.
	_ = n.Dispatcher.SendWithOptions(ctx, userID, notifications.EventGoalMilestone, title, body, payload,
		notifications.SendOptions{DedupKey: dedupeKey, DedupWindow: milestoneDedupWindow})
}

func milestoneNotificationContent(milestone int, goalName string) (string, string) {
	switch milestone {
	case 25:
		return "Great start!", fmt.Sprintf("You're 25%% of the way to your %s goal. Keep it up!", goalName)
	case 50:
		return "Halfway there!", fmt.Sprintf("You've hit the halfway mark on %s. You're on track!", goalName)
	case 75:
		return "Almost there!", fmt.Sprintf("75%% of %s funded. One more push and you're done!", goalName)
	case 100:
		return "Goal achieved! 🎉", fmt.Sprintf("Congratulations! You've fully funded your %s goal.", goalName)
	default:
		return "Savings milestone", fmt.Sprintf("You've reached %d%% of your %s goal.", milestone, goalName)
	}
}
