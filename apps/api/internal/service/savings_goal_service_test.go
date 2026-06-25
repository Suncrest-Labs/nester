package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

type memorySavingsGoalRepo struct {
	goals    map[uuid.UUID]savingsgoal.SavingsGoal
	balances map[string]decimal.Decimal
}

func newMemorySavingsGoalRepo() *memorySavingsGoalRepo {
	return &memorySavingsGoalRepo{
		goals:    make(map[uuid.UUID]savingsgoal.SavingsGoal),
		balances: make(map[string]decimal.Decimal),
	}
}

func balanceKey(userID uuid.UUID, currency string) string {
	return userID.String() + ":" + savingsgoal.NormalizeCurrency(currency)
}

func (m *memorySavingsGoalRepo) setBalance(userID uuid.UUID, currency string, amount decimal.Decimal) {
	m.balances[balanceKey(userID, currency)] = amount
}

func (m *memorySavingsGoalRepo) Create(_ context.Context, goal *savingsgoal.SavingsGoal) error {
	now := time.Now().UTC()
	goal.CreatedAt = now
	goal.UpdatedAt = now
	m.goals[goal.ID] = *goal
	return nil
}

func (m *memorySavingsGoalRepo) ListByUser(_ context.Context, userID uuid.UUID, category string) ([]savingsgoal.SavingsGoal, error) {
	var out []savingsgoal.SavingsGoal
	for _, g := range m.goals {
		if g.UserID != userID {
			continue
		}
		if category != "" && string(g.Category) != category {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (m *memorySavingsGoalRepo) GetByID(_ context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[id]
	if !ok {
		return nil, savingsgoal.ErrGoalNotFound
	}
	return &g, nil
}

func (m *memorySavingsGoalRepo) Update(_ context.Context, goal *savingsgoal.SavingsGoal) error {
	if _, ok := m.goals[goal.ID]; !ok {
		return savingsgoal.ErrGoalNotFound
	}
	m.goals[goal.ID] = *goal
	return nil
}

func (m *memorySavingsGoalRepo) Delete(_ context.Context, id, userID uuid.UUID) error {
	g, ok := m.goals[id]
	if !ok || g.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	delete(m.goals, id)
	return nil
}

func (m *memorySavingsGoalRepo) SumVaultBalance(_ context.Context, userID uuid.UUID, currency string) (decimal.Decimal, error) {
	if bal, ok := m.balances[balanceKey(userID, currency)]; ok {
		return bal, nil
	}
	return decimal.Zero, nil
}

func (m *memorySavingsGoalRepo) UpdateMilestones(_ context.Context, goalID uuid.UUID, milestones []int) error {
	g, ok := m.goals[goalID]
	if !ok {
		return savingsgoal.ErrGoalNotFound
	}
	g.NotifiedMilestones = append([]int(nil), milestones...)
	m.goals[goalID] = g
	return nil
}

type recordingGoalMilestoneNotifier struct {
	mu    sync.Mutex
	calls []recordedGoalMilestone
}

type recordedGoalMilestone struct {
	UserID    uuid.UUID
	GoalID    uuid.UUID
	Milestone int
}

func (r *recordingGoalMilestoneNotifier) SendGoalMilestone(_ context.Context, userID uuid.UUID, goal savingsgoal.SavingsGoal, milestone int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedGoalMilestone{UserID: userID, GoalID: goal.ID, Milestone: milestone})
}

func (r *recordingGoalMilestoneNotifier) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func waitForMilestoneNotifications(t *testing.T, notifier *recordingGoalMilestoneNotifier, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if notifier.count() >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d notifications, got %d", want, notifier.count())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func assertNoMilestoneNotifications(t *testing.T, notifier *recordingGoalMilestoneNotifier) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	if n := notifier.count(); n != 0 {
		t.Fatalf("notifications = %d, want 0", n)
	}
}

func TestSavingsGoalService_MilestoneNotifications(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	target := decimal.NewFromInt(100)

	t.Run("24 percent no notification", func(t *testing.T) {
		repo := newMemorySavingsGoalRepo()
		repo.setBalance(userID, "USDC", decimal.NewFromInt(24))
		notifier := &recordingGoalMilestoneNotifier{}
		svc := NewSavingsGoalService(repo, notifier)

		goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
			TargetAmount: target,
			Currency:     "USDC",
			Deadline:     testDeadline(),
			Description:  "Vacation",
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if goal.ProgressPct != 24 {
			t.Fatalf("progress_pct = %v, want 24", goal.ProgressPct)
		}
		assertNoMilestoneNotifications(t, notifier)
	})

	t.Run("25 percent fires notification", func(t *testing.T) {
		repo := newMemorySavingsGoalRepo()
		repo.setBalance(userID, "USDC", decimal.NewFromInt(25))
		notifier := &recordingGoalMilestoneNotifier{}
		svc := NewSavingsGoalService(repo, notifier)

		goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
			TargetAmount: target,
			Currency:     "USDC",
			Deadline:     testDeadline(),
			Description:  "Vacation",
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if goal.ProgressPct != 25 {
			t.Fatalf("progress_pct = %v, want 25", goal.ProgressPct)
		}
		waitForMilestoneNotifications(t, notifier, 1)
		if notifier.calls[0].Milestone != 25 {
			t.Fatalf("milestone = %d, want 25", notifier.calls[0].Milestone)
		}
	})

	t.Run("25 percent again no duplicate", func(t *testing.T) {
		repo := newMemorySavingsGoalRepo()
		repo.setBalance(userID, "USDC", decimal.NewFromInt(25))
		notifier := &recordingGoalMilestoneNotifier{}
		svc := NewSavingsGoalService(repo, notifier)

		goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
			TargetAmount: target,
			Currency:     "USDC",
			Deadline:     testDeadline(),
			Description:  "Vacation",
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		waitForMilestoneNotifications(t, notifier, 1)

		if _, err := svc.Get(ctx, userID, goal.ID); err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		time.Sleep(50 * time.Millisecond)
		if n := notifier.count(); n != 1 {
			t.Fatalf("notifications = %d, want 1 (no duplicate)", n)
		}
		stored, err := repo.GetByID(ctx, goal.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if !savingsgoal.ContainsMilestone(stored.NotifiedMilestones, 25) {
			t.Fatalf("notified_milestones = %v, want 25", stored.NotifiedMilestones)
		}
	})
}

func testDeadline() time.Time {
	return time.Now().UTC().Add(30 * 24 * time.Hour)
}

func TestSavingsGoalService_Create_ValidCategory(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Category:     "education",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if goal.Category != savingsgoal.CategoryEducation {
		t.Fatalf("category = %q, want education", goal.Category)
	}
}

func TestSavingsGoalService_Create_InvalidCategory(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil)

	_, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Category:     "vacation",
	})
	if err == nil {
		t.Fatal("Create() error = nil, want invalid category")
	}
}

