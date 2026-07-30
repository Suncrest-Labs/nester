package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/nudge"
)

type NudgeOutcomeService struct {
	outcomeRepo nudge.OutcomeRepository
}

func NewNudgeOutcomeService(outcomeRepo nudge.OutcomeRepository) *NudgeOutcomeService {
	return &NudgeOutcomeService{outcomeRepo: outcomeRepo}
}

func (s *NudgeOutcomeService) RecordDeposit(ctx context.Context, userID uuid.UUID, occurredAt time.Time) error {
	return s.outcomeRepo.RecordOutcome(ctx, userID, "deposit", occurredAt)
}

func (s *NudgeOutcomeService) RecordGoalCompletion(ctx context.Context, userID uuid.UUID, occurredAt time.Time) error {
	return s.outcomeRepo.RecordOutcome(ctx, userID, "goal_completed", occurredAt)
}

func (s *NudgeOutcomeService) RecordReturnVisit(ctx context.Context, userID uuid.UUID, occurredAt time.Time) error {
	return s.outcomeRepo.RecordOutcome(ctx, userID, "return_visit", occurredAt)
}
