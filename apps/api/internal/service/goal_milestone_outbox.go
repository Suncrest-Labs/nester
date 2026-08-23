package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/outbox"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// GoalMilestoneJobType is the job type the outbox relay routes
// OutboxEventGoalMilestone to (#1049).
const GoalMilestoneJobType = "goal_milestone_notify"

// OutboxEventGoalMilestone is the outbox event type for "this goal crossed
// this milestone; tell the user and their webhook subscribers".
const OutboxEventGoalMilestone = "savings_goal.milestone"

// GoalMilestonePayload is the outbox payload for a milestone side effect.
//
// The goal is snapshotted rather than re-read at delivery time. The
// notification describes what happened when it happened; re-reading would
// describe the goal as it is whenever the queue gets around to it, which for
// a delayed or redelivered event is a different — and wrong — statement.
type GoalMilestonePayload struct {
	UserID    uuid.UUID               `json:"user_id"`
	Milestone int                     `json:"milestone"`
	Goal      savingsgoal.SavingsGoal `json:"goal"`
	DedupeKey string                  `json:"dedupe_key"`
}

// NewGoalMilestoneEvent builds the outbox event the savings-goal service
// inserts inside the same transaction that records the milestone as
// notified. That shared transaction is what makes "we marked it notified"
// and "we will notify" the same fact rather than two that can disagree.
//
// The aggregate is the goal, so milestones for one goal are delivered in
// order (25 before 50 before 75) while other goals are unaffected.
func NewGoalMilestoneEvent(userID uuid.UUID, goal savingsgoal.SavingsGoal, milestone int) (outbox.Event, error) {
	dedupeKey := MilestoneDedupeKey(goal.ID, milestone)
	return outbox.NewEvent("savings_goal", goal.ID.String(), OutboxEventGoalMilestone, dedupeKey, GoalMilestonePayload{
		UserID:    userID,
		Milestone: milestone,
		Goal:      goal,
		DedupeKey: dedupeKey,
	})
}

// NewGoalMilestoneJobHandler builds the jobqueue.Handler that delivers a
// milestone side effect by invoking the notifier chain.
//
// The chain is the same one the pre-outbox code called from a detached
// goroutine — dispatcher, webhooks, nudge engine — so behaviour is
// unchanged. What changed is that the call now happens from durable,
// restart-surviving work instead of a goroutine that dies with the process,
// and that every member of the chain is handed the dedupe key so a repeat
// delivery is recognisable as one.
func NewGoalMilestoneJobHandler(notifier GoalMilestoneNotifier, logger *slog.Logger) jobqueue.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}
	return jobqueue.HandlerFunc(func(ctx context.Context, job jobqueue.Job) error {
		if notifier == nil {
			return jobqueue.Permanent(errors.New("goal milestone: no notifier configured"))
		}
		var p GoalMilestonePayload
		if err := json.Unmarshal(job.Payload, &p); err != nil {
			return jobqueue.Permanent(fmt.Errorf("goal milestone: unmarshal payload: %w", err))
		}
		if p.DedupeKey == "" {
			return jobqueue.Permanent(errors.New("goal milestone: payload carries no dedupe key"))
		}

		notifier.SendGoalMilestone(ctx, p.UserID, p.Goal, p.Milestone, p.DedupeKey)
		logger.Debug("goal milestone notified",
			"user_id", p.UserID, "goal_id", p.Goal.ID, "milestone", p.Milestone, "dedupe_key", p.DedupeKey)
		return nil
	})
}

// MilestoneOutboxRecorder is the savingsgoal.Repository extension that
// records a milestone as notified and the side effect that follows from it
// in ONE transaction.
//
// It is a separate, optional interface rather than a method on
// savingsgoal.Repository because the guarantee it offers is inherently
// database-backed: an in-memory repository cannot provide it, and pretending
// otherwise by adding a method every implementation must stub would hide
// exactly the distinction that matters here. SavingsGoalService checks for
// it and falls back to the old non-atomic path when it is absent.
type MilestoneOutboxRecorder interface {
	UpdateMilestonesWithOutbox(ctx context.Context, goalID uuid.UUID, milestones []int, events []outbox.Event) error
}
