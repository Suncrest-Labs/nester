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
	repo     savingsgoal.Repository
	notifier GoalMilestoneNotifier
}

func NewSavingsGoalService(repo savingsgoal.Repository, notifier GoalMilestoneNotifier) *SavingsGoalService {
	if notifier == nil {
		notifier = noopGoalMilestoneNotifier{}
	}
	return &SavingsGoalService{repo: repo, notifier: notifier}
}

type CreateSavingsGoalInput struct {
	TargetAmount decimal.Decimal `json:"target_amount"`
	Currency     string          `json:"currency"`
	Deadline     time.Time       `json:"deadline"`
	Description  string          `json:"description"`
	Category     string          `json:"category"`
}

type UpdateSavingsGoalInput struct {
	TargetAmount *decimal.Decimal `json:"target_amount"`
	Currency     *string          `json:"currency"`
	Deadline     *time.Time       `json:"deadline"`
	Description  *string          `json:"description"`
	Category     *string          `json:"category"`
}

func (s *SavingsGoalService) Create(ctx context.Context, userID uuid.UUID, in CreateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	if err := validateSavingsGoalInput(in.TargetAmount, in.Currency); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if err := validateCreateDeadline(in.Deadline.UTC()); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	category, err := resolveCategory(in.Category, true)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal := &savingsgoal.SavingsGoal{
		ID:           uuid.New(),
		UserID:       userID,
		TargetAmount: in.TargetAmount,
		Currency:     savingsgoal.NormalizeCurrency(in.Currency),
		Deadline:     in.Deadline.UTC(),
		Description:  strings.TrimSpace(in.Description),
		Category:     category,
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

func (s *SavingsGoalService) List(ctx context.Context, userID uuid.UUID, category string) ([]savingsgoal.SavingsGoal, error) {
	filterCategory := ""
	if strings.TrimSpace(category) != "" {
		parsed, err := savingsgoal.ParseCategory(category)
		if err != nil {
			return nil, err
		}
		filterCategory = string(parsed)
	}

	goals, err := s.repo.ListByUser(ctx, userID, filterCategory)
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
		goal.Currency = savingsgoal.NormalizeCurrency(*in.Currency)
	}
	if in.Deadline != nil {
		// Changing the deadline of a completed goal is not allowed (#684/#686).
		balance, err := s.repo.SumVaultBalance(ctx, goal.UserID, goal.Currency)
		if err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		if goal.TargetAmount.IsPositive() && balance.GreaterThanOrEqual(goal.TargetAmount) {
			return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: cannot change deadline of a completed goal", savingsgoal.ErrGoalCompleted)
		}
		// The new deadline must be in the future. Extending an already-overdue
		// (but not completed) goal to a future date is a legitimate use case,
		// so the only rule on update is "must be in the future".
		newDeadline := in.Deadline.UTC()
		if !newDeadline.After(time.Now().UTC()) {
			return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: deadline must be in the future", savingsgoal.ErrInvalidGoal)
		}
		goal.Deadline = newDeadline
	}
	if in.Description != nil {
		goal.Description = strings.TrimSpace(*in.Description)
	}
	if in.Category != nil {
		category, err := resolveCategory(*in.Category, false)
		if err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		goal.Category = category
	}
	// Deadline is validated above (when changed); only amount/currency here, so
	// other fields of an overdue goal can still be updated.
	if err := validateSavingsGoalInput(goal.TargetAmount, goal.Currency); err != nil {
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

func (s *SavingsGoalService) Summary(ctx context.Context, userID uuid.UUID) (savingsgoal.SavingsGoalsSummary, error) {
	goals, err := s.repo.ListByUser(ctx, userID, "")
	if err != nil {
		return savingsgoal.SavingsGoalsSummary{}, err
	}

	summary := savingsgoal.SavingsGoalsSummary{GoalCount: len(goals)}

	now := time.Now().UTC()
	for _, goal := range goals {
		enriched, err := s.enrichProgress(ctx, goal)
		if err != nil {
			return savingsgoal.SavingsGoalsSummary{}, err
		}
		switch savingsgoal.NormalizeCurrency(enriched.Currency) {
		case savingsgoal.CurrencyUSDC:
			summary.TotalTargetUSDC = summary.TotalTargetUSDC.Add(enriched.TargetAmount)
			summary.TotalSavedUSDC = summary.TotalSavedUSDC.Add(enriched.CurrentAmount)
		case savingsgoal.CurrencyXLM:
			summary.TotalTargetXLM = summary.TotalTargetXLM.Add(enriched.TargetAmount)
			summary.TotalSavedXLM = summary.TotalSavedXLM.Add(enriched.CurrentAmount)
		}

		// Goal status counts + nearest upcoming deadline across active goals (#683).
		completed := enriched.TargetAmount.IsPositive() &&
			enriched.CurrentAmount.GreaterThanOrEqual(enriched.TargetAmount)
		if completed {
			summary.CompletedGoals++
		} else {
			summary.ActiveGoals++
			if enriched.Deadline.After(now) &&
				(summary.NextDeadline == nil || enriched.Deadline.Before(*summary.NextDeadline)) {
				d := enriched.Deadline
				summary.NextDeadline = &d
			}
		}
	}

	// Overall progress is USDC-denominated, capped at 100 (#683).
	if summary.TotalTargetUSDC.IsPositive() {
		pct, _ := summary.TotalSavedUSDC.Div(summary.TotalTargetUSDC).
			Mul(decimal.NewFromInt(100)).Float64()
		if pct > 100 {
			pct = 100
		}
		if pct < 0 {
			pct = 0
		}
		summary.OverallProgressPct = pct
	}

	return summary, nil
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

	newMilestones := savingsgoal.DetectNewMilestones(goal.ProgressPct, goal.NotifiedMilestones)
	if len(newMilestones) > 0 {
		goal.NotifiedMilestones = append(append([]int(nil), goal.NotifiedMilestones...), newMilestones...)
		if err := s.repo.UpdateMilestones(ctx, goal.ID, goal.NotifiedMilestones); err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		s.notifyMilestonesAsync(goal, newMilestones)
	}

	return goal, nil
}

func (s *SavingsGoalService) notifyMilestonesAsync(goal savingsgoal.SavingsGoal, milestones []int) {
	for _, milestone := range milestones {
		m := milestone
		goalCopy := goal
		go func() {
			s.notifier.SendGoalMilestone(context.Background(), goalCopy.UserID, goalCopy, m)
		}()
	}
}

func resolveCategory(value string, defaultIfEmpty bool) (savingsgoal.GoalCategory, error) {
	if strings.TrimSpace(value) == "" {
		if defaultIfEmpty {
			return savingsgoal.CategoryOther, nil
		}
		return "", fmt.Errorf("%w: invalid category", savingsgoal.ErrInvalidGoal)
	}
	return savingsgoal.ParseCategory(value)
}

// MinDeadlineLeadTime is the minimum distance into the future a new goal's
// deadline must be. A deadline only an hour away is technically valid but
// meaningless for a savings goal, so creation requires at least 24h (#686).
const MinDeadlineLeadTime = 24 * time.Hour

func validateSavingsGoalInput(target decimal.Decimal, currency string) error {
	if !target.IsPositive() {
		return fmt.Errorf("%w: target_amount must be positive", savingsgoal.ErrInvalidGoal)
	}
	currency = strings.TrimSpace(currency)
	if currency == "" {
		return fmt.Errorf("%w: currency is required", savingsgoal.ErrInvalidGoal)
	}
	normalized := savingsgoal.NormalizeCurrency(currency)
	if !savingsgoal.IsSupportedCurrency(normalized) {
		return fmt.Errorf("%w: unsupported currency %q (supported: USDC, XLM)", savingsgoal.ErrInvalidGoal, currency)
	}
	return nil
}

// validateCreateDeadline enforces that a new goal's deadline is at least
// MinDeadlineLeadTime in the future.
func validateCreateDeadline(deadline time.Time) error {
	if !deadline.After(time.Now().UTC()) {
		return fmt.Errorf("%w: deadline must be in the future", savingsgoal.ErrInvalidGoal)
	}
	if deadline.Before(time.Now().UTC().Add(MinDeadlineLeadTime)) {
		return fmt.Errorf("%w: deadline must be at least 24 hours in the future", savingsgoal.ErrInvalidGoal)
	}
	return nil
}
