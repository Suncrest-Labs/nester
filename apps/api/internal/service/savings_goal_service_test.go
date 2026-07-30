package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type memorySavingsGoalRepo struct {
	goals        map[uuid.UUID]savingsgoal.SavingsGoal
	balances     map[string]decimal.Decimal
	goalDeposits map[string]decimal.Decimal // keyed by goalID string
}

func newMemorySavingsGoalRepo() *memorySavingsGoalRepo {
	return &memorySavingsGoalRepo{
		goals:        make(map[uuid.UUID]savingsgoal.SavingsGoal),
		balances:     make(map[string]decimal.Decimal),
		goalDeposits: make(map[string]decimal.Decimal),
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

func (m *memorySavingsGoalRepo) ListByUser(_ context.Context, userID uuid.UUID, category, search string) ([]savingsgoal.SavingsGoal, error) {
	var out []savingsgoal.SavingsGoal
	for _, g := range m.goals {
		if g.UserID != userID {
			continue
		}
		if g.DeletedAt != nil {
			continue
		}
		if category != "" && string(g.Category) != category {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(g.Name), strings.ToLower(search)) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (m *memorySavingsGoalRepo) GetByID(_ context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[id]
	if !ok || g.DeletedAt != nil {
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
	if !ok || g.UserID != userID || g.DeletedAt != nil {
		return savingsgoal.ErrGoalNotFound
	}
	now := time.Now().UTC()
	g.DeletedAt = &now
	m.goals[id] = g
	return nil
}

func (m *memorySavingsGoalRepo) Restore(_ context.Context, id, userID uuid.UUID) error {
	g, ok := m.goals[id]
	if !ok || g.UserID != userID || g.DeletedAt == nil {
		return savingsgoal.ErrGoalNotFound
	}
	g.DeletedAt = nil
	m.goals[id] = g
	return nil
}

func (m *memorySavingsGoalRepo) GetByIDIncludingDeleted(_ context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[id]
	if !ok {
		return nil, savingsgoal.ErrGoalNotFound
	}
	return &g, nil
}

func (m *memorySavingsGoalRepo) ListDeletedOlderThan(_ context.Context, cutoff time.Time) ([]savingsgoal.SavingsGoal, error) {
	var out []savingsgoal.SavingsGoal
	for _, g := range m.goals {
		if g.DeletedAt != nil && g.DeletedAt.Before(cutoff) {
			out = append(out, g)
		}
	}
	return out, nil
}

func (m *memorySavingsGoalRepo) HardDelete(_ context.Context, id uuid.UUID) error {
	g, ok := m.goals[id]
	if !ok || g.DeletedAt == nil {
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

func (m *memorySavingsGoalRepo) ListContributions(context.Context, uuid.UUID, uuid.UUID, interface{}) ([]savingsgoal.GoalContribution, int, string, error) {
	return nil, 0, "", nil
}

func (m *memorySavingsGoalRepo) UpdateDeadlineReminders(_ context.Context, goalID uuid.UUID, reminders []int) error {
	g, ok := m.goals[goalID]
	if !ok {
		return savingsgoal.ErrGoalNotFound
	}
	g.DeadlineRemindersSent = append([]int(nil), reminders...)
	m.goals[goalID] = g
	return nil
}

func (m *memorySavingsGoalRepo) ListActiveApproachingDeadline(_ context.Context, maxDays int) ([]savingsgoal.SavingsGoal, error) {
	now := time.Now().UTC()
	cutoff := now.AddDate(0, 0, maxDays)
	var out []savingsgoal.SavingsGoal
	for _, g := range m.goals {
		if g.Status != "" && g.Status != savingsgoal.GoalStatusActive {
			continue
		}
		if g.Deadline.Before(now) || g.Deadline.After(cutoff) {
			continue
		}
		out = append(out, g)
	}
	return out, nil
}

func (m *memorySavingsGoalRepo) SumRecentDeposits(context.Context, uuid.UUID, string, time.Time) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *memorySavingsGoalRepo) UpdateStatus(_ context.Context, goalID, _ uuid.UUID, status string) error {
	g, ok := m.goals[goalID]
	if !ok {
		return savingsgoal.ErrGoalNotFound
	}
	g.Status = status
	m.goals[goalID] = g
	return nil
}
func (m *memorySavingsGoalRepo) MarkCompleted(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}

func (m *memorySavingsGoalRepo) RecordGoalDeposits(_ context.Context, deposits []savingsgoal.GoalDeposit) error {
	for _, d := range deposits {
		key := d.GoalID.String()
		m.goalDeposits[key] = m.goalDeposits[key].Add(d.Amount)
	}
	return nil
}

func (m *memorySavingsGoalRepo) SumGoalDeposits(_ context.Context, goalID uuid.UUID) (decimal.Decimal, error) {
	if v, ok := m.goalDeposits[goalID.String()]; ok {
		return v, nil
	}
	return decimal.Zero, nil
}

func (m *memorySavingsGoalRepo) SetShareToken(_ context.Context, goalID, userID uuid.UUID, token uuid.UUID) error {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	g.ShareToken = &token
	g.IsShared = true
	m.goals[goalID] = g
	return nil
}

func (m *memorySavingsGoalRepo) ClearShareToken(_ context.Context, goalID, userID uuid.UUID) error {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	g.ShareToken = nil
	g.IsShared = false
	m.goals[goalID] = g
	return nil
}

func (m *memorySavingsGoalRepo) GetByShareToken(_ context.Context, token uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	for _, g := range m.goals {
		if g.ShareToken != nil && *g.ShareToken == token {
			return &g, nil
		}
	}
	return nil, savingsgoal.ErrGoalNotFound
}

func (m *memorySavingsGoalRepo) UpdateOnchainLink(_ context.Context, goalID uuid.UUID, onchainGoalID, onchainStatus string) error {
	g, ok := m.goals[goalID]
	if !ok {
		return savingsgoal.ErrGoalNotFound
	}
	g.OnchainGoalID = &onchainGoalID
	g.OnchainStatus = &onchainStatus
	m.goals[goalID] = g
	return nil
}

// newVaultReader builds an in-memory VaultReader seeded with the given vaults,
// reusing the shared memoryVaultRepo fake.
func newVaultReader(vaults ...vault.Vault) *memoryVaultRepo {
	repo := &memoryVaultRepo{vaults: make(map[uuid.UUID]vault.Vault, len(vaults))}
	for _, v := range vaults {
		repo.vaults[v.ID] = v
	}
	return repo
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
		svc := NewSavingsGoalService(repo, nil, notifier)

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
		svc := NewSavingsGoalService(repo, nil, notifier)

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
		svc := NewSavingsGoalService(repo, nil, notifier)

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
	svc := NewSavingsGoalService(repo, nil, nil)

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
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)

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
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)

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
	svc := NewSavingsGoalService(repo, nil, nil)

	goals, err := svc.List(ctx, userID, "education", "", false)
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
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)

	_, err := svc.List(ctx, userID, "invalid", "", false)
	if err == nil {
		t.Fatal("List() error = nil, want invalid category")
	}
}

func TestSavingsGoalService_List_StatusFilter(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()

	// An active goal under target — stays "active" after enrichment.
	activeID := uuid.New()
	repo.goals[activeID] = savingsgoal.SavingsGoal{
		ID: activeID, UserID: userID, TargetAmount: decimal.NewFromInt(1000),
		Currency: "USDC", Deadline: testDeadline(), Category: savingsgoal.CategoryEducation,
		Status: savingsgoal.GoalStatusActive,
	}
	// An active goal whose balance has reached target — auto-completes (#716),
	// so it must read as "completed" and be excluded from ?status=active.
	completedID := uuid.New()
	repo.goals[completedID] = savingsgoal.SavingsGoal{
		ID: completedID, UserID: userID, TargetAmount: decimal.NewFromInt(500),
		Currency: "XLM", Deadline: testDeadline(), Category: savingsgoal.CategoryTravel,
		Status: savingsgoal.GoalStatusActive,
	}
	repo.setBalance(userID, "XLM", decimal.NewFromInt(500))
	// An archived goal.
	archivedID := uuid.New()
	repo.goals[archivedID] = savingsgoal.SavingsGoal{
		ID: archivedID, UserID: userID, TargetAmount: decimal.NewFromInt(200),
		Currency: "USDC", Deadline: testDeadline(), Category: savingsgoal.CategoryHousing,
		Status: savingsgoal.GoalStatusArchived,
	}

	svc := NewSavingsGoalService(repo, nil, nil)

	cases := map[string]uuid.UUID{
		savingsgoal.GoalStatusActive:    activeID,
		savingsgoal.GoalStatusCompleted: completedID,
		savingsgoal.GoalStatusArchived:  archivedID,
	}
	for status, wantID := range cases {
		goals, err := svc.List(ctx, userID, "", status, false)
		if err != nil {
			t.Fatalf("List(status=%q) error = %v", status, err)
		}
		if len(goals) != 1 {
			t.Fatalf("List(status=%q) returned %d goals, want 1", status, len(goals))
		}
		if goals[0].ID != wantID {
			t.Fatalf("List(status=%q) returned goal %s, want %s", status, goals[0].ID, wantID)
		}
		if goals[0].Status != status {
			t.Fatalf("List(status=%q) goal status = %q, want %q", status, goals[0].Status, status)
		}
	}

	// No filter excludes archived by default (#721).
	nonArchived, err := svc.List(ctx, userID, "", "", false)
	if err != nil {
		t.Fatalf("List(no filter) error = %v", err)
	}
	if len(nonArchived) != 2 {
		t.Fatalf("List(no filter) returned %d goals, want 2 (archived excluded)", len(nonArchived))
	}

	// include_archived=true returns all three.
	all, err := svc.List(ctx, userID, "", "", true)
	if err != nil {
		t.Fatalf("List(include_archived) error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List(include_archived) returned %d goals, want 3", len(all))
	}
}

func TestSavingsGoalService_List_InvalidStatusFilter(t *testing.T) {
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)
	if _, err := svc.List(context.Background(), uuid.New(), "", "bogus", false); err == nil {
		t.Fatal("List() error = nil, want invalid status")
	}
}

func TestSavingsGoalService_Archive(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	goalID := uuid.New()
	repo.goals[goalID] = savingsgoal.SavingsGoal{
		ID: goalID, UserID: userID, TargetAmount: decimal.NewFromInt(1000),
		Currency: "USDC", Deadline: testDeadline(), Category: savingsgoal.CategoryEducation,
		Status: savingsgoal.GoalStatusCompleted,
	}
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Archive(ctx, userID, goalID)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if goal.Status != savingsgoal.GoalStatusArchived {
		t.Fatalf("status = %q, want archived", goal.Status)
	}

	// Archiving someone else's goal is not found.
	if _, err := svc.Archive(ctx, uuid.New(), goalID); err != savingsgoal.ErrGoalNotFound {
		t.Fatalf("Archive(other user) error = %v, want ErrGoalNotFound", err)
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
	svc := NewSavingsGoalService(repo, nil, nil)

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
	svc := NewSavingsGoalService(repo, nil, nil)

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
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)

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
	svc := NewSavingsGoalService(repo, nil, nil)

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
	if summary.ActiveGoalCount != 2 || summary.CompletedGoalCount != 0 {
		t.Fatalf("active/completed = %d/%d, want 2/0", summary.ActiveGoalCount, summary.CompletedGoalCount)
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
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)

	summary, err := svc.Summary(ctx, uuid.New())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.GoalCount != 0 || summary.ActiveGoalCount != 0 || summary.CompletedGoalCount != 0 {
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
	svc := NewSavingsGoalService(repo, nil, nil)

	summary, err := svc.Summary(ctx, userID)
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if summary.CompletedGoalCount != 1 || summary.ActiveGoalCount != 0 {
		t.Fatalf("active/completed = %d/%d, want 0/1", summary.ActiveGoalCount, summary.CompletedGoalCount)
	}
	if summary.OverallProgressPct != 100 {
		t.Fatalf("overall_progress_pct = %v, want 100 (capped)", summary.OverallProgressPct)
	}
	if summary.NextDeadline != nil {
		t.Fatalf("next_deadline = %v, want nil (no active goals)", summary.NextDeadline)
	}
}

func TestSavingsGoalService_Create_DeadlineValidation(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	cases := []struct {
		name     string
		deadline time.Time
		wantErr  bool
	}{
		{"deadline in the past is rejected", now.Add(-24 * time.Hour), true},
		{"deadline in 1 hour is rejected (under 24h)", now.Add(time.Hour), true},
		{"deadline in 25 hours is accepted", now.Add(25 * time.Hour), false},
		{"deadline in 30 days is accepted", now.Add(30 * 24 * time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)
			_, err := svc.Create(ctx, uuid.New(), CreateSavingsGoalInput{
				TargetAmount: decimal.NewFromInt(1000),
				Currency:     "USDC",
				Deadline:     tc.deadline,
			})
			if tc.wantErr {
				if err == nil {
					t.Fatal("Create() error = nil, want error")
				}
				if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
					t.Fatalf("Create() error = %v, want ErrInvalidGoal", err)
				}
			} else if err != nil {
				t.Fatalf("Create() error = %v, want nil", err)
			}
		})
	}
}

func TestSavingsGoalService_Update_DeadlineToPastRejected(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	goalID := uuid.New()
	repo.goals[goalID] = savingsgoal.SavingsGoal{
		ID:           goalID,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     savingsgoal.CurrencyUSDC,
		Deadline:     time.Now().UTC().Add(30 * 24 * time.Hour),
	}
	svc := NewSavingsGoalService(repo, nil, nil)

	past := time.Now().UTC().Add(-time.Hour)
	_, err := svc.Update(ctx, userID, goalID, UpdateSavingsGoalInput{Deadline: &past})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("Update() error = %v, want ErrInvalidGoal", err)
	}
}

