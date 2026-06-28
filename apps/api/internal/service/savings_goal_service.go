package service

import (
	"context"
	"fmt"
	"math"
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

func (s *SavingsGoalService) List(ctx context.Context, userID uuid.UUID, category, status string) ([]savingsgoal.SavingsGoal, error) {
	filterCategory := ""
	if strings.TrimSpace(category) != "" {
		parsed, err := savingsgoal.ParseCategory(category)
		if err != nil {
			return nil, err
		}
		filterCategory = string(parsed)
	}

	// Status is filtered after enrichment so the dynamic auto-complete (#716) is
	// reflected: a stored-active goal that has reached its target reads as
	// "completed" here, matching ?status=completed and excluded by ?status=active.
	filterStatus, err := savingsgoal.ParseStatusFilter(status)
	if err != nil {
		return nil, err
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
		if filterStatus != "" && enriched.Status != filterStatus {
			continue
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

	// Auto-complete when target is reached and not already marked complete (#716).
	if goal.Status == savingsgoal.GoalStatusActive &&
		goal.TargetAmount.IsPositive() &&
		balance.GreaterThanOrEqual(goal.TargetAmount) &&
		goal.CompletedAt == nil {
		_ = s.repo.MarkCompleted(ctx, goal.ID, goal.UserID, "")
		now := time.Now().UTC()
		goal.CompletedAt = &now
		goal.Status = savingsgoal.GoalStatusCompleted
	}

	newMilestones := savingsgoal.DetectNewMilestones(goal.ProgressPct, goal.NotifiedMilestones)
	if len(newMilestones) > 0 {
		goal.NotifiedMilestones = append(append([]int(nil), goal.NotifiedMilestones...), newMilestones...)
		if err := s.repo.UpdateMilestones(ctx, goal.ID, goal.NotifiedMilestones); err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		s.notifyMilestonesAsync(goal, newMilestones)
	}

	// Velocity stats (#714): compute from last 30 days of deposits.
	since := time.Now().UTC().Add(-30 * 24 * time.Hour)
	deposited30d, err := s.repo.SumRecentDeposits(ctx, goal.UserID, goal.Currency, since)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	weeksInWindow := decimal.NewFromFloat(30.0 / 7.0)
	if weeksInWindow.IsPositive() {
		goal.AvgWeeklyDeposit = deposited30d.Div(weeksInWindow).Round(8)
	}
	remaining := goal.TargetAmount.Sub(balance)
	if goal.AvgWeeklyDeposit.IsPositive() && remaining.IsPositive() {
		dailyRate := goal.AvgWeeklyDeposit.Div(decimal.NewFromInt(7))
		days := int(math.Ceil(remaining.Div(dailyRate).InexactFloat64()))
		goal.ProjectedDaysToComplete = &days
		daysUntilDeadline := int(math.Ceil(time.Until(goal.Deadline).Hours() / 24))
		goal.OnTrack = days <= daysUntilDeadline
	} else if remaining.IsZero() || remaining.IsNegative() {
		goal.OnTrack = true
	}

	return goal, nil
}

// Pause suspends deposits and notifications for a goal (#718).
func (s *SavingsGoalService) Pause(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if goal.Status == savingsgoal.GoalStatusCompleted {
		return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: cannot pause a completed goal", savingsgoal.ErrGoalCompleted)
	}
	if goal.Status == savingsgoal.GoalStatusPaused {
		return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: goal is already paused", savingsgoal.ErrGoalPaused)
	}
	if err := s.repo.UpdateStatus(ctx, goalID, userID, savingsgoal.GoalStatusPaused); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal.Status = savingsgoal.GoalStatusPaused
	return s.enrichProgress(ctx, *goal)
}

// Resume reactivates a paused goal (#718).
func (s *SavingsGoalService) Resume(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if goal.Status != savingsgoal.GoalStatusPaused {
		return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: goal is not paused", savingsgoal.ErrGoalPaused)
	}
	if err := s.repo.UpdateStatus(ctx, goalID, userID, savingsgoal.GoalStatusActive); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal.Status = savingsgoal.GoalStatusActive
	return s.enrichProgress(ctx, *goal)
}

// Complete explicitly marks a goal as completed with a disposition action (#716).
func (s *SavingsGoalService) Complete(ctx context.Context, userID, goalID uuid.UUID, action string) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if goal.Status == savingsgoal.GoalStatusCompleted {
		return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: goal is already completed", savingsgoal.ErrGoalCompleted)
	}
	if action != "reinvest" && action != "withdraw" {
		return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: action must be 'reinvest' or 'withdraw'", savingsgoal.ErrInvalidGoal)
	}
	balance, err := s.repo.SumVaultBalance(ctx, goal.UserID, goal.Currency)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.TargetAmount.IsPositive() && balance.LessThan(goal.TargetAmount) {
		return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: target not yet reached", savingsgoal.ErrInvalidGoal)
	}
	if err := s.repo.MarkCompleted(ctx, goalID, userID, action); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal.Status = savingsgoal.GoalStatusCompleted
	goal.CompletionAction = action
	now := time.Now().UTC()
	goal.CompletedAt = &now
	return s.enrichProgress(ctx, *goal)
}

// Archive moves a goal into the terminal "archived" state, hiding it without
// deleting it. This is the only mutation allowed on a completed goal (#684);
// already-archived goals are returned unchanged.
func (s *SavingsGoalService) Archive(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if goal.Status == savingsgoal.GoalStatusArchived {
		return s.enrichProgress(ctx, *goal)
	}
	if err := s.repo.UpdateStatus(ctx, goalID, userID, savingsgoal.GoalStatusArchived); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal.Status = savingsgoal.GoalStatusArchived
	return s.enrichProgress(ctx, *goal)
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
