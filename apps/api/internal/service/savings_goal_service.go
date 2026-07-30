package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
)

// VaultReader exposes the single read the savings goal service needs from the
// vault store: looking up a vault to validate ownership/currency at link time
// and to read its live balance when computing goal progress.
type VaultReader interface {
	GetVault(ctx context.Context, id uuid.UUID) (vault.Vault, error)
}

type OutcomeRecorder interface {
	RecordGoalCompletion(ctx context.Context, userID uuid.UUID, ts time.Time) error
}

type SavingsGoalService struct {
	repo           savingsgoal.Repository
	templateRepo   savingsgoal.TemplateRepository
	vaultRepo      VaultReader
	notifier       GoalMilestoneNotifier
	streakRepo     savingsstreak.Repository
	streakNotifier StreakMilestoneNotifier
	outcomeRec     OutcomeRecorder
}

func NewSavingsGoalService(repo savingsgoal.Repository, vaultRepo VaultReader, notifier GoalMilestoneNotifier) *SavingsGoalService {
	if notifier == nil {
		notifier = noopGoalMilestoneNotifier{}
	}
	return &SavingsGoalService{
		repo:           repo,
		templateRepo:   nil,
		vaultRepo:      vaultRepo,
		notifier:       notifier,
		streakNotifier: noopStreakMilestoneNotifier{},
	}
}

// SetOutcomeRecorder attaches an outcome recorder for nudge effectiveness tracking.
func (s *SavingsGoalService) SetOutcomeRecorder(rec OutcomeRecorder) {
	s.outcomeRec = rec
}

// SetTemplateRepository attaches the template repository.
func (s *SavingsGoalService) SetTemplateRepository(repo savingsgoal.TemplateRepository) {
	s.templateRepo = repo
}

// SetStreakRepository attaches the streak persistence layer.
func (s *SavingsGoalService) SetStreakRepository(repo savingsstreak.Repository) {
	s.streakRepo = repo
}

// SetStreakNotifier attaches a notifier for streak badge milestones.
func (s *SavingsGoalService) SetStreakNotifier(n StreakMilestoneNotifier) {
	if n == nil {
		s.streakNotifier = noopStreakMilestoneNotifier{}
		return
	}
	s.streakNotifier = n
}

type CreateSavingsGoalInput struct {
	TargetAmount decimal.Decimal `json:"target_amount"`
	Currency     string          `json:"currency"`
	Deadline     time.Time       `json:"deadline"`
	Description  string          `json:"description"`
	Category     string          `json:"category"`
	Name         string          `json:"name"`
	Emoji        string          `json:"emoji"`
	VaultID      *uuid.UUID      `json:"vault_id,omitempty"`
	// MinContribution/MaxContribution are optional per-contribution limits
	// (#922) enforced at deposit time.
	MinContribution *decimal.Decimal `json:"min_contribution,omitempty"`
	MaxContribution *decimal.Decimal `json:"max_contribution,omitempty"`
}

type UpdateSavingsGoalInput struct {
	TargetAmount *decimal.Decimal `json:"target_amount"`
	Currency     *string          `json:"currency"`
	Deadline     *time.Time       `json:"deadline"`
	Description  *string          `json:"description"`
	Category     *string          `json:"category"`
	Name         *string          `json:"name"`
	Emoji        *string          `json:"emoji"`
	// AutoCompound toggles whether harvested yield is reinvested into the
	// goal's vault position or credited to yield_balance instead.
	AutoCompound *bool `json:"auto_compound"`
	// MinContribution/MaxContribution update a goal's per-contribution
	// limits (#922) when non-nil. Setting either to a zero decimal (rather
	// than leaving it nil) clears that limit.
	MinContribution      *decimal.Decimal `json:"min_contribution,omitempty"`
	MaxContribution      *decimal.Decimal `json:"max_contribution,omitempty"`
	ClearMinContribution bool             `json:"-"`
	ClearMaxContribution bool             `json:"-"`
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
	emoji := strings.TrimSpace(in.Emoji)
	if err := savingsgoal.ValidateEmoji(emoji); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	desc := strings.TrimSpace(in.Description)
	name := strings.TrimSpace(in.Name)
	if err := validateGoalName(name); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if name == "" && desc != "" {
		runes := []rune(desc)
		if len(runes) > 50 {
			name = string(runes[:50])
		} else {
			name = desc
		}
	}
	if err := savingsgoal.ValidateContributionLimits(in.MinContribution, in.MaxContribution); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal := &savingsgoal.SavingsGoal{
		ID:              uuid.New(),
		UserID:          userID,
		TargetAmount:    in.TargetAmount,
		Currency:        savingsgoal.NormalizeCurrency(in.Currency),
		Deadline:        in.Deadline.UTC(),
		Description:     desc,
		Name:            name,
		Emoji:           emoji,
		Category:        category,
		MinContribution: in.MinContribution,
		MaxContribution: in.MaxContribution,
	}
	if in.VaultID != nil {
		if err := s.validateGoalVault(ctx, userID, *in.VaultID, goal.Currency); err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		goal.VaultID = in.VaultID
	}
	if err := s.repo.Create(ctx, goal); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	return s.EnrichProgress(ctx, *goal)
}