func TestSavingsGoalService_Update_ExtendOverdueGoalSucceeds(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(100)) // below target → not completed
	goalID := uuid.New()
	repo.goals[goalID] = savingsgoal.SavingsGoal{
		ID:           goalID,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     savingsgoal.CurrencyUSDC,
		Deadline:     time.Now().UTC().Add(-48 * time.Hour), // already overdue
	}
	svc := NewSavingsGoalService(repo, nil, nil)

	newDeadline := time.Now().UTC().Add(14 * 24 * time.Hour)
	updated, err := svc.Update(ctx, userID, goalID, UpdateSavingsGoalInput{Deadline: &newDeadline})
	if err != nil {
		t.Fatalf("Update() error = %v, want nil (extending an overdue goal is allowed)", err)
	}
	if !updated.Deadline.Equal(newDeadline) {
		t.Fatalf("deadline = %v, want %v", updated.Deadline, newDeadline)
	}
}

func TestSavingsGoalService_Update_DeadlineOnCompletedGoalConflicts(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(1000)) // meets target → completed
	goalID := uuid.New()
	repo.goals[goalID] = savingsgoal.SavingsGoal{
		ID:           goalID,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     savingsgoal.CurrencyUSDC,
		Deadline:     time.Now().UTC().Add(30 * 24 * time.Hour),
	}
	svc := NewSavingsGoalService(repo, nil, nil)

	newDeadline := time.Now().UTC().Add(60 * 24 * time.Hour)
	_, err := svc.Update(ctx, userID, goalID, UpdateSavingsGoalInput{Deadline: &newDeadline})
	if !errors.Is(err, savingsgoal.ErrGoalCompleted) {
		t.Fatalf("Update() error = %v, want ErrGoalCompleted", err)
	}
}

