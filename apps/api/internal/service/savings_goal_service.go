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
	Status       *string          `json:"status"`
}

func (s *SavingsGoalService) Create(ctx context.Context, userID uuid.UUID, in CreateSavingsGoalInput) (savingsgoal.SavingsGoal, error) {
	if err := validateSavingsGoalInput(in.TargetAmount, in.Currency, in.Deadline); err != nil {
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
		Status:       savingsgoal.GoalStatusActive,
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

	var filterStatus savingsgoal.GoalStatus
	if strings.TrimSpace(status) != "" {
		parsed, err := savingsgoal.ParseStatus(status)
		if err != nil {
			return nil, err
		}
		filterStatus = parsed
	}

	goals, err := s.repo.ListByUser(ctx, userID, filterCategory)
	if err != nil {
		return nil, err
	}
	out := make([]savingsgoal.SavingsGoal, 0, len(goals))
	for _, g := range goals {
		// enrichProgress may auto-complete the goal, so filter on the
		// post-enrichment status to reflect freshly completed goals (#684).
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

	// A completed goal is immutable except for archiving it (#684).
	if goal.Status == savingsgoal.GoalStatusCompleted {
		archiving := in.Status != nil &&
			savingsgoal.GoalStatus(strings.ToLower(strings.TrimSpace(*in.Status))) == savingsgoal.GoalStatusArchived
		otherEdits := in.TargetAmount != nil || in.Deadline != nil ||
			in.Description != nil || in.Category != nil || in.Currency != nil
		if !archiving || otherEdits {
			return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalCompleted
		}
		goal.Status = savingsgoal.GoalStatusArchived
		if err := s.repo.Update(ctx, goal); err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		return s.enrichProgress(ctx, *goal)
	}

	if in.Status != nil {
		parsed, err := savingsgoal.ParseStatus(*in.Status)
		if err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		// Completion is automatic; callers may only archive (or keep active).
		if parsed == savingsgoal.GoalStatusCompleted {
			return savingsgoal.SavingsGoal{}, fmt.Errorf("%w: cannot set status to completed manually", savingsgoal.ErrInvalidGoal)
		}
		goal.Status = parsed
	}
	if in.TargetAmount != nil {
		goal.TargetAmount = *in.TargetAmount
	}
	if in.Currency != nil {
		goal.Currency = savingsgoal.NormalizeCurrency(*in.Currency)
	}
	if in.Deadline != nil {
		goal.Deadline = in.Deadline.UTC()
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

func (s *SavingsGoalService) Summary(ctx context.Context, userID uuid.UUID) (savingsgoal.SavingsGoalsSummary, error) {
	goals, err := s.repo.ListByUser(ctx, userID, "")
	if err != nil {
		return savingsgoal.SavingsGoalsSummary{}, err
	}

	summary := savingsgoal.SavingsGoalsSummary{GoalCount: len(goals)}

	for _, goal := range goals {
		enriched, err := s.enrichProgress(ctx, goal)
		if err != nil {
			return savingsgoal.SavingsGoalsSummary{}, err
		}
		switch enriched.Currency {
		case savingsgoal.CurrencyUSDC:
			summary.TotalTargetUSDC = summary.TotalTargetUSDC.Add(enriched.TargetAmount)
			summary.TotalSavedUSDC = summary.TotalSavedUSDC.Add(enriched.CurrentAmount)
		case savingsgoal.CurrencyXLM:
			summary.TotalTargetXLM = summary.TotalTargetXLM.Add(enriched.TargetAmount)
			summary.TotalSavedXLM = summary.TotalSavedXLM.Add(enriched.CurrentAmount)
		}
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

	// Auto-complete: once a goal reaches its target, transition it to
	// completed and stamp completed_at, persisting the change (#684).
	if goal.ProgressPct >= 100 && goal.Status == savingsgoal.GoalStatusActive {
		goal.Status = savingsgoal.GoalStatusCompleted
		now := time.Now().UTC()
		goal.CompletedAt = &now
		if err := s.repo.Update(ctx, &goal); err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		// future: fire completion notification here.
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

func validateSavingsGoalInput(target decimal.Decimal, currency string, deadline time.Time) error {
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
	if deadline.Before(time.Now().UTC()) {
		return fmt.Errorf("%w: deadline must be in the future", savingsgoal.ErrInvalidGoal)
	}
	return nil
}
