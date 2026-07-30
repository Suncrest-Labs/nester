package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// WebhookGoalMilestoneNotifier fires webhooks on goal milestones and deadline breaches.
type WebhookGoalMilestoneNotifier struct {
	Svc *WebhookService
}

func (n WebhookGoalMilestoneNotifier) SendGoalMilestone(
	ctx context.Context,
	userID uuid.UUID,
	goal savingsgoal.SavingsGoal,
	milestone int,
) {
	if n.Svc == nil {
		return
	}
	var event string
	switch milestone {
	case 25, 50, 75, 100:
		event = fmt.Sprintf("goal.milestone.%d", milestone)
	case -1:
		event = "goal.deadline_breach"
	default:
		event = fmt.Sprintf("goal.milestone.%d", milestone)
	}

	milestonePct := milestone
	if milestone == -1 {
		milestonePct = 0
	}

	payload, err := BuildWebhookPayload(
		event,
		goal.ID,
		userID,
		milestonePct,
		goal.CurrentAmount.String(),
		goal.TargetAmount.String(),
	)
	if err != nil {
		return
	}
	n.Svc.FireForUser(ctx, userID, event, payload)
}

// CompositeGoalMilestoneNotifier fans out to multiple GoalMilestoneNotifier implementations.
type CompositeGoalMilestoneNotifier struct {
	Notifiers []GoalMilestoneNotifier
}

func (c CompositeGoalMilestoneNotifier) SendGoalMilestone(
	ctx context.Context,
	userID uuid.UUID,
	goal savingsgoal.SavingsGoal,
	milestone int,
) {
	for _, n := range c.Notifiers {
		n.SendGoalMilestone(ctx, userID, goal, milestone)
	}
}