func TestSavingsGoalService_Unarchive(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	goalID := uuid.New()
	repo.goals[goalID] = savingsgoal.SavingsGoal{
		ID: goalID, UserID: userID, TargetAmount: decimal.NewFromInt(1000),
		Currency: "USDC", Deadline: testDeadline(), Category: savingsgoal.CategoryEducation,
		Status: savingsgoal.GoalStatusArchived,
	}
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Unarchive(ctx, userID, goalID)
	if err != nil {
		t.Fatalf("Unarchive() error = %v", err)
	}
	if goal.Status != savingsgoal.GoalStatusActive {
		t.Fatalf("status = %q, want active", goal.Status)
	}

	// Unarchiving someone else's goal is not found.
	if _, err := svc.Unarchive(ctx, uuid.New(), goalID); err != savingsgoal.ErrGoalNotFound {
		t.Fatalf("Unarchive(other user) error = %v, want ErrGoalNotFound", err)
	}

	// Unarchiving a non-archived goal returns it unchanged (idempotent).
	repo.goals[goalID] = savingsgoal.SavingsGoal{
		ID: goalID, UserID: userID, TargetAmount: decimal.NewFromInt(1000),
		Currency: "USDC", Deadline: testDeadline(), Category: savingsgoal.CategoryEducation,
		Status: savingsgoal.GoalStatusActive,
	}
	active, err := svc.Unarchive(ctx, userID, goalID)
	if err != nil {
		t.Fatalf("Unarchive(active goal) error = %v", err)
	}
	if active.Status != savingsgoal.GoalStatusActive {
		t.Fatalf("status = %q, want active", active.Status)
	}
}