type CreateFromTemplateInput struct {
	TemplateID     uuid.UUID
	OverrideAmount *decimal.Decimal
	OverrideMonths *int
	VaultID        *uuid.UUID
}

func (s *SavingsGoalService) ListTemplates(ctx context.Context) ([]savingsgoal.GoalTemplate, error) {
	if s.templateRepo == nil {
		return nil, fmt.Errorf("goal templates not configured")
	}
	return s.templateRepo.List(ctx)
}

func (s *SavingsGoalService) CreateFromTemplate(ctx context.Context, userID uuid.UUID, in CreateFromTemplateInput) (savingsgoal.SavingsGoal, error) {
	if s.templateRepo == nil {
		return savingsgoal.SavingsGoal{}, fmt.Errorf("goal templates not configured")
	}
	template, err := s.templateRepo.GetByID(ctx, in.TemplateID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	amount := template.SuggestedAmount
	if in.OverrideAmount != nil {
		amount = *in.OverrideAmount
	}
	months := template.SuggestedMonths
	if in.OverrideMonths != nil {
		months = *in.OverrideMonths
	}
	deadline := time.Now().UTC().AddDate(0, months, 0)

	createIn := CreateSavingsGoalInput{
		TargetAmount: amount,
		Currency:     template.Currency,
		Deadline:     deadline,
		Description:  template.Description,
		Category:     string(template.Category),
		Name:         template.Name,
		Emoji:        "", // Optional: could map template.Icon to emoji if needed
		VaultID:      in.VaultID,
	}
	return s.Create(ctx, userID, createIn)
}

func (s *SavingsGoalService) Get(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	return s.EnrichProgress(ctx, *goal)
}

func (s *SavingsGoalService) List(ctx context.Context, userID uuid.UUID, category, status string, includeArchived bool) ([]savingsgoal.SavingsGoal, error) {
	filterCategory := ""
	if strings.TrimSpace(category) != "" {
		parsed, err := savingsgoal.ParseCategory(category)
		if err != nil {
			return nil, err
		}
		filterCategory = string(parsed)
	}

	filterStatus, err := savingsgoal.ParseStatusFilter(status)
	if err != nil {
		return nil, err
	}

	goals, err := s.repo.ListByUser(ctx, userID, filterCategory, "")
	if err != nil {
		return nil, err
	}
	out := make([]savingsgoal.SavingsGoal, 0, len(goals))
	for _, g := range goals {
		enriched, err := s.EnrichProgress(ctx, g)
		if err != nil {
			return nil, err
		}
		if filterStatus != "" {
			if enriched.Status != filterStatus {
				continue
			}
		} else if !includeArchived && enriched.Status == savingsgoal.GoalStatusArchived {
			continue
		}
		out = append(out, enriched)
	}
	return out, nil
}

// SavingsGoalListFilter drives ListPaginated: pagination, sort, and search on
// top of the same category/status/archived semantics as List.
type SavingsGoalListFilter struct {
	Page            int
	PerPage         int
	SortField       string
	SortOrder       string
	Category        string
	Status          string
	IncludeArchived bool
	Search          string
}

// ListPaginated is List with pagination, sort, and full-text search applied.
// Status/archived filtering depends on each goal's enriched status, which is
// only known after EnrichProgress runs — so, unlike vault/settlement
// listing, pagination here is applied in Go after enrichment and filtering,
// not pushed down as SQL LIMIT/OFFSET. Goals-per-user is low-cardinality, so
// this is not a performance concern.
func (s *SavingsGoalService) ListPaginated(ctx context.Context, userID uuid.UUID, filter SavingsGoalListFilter) ([]savingsgoal.SavingsGoal, int, error) {
	filterCategory := ""
	if strings.TrimSpace(filter.Category) != "" {
		parsed, err := savingsgoal.ParseCategory(filter.Category)
		if err != nil {
			return nil, 0, err
		}
		filterCategory = string(parsed)
	}

	filterStatus, err := savingsgoal.ParseStatusFilter(filter.Status)
	if err != nil {
		return nil, 0, err
	}

	goals, err := s.repo.ListByUser(ctx, userID, filterCategory, filter.Search)
	if err != nil {
		return nil, 0, err
	}

	out := make([]savingsgoal.SavingsGoal, 0, len(goals))
	for _, g := range goals {
		enriched, err := s.EnrichProgress(ctx, g)
		if err != nil {
			return nil, 0, err
		}
		if filterStatus != "" {
			if enriched.Status != filterStatus {
				continue
			}
		} else if !filter.IncludeArchived && enriched.Status == savingsgoal.GoalStatusArchived {
			continue
		}
		out = append(out, enriched)
	}

	sortSavingsGoals(out, filter.SortField, filter.SortOrder)

	total := len(out)
	page, perPage := filter.Page, filter.PerPage
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = listquery.DefaultPerPage
	}
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return out[start:end], total, nil
}