func TestSavingsGoalService_Create_MissingCategoryDefaultsToOther(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if goal.Category != savingsgoal.CategoryOther {
		t.Fatalf("category = %q, want other", goal.Category)
	}
}

func TestSavingsGoalService_List_FilterByCategory(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	eduID := uuid.New()
	travelID := uuid.New()
	repo.goals[eduID] = savingsgoal.SavingsGoal{
		ID:           eduID,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Category:     savingsgoal.CategoryEducation,
	}
	repo.goals[travelID] = savingsgoal.SavingsGoal{
		ID:           travelID,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(500),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Category:     savingsgoal.CategoryTravel,
	}
	svc := NewSavingsGoalService(repo, nil)

	goals, err := svc.List(ctx, userID, "education")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(goals) != 1 {
		t.Fatalf("len(goals) = %d, want 1", len(goals))
	}
	if goals[0].Category != savingsgoal.CategoryEducation {
		t.Fatalf("category = %q, want education", goals[0].Category)
	}
}

func TestSavingsGoalService_List_InvalidCategoryFilter(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil)

	_, err := svc.List(ctx, userID, "invalid")
	if err == nil {
		t.Fatal("List() error = nil, want invalid category")
	}
}

func TestParseCategory_AcceptsAllValues(t *testing.T) {
	categories := []savingsgoal.GoalCategory{
		savingsgoal.CategoryEmergencyFund,
		savingsgoal.CategoryEducation,
		savingsgoal.CategoryHousing,
		savingsgoal.CategoryTravel,
		savingsgoal.CategoryBusiness,
		savingsgoal.CategoryHealth,
		savingsgoal.CategoryRetirement,
		savingsgoal.CategoryOther,
	}
	for _, want := range categories {
		got, err := savingsgoal.ParseCategory(string(want))
		if err != nil {
			t.Fatalf("ParseCategory(%q) error = %v", want, err)
		}
		if got != want {
			t.Fatalf("ParseCategory(%q) = %q", want, got)
		}
	}
}