func makeActiveGoal(repo *memorySavingsGoalRepo, userID uuid.UUID, currency string, target int64) uuid.UUID {
	id := uuid.New()
	repo.goals[id] = savingsgoal.SavingsGoal{
		ID:           id,
		UserID:       userID,
		TargetAmount: decimal.NewFromInt(target),
		Currency:     savingsgoal.NormalizeCurrency(currency),
		Status:       savingsgoal.GoalStatusActive,
		Deadline:     time.Now().UTC().Add(30 * 24 * time.Hour),
	}
	return id
}

func TestSavingsGoalService_DepositSplit_AmountMode(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "USDC", 1000)
	g2 := makeActiveGoal(repo, userID, "USDC", 500)

	result, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Amount: decimal.NewFromInt(60)},
			{GoalID: g2, Amount: decimal.NewFromInt(40)},
		},
	})
	if err != nil {
		t.Fatalf("DepositSplit() error = %v", err)
	}
	if len(result.Goals) != 2 {
		t.Fatalf("len(Goals) = %d, want 2", len(result.Goals))
	}
	if !result.TotalDeposited.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("TotalDeposited = %s, want 100", result.TotalDeposited)
	}
	if !result.Goals[0].Deposited.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("Goals[0].Deposited = %s, want 60", result.Goals[0].Deposited)
	}
	if !result.Goals[1].Deposited.Equal(decimal.NewFromInt(40)) {
		t.Fatalf("Goals[1].Deposited = %s, want 40", result.Goals[1].Deposited)
	}
	// current_amount from SumGoalDeposits
	if !result.Goals[0].CurrentAmount.Equal(decimal.NewFromInt(60)) {
		t.Fatalf("Goals[0].CurrentAmount = %s, want 60", result.Goals[0].CurrentAmount)
	}
}

