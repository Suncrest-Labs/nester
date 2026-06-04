package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

type SavingsGoalService struct {
	repo savingsgoal.Repository
}

func NewSavingsGoalService(repo savingsgoal.Repository) *SavingsGoalService {
	return &SavingsGoalService{repo: repo}
}

type CreateSavingsGoalInput struct {
	TargetAmount decimal.Decimal `json:"target_amount"`
	Currency     string          `json:"currency"`
	Deadline     time.Time       `json:"deadline"`
	Description  string          `json:"description"`
}

type UpdateSavingsGoalInput struct {
	TargetAmount *decimal.Decimal `json:"target_amount"`
	Currency     *string          `json:"currency"`
	Deadline     *time.Time       `json:"deadline"`
	Description  *string          `json:"description"`
}

func (s *SavingsGoalService) Create(ctx context.Context, userID uuid.UUID, in CreateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	if err := validateSavingsGoalInput(in.TargetAmount, in.Currency, in.Deadline); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal := &savingsgoal.SavingsGoal{
		ID:           uuid.New(),
		UserID:       userID,
		TargetAmount: in.TargetAmount,
		Currency:     strings.ToUpper(strings.TrimSpace(in.Currency)),
		Deadline:     in.Deadline.UTC(),
		Description:  strings.TrimSpace(in.Description),
	}
	if err := s.repo.Create(ctx, goal); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	return s.enrichProgress(ctx, *goal)
}

func (s *SavingsGoalService) Get(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	return s.enrichProgress(ctx, *goal)
}

func (s *SavingsGoalService) List(ctx context.Context, userID uuid.UUID) ([]savingsgoal.SavingsGoal, error) {
	goals, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]savingsgoal.SavingsGoal, 0, len(goals))
	for _, g := range goals {
		enriched, err := s.enrichProgress(ctx, g)
		if err != nil {
			return nil, err
		}
		out = append(out, enriched)
	}
	return out, nil
}

func (s *SavingsGoalService) Update(ctx context.Context, userID, goalID uuid.UUID, in UpdateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if in.TargetAmount != nil {
		goal.TargetAmount = *in.TargetAmount
	}
	if in.Currency != nil {
		goal.Currency = strings.ToUpper(strings.TrimSpace(*in.Currency))
	}
	if in.Deadline != nil {
		goal.Deadline = in.Deadline.UTC()
	}
	if in.Description != nil {
		goal.Description = strings.TrimSpace(*in.Description)
	}
	if err := validateSavingsGoalInput(goal.TargetAmount, goal.Currency, goal.Deadline); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if err := s.repo.Update(ctx, goal); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	return s.enrichProgress(ctx, *goal)
}

func (s *SavingsGoalService) Delete(ctx context.Context, userID, goalID uuid.UUID) error {
	return s.repo.Delete(ctx, goalID, userID)
}

func (s *SavingsGoalService) enrichProgress(ctx context.Context, goal savingsgoal.SavingsGoal) (savingsgoal.SavingsGoal, error) {
	balance, err := s.repo.SumVaultBalance(ctx, goal.UserID, goal.Currency)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal.CurrentAmount = balance
	if goal.TargetAmount.IsPositive() {
		pct, _ := balance.Div(goal.TargetAmount).Mul(decimal.NewFromInt(100)).Float64()
		if pct > 100 {
			pct = 100
		}
		if pct < 0 {
			pct = 0
		}
		goal.ProgressPct = pct
	}
	return goal, nil
}

func validateSavingsGoalInput(target decimal.Decimal, currency string, deadline time.Time) error {
	if !target.IsPositive() {
		return fmt.Errorf("%w: target_amount must be positive", savingsgoal.ErrInvalidGoal)
	}
	currency = strings.TrimSpace(currency)
	if currency == "" {
		return fmt.Errorf("%w: currency is required", savingsgoal.ErrInvalidGoal)
	}
	if deadline.Before(time.Now().UTC()) {
		return fmt.Errorf("%w: deadline must be in the future", savingsgoal.ErrInvalidGoal)
	}
	return nil
}