func sortSavingsGoals(goals []savingsgoal.SavingsGoal, field, order string) {
	desc := order != "asc"
	compare := func(i, j int) int {
		switch field {
		case "target_amount":
			return goals[i].TargetAmount.Cmp(goals[j].TargetAmount)
		case "deadline":
			switch {
			case goals[i].Deadline.Before(goals[j].Deadline):
				return -1
			case goals[i].Deadline.After(goals[j].Deadline):
				return 1
			default:
				return 0
			}
		default:
			switch {
			case goals[i].CreatedAt.Before(goals[j].CreatedAt):
				return -1
			case goals[i].CreatedAt.After(goals[j].CreatedAt):
				return 1
			default:
				return 0
			}
		}
	}
	sort.SliceStable(goals, func(i, j int) bool {
		c := compare(i, j)
		if desc {
			return c > 0
		}
		return c < 0
	})
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
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if err := validateGoalName(name); err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		goal.Name = name
	}
	if in.Emoji != nil {
		e := strings.TrimSpace(*in.Emoji)
		if err := savingsgoal.ValidateEmoji(e); err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		goal.Emoji = e
	}
	if in.AutoCompound != nil {
		goal.AutoCompound = *in.AutoCompound
	}
	if in.ClearMinContribution {
		goal.MinContribution = nil
	} else if in.MinContribution != nil {
		goal.MinContribution = in.MinContribution
	}
	if in.ClearMaxContribution {
		goal.MaxContribution = nil
	} else if in.MaxContribution != nil {
		goal.MaxContribution = in.MaxContribution
	}
	if err := savingsgoal.ValidateContributionLimits(goal.MinContribution, goal.MaxContribution); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	// Deadline is validated above (when changed); only amount/currency here, so
	// other fields of an overdue goal can still be updated.
	if err := validateSavingsGoalInput(goal.TargetAmount, goal.Currency); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if err := s.repo.Update(ctx, goal); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	return s.EnrichProgress(ctx, *goal)
}

// Delete soft-deletes the goal (#924): it stamps deleted_at rather than
// destroying the row, leaving a SavingsGoalRecoveryWindow-long window during
// which Restore can undo it before the scheduled purge job hard-deletes it.
func (s *SavingsGoalService) Delete(ctx context.Context, userID, goalID uuid.UUID) error {
	return s.repo.Delete(ctx, goalID, userID)
}

