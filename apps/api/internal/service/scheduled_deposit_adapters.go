package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

// ScheduledDepositService records ledger-only deposits for the recurring deposit job.
type ScheduledDepositService struct {
	vaultSvc *VaultService
}

func NewScheduledDepositService(vaultSvc *VaultService) *ScheduledDepositService {
	return &ScheduledDepositService{vaultSvc: vaultSvc}
}

func (s *ScheduledDepositService) RecordScheduledDeposit(
	ctx context.Context,
	userID, vaultID uuid.UUID,
	amount decimal.Decimal,
	scheduleID uuid.UUID,
) error {
	txHash := fmt.Sprintf("scheduled-%s", scheduleID.String())
	_, err := s.vaultSvc.RecordDeposit(ctx, RecordDepositInput{
		VaultID: vaultID,
		UserID:  userID,
		Amount:  amount,
		TxHash:  txHash,
	})
	return err
}

// GoalProgressService checks whether a savings goal has reached its target.
type GoalProgressService struct {
	goals savingsgoal.Repository
}

func NewGoalProgressService(goals savingsgoal.Repository) *GoalProgressService {
	return &GoalProgressService{goals: goals}
}

func (s *GoalProgressService) IsGoalCompleted(ctx context.Context, goalID, userID uuid.UUID) (bool, string, error) {
	goal, err := s.goals.GetByID(ctx, goalID)
	if err != nil {
		return false, "", err
	}
	if goal.UserID != userID {
		return false, "", savingsgoal.ErrGoalNotFound
	}
	balance, err := s.goals.SumVaultBalance(ctx, userID, goal.Currency)
	if err != nil {
		return false, "", err
	}
	name := goal.Description
	if name == "" {
		name = "your goal"
	}
	return balance.GreaterThanOrEqual(goal.TargetAmount), name, nil
}
