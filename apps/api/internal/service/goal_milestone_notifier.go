package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// GoalMilestoneNotifier delivers savings goal progress milestone notifications.
type GoalMilestoneNotifier interface {
	SendGoalMilestone(ctx context.Context, userID uuid.UUID, goal savingsgoal.SavingsGoal, milestone int)
}

type noopGoalMilestoneNotifier struct{}

func (noopGoalMilestoneNotifier) SendGoalMilestone(context.Context, uuid.UUID, savingsgoal.SavingsGoal, int) {}

// DispatcherGoalMilestoneNotifier sends milestone notifications via the notifications dispatcher.
type DispatcherGoalMilestoneNotifier struct {
	Dispatcher *notifications.Dispatcher
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
	_ = n.Dispatcher.Send(ctx, userID, notifications.EventGoalMilestone, title, body, map[string]any{
		"goal_id":   goal.ID.String(),
		"milestone": milestone,
		"currency":  goal.Currency,
	})
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