// Restore undoes a soft delete (#924), provided the goal was deleted less
// than SavingsGoalRecoveryWindow ago. Returns ErrGoalNotFound if the goal
// doesn't exist or isn't owned by userID, ErrGoalNotDeleted if it was never
// deleted, and ErrRecoveryWindowExpired once the window has elapsed (the
// goal may already be gone, or about to be purged).
func (s *SavingsGoalService) Restore(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByIDIncludingDeleted(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if goal.DeletedAt == nil {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotDeleted
	}
	if time.Since(*goal.DeletedAt) > savingsgoal.SavingsGoalRecoveryWindow {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrRecoveryWindowExpired
	}
	if err := s.repo.Restore(ctx, goalID, userID); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal.DeletedAt = nil
	return s.EnrichProgress(ctx, *goal)
}

func (s *SavingsGoalService) ListContributions(ctx context.Context, userID, goalID uuid.UUID, params listquery.PageParams) ([]savingsgoal.GoalContribution, int, string, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return nil, 0, "", err
	}
	if goal.UserID != userID {
		return nil, 0, "", savingsgoal.ErrGoalNotFound
	}
	return s.repo.ListContributions(ctx, goalID, userID, params)
}

func (s *SavingsGoalService) Summary(ctx context.Context, userID uuid.UUID) (savingsgoal.SavingsGoalsSummary, error) {
	goals, err := s.repo.ListByUser(ctx, userID, "", "")
	if err != nil {
		return savingsgoal.SavingsGoalsSummary{}, err
	}

	summary := savingsgoal.SavingsGoalsSummary{GoalCount: len(goals)}

	now := time.Now().UTC()
	for _, goal := range goals {
		enriched, err := s.EnrichProgress(ctx, goal)
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

		// Goal status counts (#733): active excludes archived/paused/completed.
		// Treat "" as active: the postgres repo always normalises to "active" on
		// scan, but in-memory test repos may leave Status unset.
		effectiveStatus := enriched.Status
		if effectiveStatus == "" {
			effectiveStatus = savingsgoal.GoalStatusActive
		}
		switch effectiveStatus {
		case savingsgoal.GoalStatusCompleted:
			summary.CompletedGoalCount++
		case savingsgoal.GoalStatusActive:
			summary.ActiveGoalCount++
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

	// Streak — only computed when a streak repository is wired in.
	if s.streakRepo != nil {
		current, longest, err := s.updateStreak(ctx, userID)
		if err == nil {
			summary.CurrentStreak = current
			summary.LongestStreak = longest
		}
	}

	return summary, nil
}

// validateGoalVault ensures a vault may be linked to a goal: it must exist,
// belong to the authenticated user, and share the goal's currency.
func (s *SavingsGoalService) validateGoalVault(ctx context.Context, userID, vaultID uuid.UUID, currency string) error {
	if s.vaultRepo == nil {
		return fmt.Errorf("%w: vault linking is not available", savingsgoal.ErrInvalidGoal)
	}
	v, err := s.vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		if errors.Is(err, vault.ErrVaultNotFound) {
			return fmt.Errorf("%w: vault not found", savingsgoal.ErrInvalidGoal)
		}
		return err
	}
	if v.UserID != userID {
		return savingsgoal.ErrUnauthorized
	}
	if savingsgoal.NormalizeCurrency(v.Currency) != currency {
		return fmt.Errorf("%w: vault currency (%s) does not match goal currency (%s)", savingsgoal.ErrInvalidGoal, v.Currency, currency)
	}
	return nil
}

func (s *SavingsGoalService) EnrichProgress(ctx context.Context, goal savingsgoal.SavingsGoal) (savingsgoal.SavingsGoal, error) {
	balance, err := s.currentAmount(ctx, goal)
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
	// Treat "" as active (postgres repo always normalises on scan; in-memory repos may not).
	if (goal.Status == savingsgoal.GoalStatusActive || goal.Status == "") &&
		goal.TargetAmount.IsPositive() &&
		balance.GreaterThanOrEqual(goal.TargetAmount) &&
		goal.CompletedAt == nil {
		_ = s.repo.MarkCompleted(ctx, goal.ID, goal.UserID, "")
		now := time.Now().UTC()
		goal.CompletedAt = &now
		goal.Status = savingsgoal.GoalStatusCompleted
		if s.outcomeRec != nil {
			_ = s.outcomeRec.RecordGoalCompletion(ctx, goal.UserID, now)
		}
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

// currentAmount resolves the balance backing a goal. A goal linked to a
// specific vault reflects only that vault's balance; goals created before vault
// linking (vault_id IS NULL) fall back to summing every vault in the goal's
// currency. The same fallback covers a linked vault that has since been
// soft-deleted, so progress never errors out for an orphaned link.
func (s *SavingsGoalService) currentAmount(ctx context.Context, goal savingsgoal.SavingsGoal) (decimal.Decimal, error) {
	if goal.VaultID != nil && s.vaultRepo != nil {
		v, err := s.vaultRepo.GetVault(ctx, *goal.VaultID)
		if err == nil {
			return v.CurrentBalance, nil
		}
		if !errors.Is(err, vault.ErrVaultNotFound) {
			return decimal.Zero, err
		}
	}
	return s.repo.SumVaultBalance(ctx, goal.UserID, goal.Currency)
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
	return s.EnrichProgress(ctx, *goal)
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
	return s.EnrichProgress(ctx, *goal)
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
	if s.outcomeRec != nil {
		_ = s.outcomeRec.RecordGoalCompletion(ctx, goal.UserID, now)
	}
	return s.EnrichProgress(ctx, *goal)
}

// Share generates a unique share token for the goal, enabling read-only public access.
// If the goal already has a token, the existing token is returned unchanged.
func (s *SavingsGoalService) Share(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if goal.ShareToken == nil {
		token := uuid.New()
		if err := s.repo.SetShareToken(ctx, goalID, userID, token); err != nil {
			return savingsgoal.SavingsGoal{}, err
		}
		goal.ShareToken = &token
		goal.IsShared = true
	}
	return s.EnrichProgress(ctx, *goal)
}

// Unshare revokes the share token, making the goal private again.
func (s *SavingsGoalService) Unshare(ctx context.Context, userID, goalID uuid.UUID) error {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return err
	}
	if goal.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	return s.repo.ClearShareToken(ctx, goalID, userID)
}

// GetShared returns a public read-only view of a goal by its share token.
// No user authentication is required.
func (s *SavingsGoalService) GetShared(ctx context.Context, token uuid.UUID) (savingsgoal.SharedGoalView, error) {
	goal, err := s.repo.GetByShareToken(ctx, token)
	if err != nil {
		return savingsgoal.SharedGoalView{}, err
	}
	enriched, err := s.EnrichProgress(ctx, *goal)
	if err != nil {
		return savingsgoal.SharedGoalView{}, err
	}
	displayName := savingsgoal.GoalDisplayName(enriched)
	return savingsgoal.SharedGoalView{
		Name:          displayName,
		Emoji:         enriched.Emoji,
		TargetAmount:  enriched.TargetAmount,
		Currency:      enriched.Currency,
		CurrentAmount: enriched.CurrentAmount,
		ProgressPct:   enriched.ProgressPct,
		Deadline:      enriched.Deadline,
		Category:      enriched.Category,
		Status:        enriched.Status,
	}, nil
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
		return s.EnrichProgress(ctx, *goal)
	}
	if err := s.repo.UpdateStatus(ctx, goalID, userID, savingsgoal.GoalStatusArchived); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal.Status = savingsgoal.GoalStatusArchived
	return s.EnrichProgress(ctx, *goal)
}

// GetAutoCompoundForVault looks up the goal linked to vaultID and reports its
// auto_compound preference. found is false when no goal is linked to the
// vault, in which case callers should fall back to their own default.
// Implements the GoalYieldRouter seam VaultService uses at harvest time.
func (s *SavingsGoalService) GetAutoCompoundForVault(ctx context.Context, vaultID uuid.UUID) (goalID uuid.UUID, autoCompound bool, found bool, err error) {
	goal, err := s.repo.GetByVaultID(ctx, vaultID)
	if err != nil {
		if errors.Is(err, savingsgoal.ErrGoalNotFound) {
			return uuid.Nil, false, false, nil
		}
		return uuid.Nil, false, false, err
	}
	return goal.ID, goal.AutoCompound, true, nil
}

// CreditGoalYieldBalance adds amount to the goal's yield_balance. Called by
// VaultService when a linked goal's auto_compound is false, so harvested
// yield is tracked on the goal instead of being reinvested into the vault.
func (s *SavingsGoalService) CreditGoalYieldBalance(ctx context.Context, goalID uuid.UUID, amount decimal.Decimal) error {
	return s.repo.CreditYieldBalance(ctx, goalID, amount)
}

// Unarchive restores an archived goal to active status (#721).
func (s *SavingsGoalService) Unarchive(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error) {
	goal, err := s.repo.GetByID(ctx, goalID)
	if err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	if goal.UserID != userID {
		return savingsgoal.SavingsGoal{}, savingsgoal.ErrGoalNotFound
	}
	if goal.Status != savingsgoal.GoalStatusArchived {
		return s.EnrichProgress(ctx, *goal)
	}
	if err := s.repo.UpdateStatus(ctx, goalID, userID, savingsgoal.GoalStatusActive); err != nil {
		return savingsgoal.SavingsGoal{}, err
	}
	goal.Status = savingsgoal.GoalStatusActive
	return s.EnrichProgress(ctx, *goal)
}

// isoWeekKey returns an "YYYY-Www" string uniquely identifying the ISO calendar week of t.
func isoWeekKey(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// updateStreak refreshes the user's savings streak based on whether a deposit
// occurred in the current calendar week and returns (current, longest).
func (s *SavingsGoalService) updateStreak(ctx context.Context, userID uuid.UUID) (int, int, error) {
	now := time.Now().UTC()
	currentWeek := isoWeekKey(now)

	// Start of current ISO week (Monday 00:00 UTC).
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7
	}
	weekStart := now.AddDate(0, 0, -(weekday - 1)).Truncate(24 * time.Hour)

	// Any deposit amount > 0 in any supported currency counts.
	var hasDepositThisWeek bool
	for _, currency := range []string{"USDC", "XLM"} {
		total, err := s.repo.SumRecentDeposits(ctx, userID, currency, weekStart)
		if err != nil {
			return 0, 0, err
		}
		if total.IsPositive() {
			hasDepositThisWeek = true
			break
		}
	}

	streak, err := s.streakRepo.Get(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	if streak == nil {
		streak = &savingsstreak.SavingsStreak{UserID: userID}
	}

	// Determine previous week key for consecutive-week detection.
	prevWeekStart := weekStart.AddDate(0, 0, -7)
	prevWeek := isoWeekKey(prevWeekStart)

	if hasDepositThisWeek && streak.LastDepositWeek != currentWeek {
		if streak.LastDepositWeek == prevWeek {
			streak.CurrentStreak++
		} else {
			// Gap detected — restart streak.
			streak.CurrentStreak = 1
		}
		streak.LastDepositWeek = currentWeek
		if streak.CurrentStreak > streak.LongestStreak {
			streak.LongestStreak = streak.CurrentStreak
		}
	} else if !hasDepositThisWeek && streak.LastDepositWeek != "" && streak.LastDepositWeek < prevWeek {
		// A full calendar week with no deposit: reset.
		streak.CurrentStreak = 0
	}

	if err := s.streakRepo.Upsert(ctx, streak); err != nil {
		return 0, 0, err
	}

	// Fire milestone notification asynchronously if applicable.
	if hasDepositThisWeek && streak.IsNewMilestone(streak.CurrentStreak) {
		milestone := streak.CurrentStreak
		streak.NotifiedMilestones = append(streak.NotifiedMilestones, milestone)
		_ = s.streakRepo.Upsert(ctx, streak)
		uid := userID
		go func() {
			s.streakNotifier.SendStreakMilestone(context.Background(), uid, milestone)
		}()
	}

	return streak.CurrentStreak, streak.LongestStreak, nil
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

// DepositSplitAllocation is a single goal allocation in a multi-goal deposit (#719).
type DepositSplitAllocation struct {
	GoalID     uuid.UUID
	Amount     decimal.Decimal // set when splitting by fixed amount
	Percentage decimal.Decimal // set when splitting by percentage; mutually exclusive with Amount
}

// DepositSplitInput is the input for a multi-goal deposit (#719).
type DepositSplitInput struct {
	TotalAmount decimal.Decimal
	Currency    string
	Allocations []DepositSplitAllocation
}

// GoalDepositResult is the per-goal result returned by DepositSplit (#719).
type GoalDepositResult struct {
	GoalID        uuid.UUID       `json:"goal_id"`
	Deposited     decimal.Decimal `json:"deposited"`
	CurrentAmount decimal.Decimal `json:"current_amount"`
	ProgressPct   float64         `json:"progress_pct"`
}

// SplitDepositResult is the response from DepositSplit (#719).
type SplitDepositResult struct {
	TotalDeposited decimal.Decimal     `json:"total_deposited"`
	Currency       string              `json:"currency"`
	Goals          []GoalDepositResult `json:"goals"`
}

// DepositSplit routes a single deposit across multiple active savings goals (#719).
func (s *SavingsGoalService) DepositSplit(ctx context.Context, userID uuid.UUID, in DepositSplitInput) (SplitDepositResult, error) {
	currency := savingsgoal.NormalizeCurrency(in.Currency)
	if !savingsgoal.IsSupportedCurrency(currency) {
		return SplitDepositResult{}, fmt.Errorf("%w: unsupported currency %q", savingsgoal.ErrInvalidGoal, in.Currency)
	}
	if !in.TotalAmount.IsPositive() {
		return SplitDepositResult{}, fmt.Errorf("%w: total_amount must be positive", savingsgoal.ErrInvalidGoal)
	}
	if len(in.Allocations) == 0 {
		return SplitDepositResult{}, fmt.Errorf("%w: at least one allocation is required", savingsgoal.ErrInvalidGoal)
	}

	// Detect mode: all amount or all percentage.
	allAmount := true
	allPct := true
	for _, a := range in.Allocations {
		if a.Amount.IsZero() {
			allAmount = false
		}
		if a.Percentage.IsZero() {
			allPct = false
		}
	}
	if !allAmount && !allPct {
		return SplitDepositResult{}, fmt.Errorf("%w: each allocation must set either amount or percentage, not both or neither", savingsgoal.ErrInvalidGoal)
	}
	if allAmount && allPct {
		return SplitDepositResult{}, fmt.Errorf("%w: each allocation must set either amount or percentage, not both", savingsgoal.ErrInvalidGoal)
	}

	// Compute per-goal amounts.
	amounts := make([]decimal.Decimal, len(in.Allocations))
	if allPct {
		totalPct := decimal.Zero
		for _, a := range in.Allocations {
			if !a.Percentage.IsPositive() {
				return SplitDepositResult{}, fmt.Errorf("%w: percentage must be positive", savingsgoal.ErrInvalidGoal)
			}
			totalPct = totalPct.Add(a.Percentage)
		}
		if !totalPct.Equal(decimal.NewFromInt(100)) {
			return SplitDepositResult{}, fmt.Errorf("%w: percentages must sum to 100 (got %s)", savingsgoal.ErrInvalidGoal, totalPct.String())
		}
		for i, a := range in.Allocations {
			amounts[i] = in.TotalAmount.Mul(a.Percentage).Div(decimal.NewFromInt(100)).Round(8)
		}
	} else {
		totalAmt := decimal.Zero
		for i, a := range in.Allocations {
			if !a.Amount.IsPositive() {
				return SplitDepositResult{}, fmt.Errorf("%w: allocation amount must be positive", savingsgoal.ErrInvalidGoal)
			}
			amounts[i] = a.Amount
			totalAmt = totalAmt.Add(a.Amount)
		}
		if !totalAmt.Equal(in.TotalAmount) {
			return SplitDepositResult{}, fmt.Errorf("%w: allocation amounts sum to %s but total_amount is %s", savingsgoal.ErrInvalidGoal, totalAmt.String(), in.TotalAmount.String())
		}
	}

	// Validate goal IDs are unique.
	seen := make(map[uuid.UUID]struct{}, len(in.Allocations))
	for _, a := range in.Allocations {
		if _, dup := seen[a.GoalID]; dup {
			return SplitDepositResult{}, fmt.Errorf("%w: duplicate goal_id %s", savingsgoal.ErrInvalidGoal, a.GoalID)
		}
		seen[a.GoalID] = struct{}{}
	}

	// Validate each goal exists, belongs to user, is active, and matches currency.
	goals := make([]*savingsgoal.SavingsGoal, len(in.Allocations))
	for i, a := range in.Allocations {
		g, err := s.repo.GetByID(ctx, a.GoalID)
		if err != nil {
			return SplitDepositResult{}, err
		}
		if g.UserID != userID {
			return SplitDepositResult{}, savingsgoal.ErrGoalNotFound
		}
		if savingsgoal.NormalizeCurrency(g.Currency) != currency {
			return SplitDepositResult{}, fmt.Errorf("%w: goal %s currency %s does not match deposit currency %s", savingsgoal.ErrInvalidGoal, a.GoalID, g.Currency, currency)
		}
		switch g.Status {
		case savingsgoal.GoalStatusPaused:
			return SplitDepositResult{}, fmt.Errorf("%w: goal %s is paused", savingsgoal.ErrGoalPaused, a.GoalID)
		case savingsgoal.GoalStatusCompleted:
			return SplitDepositResult{}, fmt.Errorf("%w: goal %s is already completed", savingsgoal.ErrGoalCompleted, a.GoalID)
		case savingsgoal.GoalStatusArchived:
			return SplitDepositResult{}, fmt.Errorf("%w: goal %s is archived", savingsgoal.ErrGoalArchived, a.GoalID)
		}
		goals[i] = g
	}

	// Enforce each goal's optional per-contribution limits (#922) against the
	// amount it is about to receive.
	for i, g := range goals {
		if err := savingsgoal.ValidateContributionAmount(amounts[i], g.MinContribution, g.MaxContribution); err != nil {
			return SplitDepositResult{}, fmt.Errorf("goal %s: %w", g.ID, err)
		}
	}

	// Build deposit records and persist atomically.
	deposits := make([]savingsgoal.GoalDeposit, len(in.Allocations))
	for i, a := range in.Allocations {
		deposits[i] = savingsgoal.GoalDeposit{
			ID:       uuid.New(),
			GoalID:   a.GoalID,
			UserID:   userID,
			Amount:   amounts[i],
			Currency: currency,
		}
	}
	if err := s.repo.RecordGoalDeposits(ctx, deposits); err != nil {
		return SplitDepositResult{}, err
	}

	// Build per-goal results.
	results := make([]GoalDepositResult, len(in.Allocations))
	for i, g := range goals {
		current, err := s.repo.SumGoalDeposits(ctx, g.ID)
		if err != nil {
			return SplitDepositResult{}, err
		}
		var pct float64
		if g.TargetAmount.IsPositive() {
			p, _ := current.Div(g.TargetAmount).Mul(decimal.NewFromInt(100)).Float64()
			if p > 100 {
				p = 100
			}
			pct = p
		}
		results[i] = GoalDepositResult{
			GoalID:        g.ID,
			Deposited:     amounts[i],
			CurrentAmount: current,
			ProgressPct:   pct,
		}
	}

	return SplitDepositResult{
		TotalDeposited: in.TotalAmount,
		Currency:       currency,
		Goals:          results,
	}, nil
}

// MinDeadlineLeadTime is the minimum distance into the future a new goal's
// deadline must be. A deadline only an hour away is technically valid but
// meaningless for a savings goal, so creation requires at least 24h (#686).
const MinDeadlineLeadTime = 24 * time.Hour

// MaxGoalNameLength caps the goal name to the savings_goals.name column width
// (VARCHAR(100)); validating here returns a 400 instead of a DB error (#681).
const MaxGoalNameLength = 100

func validateGoalName(name string) error {
	if len([]rune(name)) > MaxGoalNameLength {
		return fmt.Errorf("%w: name must be at most %d characters", savingsgoal.ErrInvalidGoal, MaxGoalNameLength)
	}
	return nil
}

func validateSavingsGoalInput(target decimal.Decimal, currency string) error {
	if !target.IsPositive() {
		return fmt.Errorf("%w: target_amount must be greater than zero", savingsgoal.ErrInvalidAmount)
	}
	if target.LessThan(savingsgoal.MinTargetAmount) {
		return fmt.Errorf("%w: target_amount must be at least %s", savingsgoal.ErrInvalidAmount, savingsgoal.MinTargetAmount)
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