func TestSavingsGoalService_DepositSplit_PercentageMode(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "USDC", 1000)
	g2 := makeActiveGoal(repo, userID, "USDC", 500)

	result, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(200),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Percentage: decimal.NewFromInt(75)},
			{GoalID: g2, Percentage: decimal.NewFromInt(25)},
		},
	})
	if err != nil {
		t.Fatalf("DepositSplit() percentage error = %v", err)
	}
	if !result.Goals[0].Deposited.Equal(decimal.NewFromInt(150)) {
		t.Fatalf("Goals[0].Deposited = %s, want 150", result.Goals[0].Deposited)
	}
	if !result.Goals[1].Deposited.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("Goals[1].Deposited = %s, want 50", result.Goals[1].Deposited)
	}
}

func TestSavingsGoalService_DepositSplit_AmountMismatch(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "USDC", 1000)

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Amount: decimal.NewFromInt(60)},
		},
	})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("mismatch error = %v, want ErrInvalidGoal", err)
	}
}

func TestSavingsGoalService_DepositSplit_PercentageMismatch(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "USDC", 1000)

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Percentage: decimal.NewFromInt(70)},
		},
	})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("percentage mismatch error = %v, want ErrInvalidGoal", err)
	}
}

func TestSavingsGoalService_DepositSplit_DuplicateGoal(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "USDC", 1000)

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Amount: decimal.NewFromInt(50)},
			{GoalID: g1, Amount: decimal.NewFromInt(50)},
		},
	})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("duplicate goal error = %v, want ErrInvalidGoal", err)
	}
}

func TestSavingsGoalService_DepositSplit_GoalNotFound(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: uuid.New(), Amount: decimal.NewFromInt(100)},
		},
	})
	if !errors.Is(err, savingsgoal.ErrGoalNotFound) {
		t.Fatalf("not-found error = %v, want ErrGoalNotFound", err)
	}
}

func TestSavingsGoalService_DepositSplit_GoalPaused(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "USDC", 1000)
	g := repo.goals[g1]
	g.Status = savingsgoal.GoalStatusPaused
	repo.goals[g1] = g

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Amount: decimal.NewFromInt(100)},
		},
	})
	if !errors.Is(err, savingsgoal.ErrGoalPaused) {
		t.Fatalf("paused error = %v, want ErrGoalPaused", err)
	}
}

func TestSavingsGoalService_DepositSplit_GoalArchived(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "USDC", 1000)
	g := repo.goals[g1]
	g.Status = savingsgoal.GoalStatusArchived
	repo.goals[g1] = g

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Amount: decimal.NewFromInt(100)},
		},
	})
	if !errors.Is(err, savingsgoal.ErrGoalArchived) {
		t.Fatalf("archived error = %v, want ErrGoalArchived", err)
	}
}

