package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/goalnotification"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// GoalMilestoneNotifier delivers savings goal progress milestone notifications.
type GoalMilestoneNotifier interface {
	SendGoalMilestone(ctx context.Context, userID uuid.UUID, goal savingsgoal.SavingsGoal, milestone int)
}

type noopGoalMilestoneNotifier struct{}

func (noopGoalMilestoneNotifier) SendGoalMilestone(context.Context, uuid.UUID, savingsgoal.SavingsGoal, int) {}

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

	_ = n.Dispatcher.Send(ctx, userID, notifications.EventGoalMilestone, title, body, payload)
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
