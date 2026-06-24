package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
)

type memorySavingsGoalRepo struct {
	goals    map[uuid.UUID]savingsgoal.SavingsGoal
	balances map[uuid.UUID]decimal.Decimal
}

func newMemorySavingsGoalRepo() *memorySavingsGoalRepo {
	return &memorySavingsGoalRepo{
		goals:    make(map[uuid.UUID]savingsgoal.SavingsGoal),
		balances: make(map[uuid.UUID]decimal.Decimal),
	}
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

func (m *memorySavingsGoalRepo) SumVaultBalance(_ context.Context, userID uuid.UUID, _ string) (decimal.Decimal, error) {
	return m.balances[userID], nil
}

func testDeadline() time.Time {
	return time.Now().UTC().Add(30 * 24 * time.Hour)
}

func TestSavingsGoalService_Create_ValidCategory(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	repo := newMemorySavingsGoalRepo()
	svc := NewSavingsGoalService(repo)

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
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo())

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
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo())

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
	svc := NewSavingsGoalService(repo)

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
	svc := NewSavingsGoalService(newMemorySavingsGoalRepo())

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
