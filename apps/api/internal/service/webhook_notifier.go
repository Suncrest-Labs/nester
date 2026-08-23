package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// WebhookGoalMilestoneNotifier fires webhooks on goal milestones and deadline breaches.
type WebhookGoalMilestoneNotifier struct {
	Svc *WebhookService
	// Logger records fan-out failures. The notifier interface is
	// void-returning, so without this a failed fan-out is invisible.
	Logger *slog.Logger
}

func (n WebhookGoalMilestoneNotifier) SendGoalMilestone(
	ctx context.Context,
	userID uuid.UUID,
	goal savingsgoal.SavingsGoal,
	milestone int,
	dedupeKey string,
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
	// FanOut rather than FireForUser: every delivery id it produces is
	// derived from dedupeKey, so a redelivery of this milestone reaches the
	// subscriber with the same delivery id it already saw and can discard,
	// instead of a fresh one indistinguishable from a new event.
	if err := n.Svc.FanOut(ctx, userID, event, payload, dedupeKey); err != nil && n.Logger != nil {
		n.Logger.Error("webhook: goal milestone fan-out failed",
			"user_id", userID, "goal_id", goal.ID, "milestone", milestone, "error", err)
	}
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
	dedupeKey string,
) {
	for _, n := range c.Notifiers {
		n.SendGoalMilestone(ctx, userID, goal, milestone, dedupeKey)
	}
}
