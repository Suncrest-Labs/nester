package service

import (
	"context"
	"fmt"
	"time"

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

// RecordScheduledDeposit records one occurrence of a recurring savings
// schedule against the vault ledger. occurrenceAt is the due timestamp of
// THIS occurrence (the schedule's NextRunAt when it was picked up, not
// "now") and is folded into the transaction hash so each occurrence of a
// recurring schedule gets its own hash.
//
// Before this fix, txHash was built from scheduleID alone
// ("scheduled-<scheduleID>"), which is constant across every occurrence of
// the same schedule. vault_transactions.transaction_hash has a UNIQUE
// index and RecordDeposit does a bare INSERT with no ON CONFLICT, so only
// the FIRST-ever occurrence of any schedule could ever be recorded — every
// later occurrence hit the unique-constraint violation, logged an error,
// and (since UpdateAfterRun never ran on failure) retried and failed
// forever (#846).
func (s *ScheduledDepositService) RecordScheduledDeposit(
	ctx context.Context,
	userID, vaultID uuid.UUID,
	amount decimal.Decimal,
	scheduleID uuid.UUID,
	occurrenceAt time.Time,
) error {
	txHash := fmt.Sprintf("scheduled-%s-%d", scheduleID, occurrenceAt.UTC().Unix())
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
	name := goal.Description
	if name == "" {
		name = "your goal"
	}
	if goal.Status == savingsgoal.GoalStatusArchived || goal.Status == savingsgoal.GoalStatusCompleted {
		return true, name, nil
	}
	balance, err := s.goals.SumVaultBalance(ctx, userID, goal.Currency)
	if err != nil {
		return false, "", err
	}
	return balance.GreaterThanOrEqual(goal.TargetAmount), name, nil
}

func (s *GoalProgressService) IsGoalPausedOrArchived(ctx context.Context, goalID, userID uuid.UUID) (bool, error) {
	goal, err := s.goals.GetByID(ctx, goalID)
	if err != nil {
		return false, err
	}
	if goal.UserID != userID {
		return false, savingsgoal.ErrGoalNotFound
	}
	return goal.Status == savingsgoal.GoalStatusPaused || goal.Status == savingsgoal.GoalStatusArchived, nil
}