func TestSavingsGoalService_Create_ValidXLMGoal(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	repo.setBalance(userID, "XLM", decimal.NewFromInt(120))
	svc := NewSavingsGoalService(repo, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(500),
		Currency:     "XLM",
		Deadline:     testDeadline(),
		Description:  "Staking fund",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if goal.Currency != savingsgoal.CurrencyXLM {
		t.Fatalf("currency = %q, want XLM", goal.Currency)
	}
	if !goal.CurrentAmount.Equal(decimal.NewFromInt(120)) {
		t.Fatalf("current_amount = %s, want 120 XLM vault balance", goal.CurrentAmount)
	}
}

func TestSavingsGoalService_Create_ValidUSDCGoal(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(250))
	svc := NewSavingsGoalService(repo, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "usdc",
		Deadline:     testDeadline(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if goal.Currency != savingsgoal.CurrencyUSDC {
		t.Fatalf("currency = %q, want USDC", goal.Currency)
	}
	if !goal.CurrentAmount.Equal(decimal.NewFromInt(250)) {
		t.Fatalf("current_amount = %s, want 250 USDC vault balance", goal.CurrentAmount)
	}
}

func TestSavingsGoalService_Create_InvalidCurrency(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil)

	_, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "BTC",
		Deadline:     testDeadline(),
	})
	if err == nil {
		t.Fatal("Create() error = nil, want invalid currency")
	}
}

func TestSavingsGoalService_Summary_MixedCurrencies(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(100))
	repo.setBalance(userID, "XLM", decimal.NewFromInt(50))
	usdcGoalID := uuid.New()
	xlmGoalID := uuid.New()
	repo.goals[usdcGoalID] = savingsgoal.SavingsGoal{
		ID:           usdcGoalID,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     savingsgoal.CurrencyUSDC,
		Deadline:     testDeadline(),
	}
	repo.goals[xlmGoalID] = savingsgoal.SavingsGoal{
		ID:           xlmGoalID,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(500),
		Currency:     savingsgoal.CurrencyXLM,
		Deadline:     testDeadline(),
	}
	svc := NewSavingsGoalService(repo, nil)

	summary, err := svc.Summary(ctx, userID)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.GoalCount != 2 {
		t.Fatalf("goal_count = %d, want 2", summary.GoalCount)
	}
	if !summary.TotalSavedUSDC.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("total_saved_usdc = %s, want 100", summary.TotalSavedUSDC)
	}
	if !summary.TotalTargetUSDC.Equal(decimal.NewFromInt(1000)) {
		t.Fatalf("total_target_usdc = %s, want 1000", summary.TotalTargetUSDC)
	}
	if !summary.TotalSavedXLM.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("total_saved_xlm = %s, want 50", summary.TotalSavedXLM)
	}
	if !summary.TotalTargetXLM.Equal(decimal.NewFromInt(500)) {
		t.Fatalf("total_target_xlm = %s, want 500", summary.TotalTargetXLM)
	}
	// #683: status counts, USDC overall progress, next deadline.
	if summary.ActiveGoals != 2 || summary.CompletedGoals != 0 {
		t.Fatalf("active/completed = %d/%d, want 2/0", summary.ActiveGoals, summary.CompletedGoals)
	}
	if summary.OverallProgressPct != 10 { // 100 saved / 1000 target USDC
		t.Fatalf("overall_progress_pct = %v, want 10", summary.OverallProgressPct)
	}
	if summary.NextDeadline == nil {
		t.Fatal("next_deadline = nil, want the nearest active deadline")
	}
}

func TestSavingsGoalService_Summary_NoGoals(t *testing.T) {
	ctx := context.Background()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil)

	summary, err := svc.Summary(ctx, uuid.New())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.GoalCount != 0 || summary.ActiveGoals != 0 || summary.CompletedGoals != 0 {
		t.Fatalf("counts = %+v, want all zero", summary)
	}
	if summary.OverallProgressPct != 0 {
		t.Fatalf("overall_progress_pct = %v, want 0", summary.OverallProgressPct)
	}
	if summary.NextDeadline != nil {
		t.Fatalf("next_deadline = %v, want nil", summary.NextDeadline)
	}
}

func TestSavingsGoalService_Summary_CompletedAndProgressCap(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(5000)) // saved exceeds target
	goalID := uuid.New()
	repo.goals[goalID] = savingsgoal.SavingsGoal{
		ID:           goalID,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     savingsgoal.CurrencyUSDC,
		Deadline:     testDeadline(),
	}
	svc := NewSavingsGoalService(repo, nil)

	summary, err := svc.Summary(ctx, userID)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.CompletedGoals != 1 || summary.ActiveGoals != 0 {
		t.Fatalf("active/completed = %d/%d, want 0/1", summary.ActiveGoals, summary.CompletedGoals)
	}
	if summary.OverallProgressPct != 100 {
		t.Fatalf("overall_progress_pct = %v, want 100 (capped)", summary.OverallProgressPct)
	}
	if summary.NextDeadline != nil {
		t.Fatalf("next_deadline = %v, want nil (no active goals)", summary.NextDeadline)
	}
}