func TestSavingsGoalService_DepositSplit_CurrencyMismatch(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "XLM", 1000)

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Amount: decimal.NewFromInt(100)},
		},
	})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("currency mismatch error = %v, want ErrInvalidGoal", err)
	}
}

func TestSavingsGoalService_DepositSplit_WrongUser(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	otherUser := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, otherUser, "USDC", 1000)

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Amount: decimal.NewFromInt(100)},
		},
	})
	if !errors.Is(err, savingsgoal.ErrGoalNotFound) {
		t.Fatalf("wrong user error = %v, want ErrGoalNotFound", err)
	}
}

func TestSavingsGoalService_DepositSplit_Atomic_RecordsAll(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	g1 := makeActiveGoal(repo, userID, "USDC", 1000)
	g2 := makeActiveGoal(repo, userID, "USDC", 500)

	_, err := svc.DepositSplit(ctx, userID, DepositSplitInput{
		TotalAmount: decimal.NewFromInt(100),
		Currency:    "USDC",
		Allocations: []DepositSplitAllocation{
			{GoalID: g1, Amount: decimal.NewFromInt(70)},
			{GoalID: g2, Amount: decimal.NewFromInt(30)},
		},
	})
	if err != nil {
		t.Fatalf("DepositSplit() error = %v", err)
	}

	sum1, _ := repo.SumGoalDeposits(ctx, g1)
	sum2, _ := repo.SumGoalDeposits(ctx, g2)
	if !sum1.Equal(decimal.NewFromInt(70)) {
		t.Fatalf("goal1 deposits = %s, want 70", sum1)
	}
	if !sum2.Equal(decimal.NewFromInt(30)) {
		t.Fatalf("goal2 deposits = %s, want 30", sum2)
	}
}

func TestSavingsGoalService_Create_LinksVaultAndReflectsBalance(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	vaultID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	// Sum-all would report 999; a correctly linked goal must ignore it.
	repo.setBalance(userID, "USDC", decimal.NewFromInt(999))
	vaults := newVaultReader(vault.Vault{
		ID:             vaultID,
		UserID:         userID,
		Currency:       "USDC",
		CurrentBalance: decimal.NewFromInt(300),
	})
	svc := NewSavingsGoalService(repo, vaults, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		VaultID:      &vaultID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if goal.VaultID == nil || *goal.VaultID != vaultID {
		t.Fatalf("vault_id = %v, want %v", goal.VaultID, vaultID)
	}
	if !goal.CurrentAmount.Equal(decimal.NewFromInt(300)) {
		t.Fatalf("current_amount = %s, want 300 (linked vault balance)", goal.CurrentAmount)
	}
}

func TestSavingsGoalService_Create_ForeignVaultReturnsUnauthorized(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	otherUser := uuid.New()
	vaultID := uuid.New()
	vaults := newVaultReader(vault.Vault{
		ID:             vaultID,
		UserID:         otherUser,
		Currency:       "USDC",
		CurrentBalance: decimal.NewFromInt(300),
	})
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), vaults, nil)

	_, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		VaultID:      &vaultID,
	})
	if !errors.Is(err, savingsgoal.ErrUnauthorized) {
		t.Fatalf("Create() error = %v, want ErrUnauthorized", err)
	}
}

func TestSavingsGoalService_Create_VaultCurrencyMismatch(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	vaultID := uuid.New()
	vaults := newVaultReader(vault.Vault{
		ID:             vaultID,
		UserID:         userID,
		Currency:       "XLM",
		CurrentBalance: decimal.NewFromInt(300),
	})
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), vaults, nil)

	_, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		VaultID:      &vaultID,
	})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("Create() error = %v, want ErrInvalidGoal", err)
	}
	if !strings.Contains(err.Error(), "vault currency (XLM) does not match goal currency (USDC)") {
		t.Fatalf("error message = %q, want vault/goal currency mismatch detail", err.Error())
	}
}

func TestSavingsGoalService_Create_VaultNotFound(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	missing := uuid.New()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), newVaultReader(), nil)

	_, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		VaultID:      &missing,
	})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("Create() error = %v, want ErrInvalidGoal", err)
	}
}

// TestSavingsGoalService_PerVaultIsolation is the core regression guard for
// issue #688: two vaults in the same currency, two goals each linked to their
// own vault, must each report only their own vault's balance — never the
// combined sum.
func TestSavingsGoalService_PerVaultIsolation(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	vaultA := uuid.New()
	vaultB := uuid.New()
	repo := newMemorySavingsGoalRepo()
	// Legacy sum-all would inflate both goals to 1000.
	repo.setBalance(userID, "USDC", decimal.NewFromInt(1000))
	vaults := newVaultReader(
		vault.Vault{ID: vaultA, UserID: userID, Currency: "USDC", CurrentBalance: decimal.NewFromInt(200)},
		vault.Vault{ID: vaultB, UserID: userID, Currency: "USDC", CurrentBalance: decimal.NewFromInt(800)},
	)
	svc := NewSavingsGoalService(repo, vaults, nil)

	goalA, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		VaultID:      &vaultA,
	})
	if err != nil {
		t.Fatalf("Create(goalA) error = %v", err)
	}
	goalB, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		VaultID:      &vaultB,
	})
	if err != nil {
		t.Fatalf("Create(goalB) error = %v", err)
	}

	if !goalA.CurrentAmount.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("goalA current_amount = %s, want 200", goalA.CurrentAmount)
	}
	if !goalB.CurrentAmount.Equal(decimal.NewFromInt(800)) {
		t.Fatalf("goalB current_amount = %s, want 800", goalB.CurrentAmount)
	}

	// Confirm the isolation survives a re-read through List.
	goals, err := svc.List(ctx, userID, "", "", false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	byID := map[uuid.UUID]decimal.Decimal{}
	for _, g := range goals {
		byID[g.ID] = g.CurrentAmount
	}
	if !byID[goalA.ID].Equal(decimal.NewFromInt(200)) {
		t.Fatalf("listed goalA current_amount = %s, want 200", byID[goalA.ID])
	}
	if !byID[goalB.ID].Equal(decimal.NewFromInt(800)) {
		t.Fatalf("listed goalB current_amount = %s, want 800", byID[goalB.ID])
	}
}

func TestSavingsGoalService_NoVaultFallsBackToSum(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	repo.setBalance(userID, "USDC", decimal.NewFromInt(450))
	svc := NewSavingsGoalService(repo, newVaultReader(), nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if goal.VaultID != nil {
		t.Fatalf("vault_id = %v, want nil", goal.VaultID)
	}
	if !goal.CurrentAmount.Equal(decimal.NewFromInt(450)) {
		t.Fatalf("current_amount = %s, want 450 (sum-all fallback)", goal.CurrentAmount)
	}
}

func TestSavingsGoalService_Create_RejectsZeroAndNegativeTarget(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)

	for _, amount := range []decimal.Decimal{
		decimal.Zero,
		decimal.NewFromInt(-100),
	} {
		_, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
			TargetAmount: amount,
			Currency:     "USDC",
			Deadline:     testDeadline(),
		})
		if !errors.Is(err, savingsgoal.ErrInvalidAmount) {
			t.Errorf("Create(target=%s) error = %v, want ErrInvalidAmount", amount, err)
		}
	}
}

func TestSavingsGoalService_Create_RejectsTargetBelowMinimum(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)

	_, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.RequireFromString("0.001"),
		Currency:     "USDC",
		Deadline:     testDeadline(),
	})
	if !errors.Is(err, savingsgoal.ErrInvalidAmount) {
		t.Fatalf("Create(target=0.001) error = %v, want ErrInvalidAmount", err)
	}

	// The minimum itself is accepted.
	if _, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: savingsgoal.MinTargetAmount,
		Currency:     "USDC",
		Deadline:     testDeadline(),
	}); err != nil {
		t.Fatalf("Create(target=0.01) error = %v, want nil", err)
	}
}

func TestSavingsGoalService_Update_RejectsZeroTarget(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	zero := decimal.Zero
	_, err = svc.Update(ctx, userID, goal.ID, UpdateSavingsGoalInput{TargetAmount: &zero})
	if !errors.Is(err, savingsgoal.ErrInvalidAmount) {
		t.Fatalf("Update(target=0) error = %v, want ErrInvalidAmount", err)
	}
}

func TestSavingsGoalService_Create_RejectsOverlongName(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo(), nil, nil)

	_, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Name:         strings.Repeat("n", MaxGoalNameLength+1),
	})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("Create(101-char name) error = %v, want ErrInvalidGoal", err)
	}

	// Exactly the maximum is accepted.
	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Name:         strings.Repeat("n", MaxGoalNameLength),
	})
	if err != nil {
		t.Fatalf("Create(100-char name) error = %v, want nil", err)
	}
	if len([]rune(goal.Name)) != MaxGoalNameLength {
		t.Fatalf("name length = %d, want %d", len([]rune(goal.Name)), MaxGoalNameLength)
	}
}

func TestSavingsGoalService_Update_RejectsOverlongName(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Name:         "Emergency fund",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	long := strings.Repeat("n", MaxGoalNameLength+1)
	_, err = svc.Update(ctx, userID, goal.ID, UpdateSavingsGoalInput{Name: &long})
	if !errors.Is(err, savingsgoal.ErrInvalidGoal) {
		t.Fatalf("Update(101-char name) error = %v, want ErrInvalidGoal", err)
	}
}

// TestSavingsGoalService_Delete_HidesGoalFromReads covers the #924
// soft-delete: after Delete, GetByID/ListByUser must no longer surface the
// goal (deleted_at IS NULL filtering), even though the row still exists.
func TestSavingsGoalService_Delete_HidesGoalFromReads(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Name:         "Emergency fund",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := svc.Delete(ctx, userID, goal.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := svc.Get(ctx, userID, goal.ID); !errors.Is(err, savingsgoal.ErrGoalNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrGoalNotFound", err)
	}

	goals, err := svc.List(ctx, userID, "", "", false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, g := range goals {
		if g.ID == goal.ID {
			t.Fatalf("expected deleted goal %s to be excluded from List()", goal.ID)
		}
	}
}

// TestSavingsGoalService_Restore_WithinWindowUndeletesGoal covers restoring a
// goal deleted less than SavingsGoalRecoveryWindow ago (#924).
func TestSavingsGoalService_Restore_WithinWindowUndeletesGoal(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Name:         "Emergency fund",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := svc.Delete(ctx, userID, goal.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	restored, err := svc.Restore(ctx, userID, goal.ID)
	if err != nil {
		t.Fatalf("Restore() error = %v, want nil", err)
	}
	if restored.DeletedAt != nil {
		t.Fatalf("expected restored goal DeletedAt to be nil, got %v", restored.DeletedAt)
	}

	if _, err := svc.Get(ctx, userID, goal.ID); err != nil {
		t.Fatalf("Get() after restore error = %v, want nil", err)
	}
}

// TestSavingsGoalService_Restore_AfterWindowReturnsExpiredError covers
// attempting to restore a goal deleted more than SavingsGoalRecoveryWindow
// ago (#924): Restore must refuse, not silently undelete it.
func TestSavingsGoalService_Restore_AfterWindowReturnsExpiredError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Name:         "Emergency fund",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := svc.Delete(ctx, userID, goal.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Back-date deleted_at past the recovery window directly in the fake repo.
	g := repo.goals[goal.ID]
	expired := time.Now().UTC().Add(-(savingsgoal.SavingsGoalRecoveryWindow + time.Hour))
	g.DeletedAt = &expired
	repo.goals[goal.ID] = g

	_, err = svc.Restore(ctx, userID, goal.ID)
	if !errors.Is(err, savingsgoal.ErrRecoveryWindowExpired) {
		t.Fatalf("Restore() after window error = %v, want ErrRecoveryWindowExpired", err)
	}
}

// TestSavingsGoalService_Restore_NeverDeletedReturnsNotDeletedError covers
// calling Restore on a goal that was never soft-deleted (#924).
func TestSavingsGoalService_Restore_NeverDeletedReturnsNotDeletedError(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Name:         "Emergency fund",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = svc.Restore(ctx, userID, goal.ID)
	if !errors.Is(err, savingsgoal.ErrGoalNotDeleted) {
		t.Fatalf("Restore() on non-deleted goal error = %v, want ErrGoalNotDeleted", err)
	}
}

// TestSavingsGoalService_Restore_WrongUserReturnsNotFound ensures Restore
// doesn't leak/act on another user's deleted goal (#924).
func TestSavingsGoalService_Restore_WrongUserReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	otherUserID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo, nil, nil)

	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(1000),
		Currency:     "USDC",
		Deadline:     testDeadline(),
		Name:         "Emergency fund",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := svc.Delete(ctx, userID, goal.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := svc.Restore(ctx, otherUserID, goal.ID); !errors.Is(err, savingsgoal.ErrGoalNotFound) {
		t.Fatalf("Restore() by wrong user error = %v, want ErrGoalNotFound", err)
	}
}
