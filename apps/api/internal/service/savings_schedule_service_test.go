package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type memoryScheduleRepo struct {
	schedules map[uuid.UUID]savingsschedule.SavingsSchedule
}

func (m *memoryScheduleRepo) Create(_ context.Context, schedule *savingsschedule.SavingsSchedule) error {
	for _, s := range m.schedules {
		if s.GoalID == schedule.GoalID && s.IsActive {
			return savingsschedule.ErrActiveScheduleExists
		}
	}
	m.schedules[schedule.ID] = *schedule
	return nil
}

func (m *memoryScheduleRepo) ListByGoal(_ context.Context, goalID, userID uuid.UUID) ([]savingsschedule.SavingsSchedule, error) {
	var out []savingsschedule.SavingsSchedule
	for _, s := range m.schedules {
		if s.GoalID == goalID && s.UserID == userID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memoryScheduleRepo) GetByID(_ context.Context, id uuid.UUID) (*savingsschedule.SavingsSchedule, error) {
	s, ok := m.schedules[id]
	if !ok {
		return nil, savingsschedule.ErrScheduleNotFound
	}
	return &s, nil
}

func (m *memoryScheduleRepo) Cancel(_ context.Context, scheduleID, goalID, userID uuid.UUID) error {
	s, ok := m.schedules[scheduleID]
	if !ok || s.GoalID != goalID || s.UserID != userID || !s.IsActive {
		return savingsschedule.ErrScheduleNotFound
	}
	s.IsActive = false
	m.schedules[scheduleID] = s
	return nil
}

func (m *memoryScheduleRepo) ListDue(context.Context, time.Time) ([]savingsschedule.SavingsSchedule, error) {
	return nil, nil
}

func (m *memoryScheduleRepo) UpdateAfterRun(context.Context, uuid.UUID, time.Time, time.Time) error {
	return nil
}

func (m *memoryScheduleRepo) Deactivate(context.Context, uuid.UUID) error {
	return nil
}

type memoryGoalRepo struct {
	goals map[uuid.UUID]savingsgoal.SavingsGoal
}

func (m *memoryGoalRepo) Create(context.Context, *savingsgoal.SavingsGoal) error { return nil }
func (m *memoryGoalRepo) ListByUser(context.Context, uuid.UUID, string) ([]savingsgoal.SavingsGoal, error) {
	return nil, nil
}
func (m *memoryGoalRepo) GetByID(_ context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	g, ok := m.goals[id]
	if !ok {
		return nil, savingsgoal.ErrGoalNotFound
	}
	return &g, nil
}
func (m *memoryGoalRepo) Update(context.Context, *savingsgoal.SavingsGoal) error { return nil }
func (m *memoryGoalRepo) Delete(context.Context, uuid.UUID, uuid.UUID) error     { return nil }
func (m *memoryGoalRepo) SumVaultBalance(context.Context, uuid.UUID, string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

type memoryVaultRepo struct {
	vaults map[uuid.UUID]vault.Vault
}

func (m *memoryVaultRepo) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	v, ok := m.vaults[id]
	if !ok {
		return vault.Vault{}, vault.ErrVaultNotFound
	}
	return v, nil
}

func TestSavingsScheduleService_CreateAndList(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	vaultID := uuid.New()

	svc := NewSavingsScheduleService(
		&memoryScheduleRepo{schedules: map[uuid.UUID]savingsschedule.SavingsSchedule{}},
		&memoryGoalRepo{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
			goalID: {ID: goalID, UserID: userID, TargetAmount: decimal.NewFromInt(1000), Currency: "USDC"},
		}},
		&memoryVaultRepo{vaults: map[uuid.UUID]vault.Vault{
			vaultID: {ID: vaultID, UserID: userID},
		}},
		decimal.Zero,
	)
	fixed := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return fixed })

	created, err := svc.Create(context.Background(), userID, goalID, CreateSavingsScheduleInput{
		Amount:    decimal.RequireFromString("50"),
		Currency:  "USDC",
		Frequency: "weekly",
		VaultID:   vaultID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.NextRunAt.Equal(fixed) {
		t.Fatalf("next_run_at = %v, want %v", created.NextRunAt, fixed)
	}

	list, err := svc.List(context.Background(), userID, goalID)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 schedule, got %d", len(list))
	}
}

func TestSavingsScheduleService_Create_ConflictOnSecondActive(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	vaultID := uuid.New()
	repo := &memoryScheduleRepo{schedules: map[uuid.UUID]savingsschedule.SavingsSchedule{}}
	svc := NewSavingsScheduleService(
		repo,
		&memoryGoalRepo{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
			goalID: {ID: goalID, UserID: userID},
		}},
		&memoryVaultRepo{vaults: map[uuid.UUID]vault.Vault{
			vaultID: {ID: vaultID, UserID: userID},
		}},
		decimal.Zero,
	)
	in := CreateSavingsScheduleInput{
		Amount:    decimal.RequireFromString("10"),
		Frequency: "weekly",
		VaultID:   vaultID,
	}
	if _, err := svc.Create(context.Background(), userID, goalID, in); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if _, err := svc.Create(context.Background(), userID, goalID, in); err != savingsschedule.ErrActiveScheduleExists {
		t.Fatalf("second Create() error = %v, want ErrActiveScheduleExists", err)
	}
}

func TestSavingsScheduleService_Create_RejectsForeignVault(t *testing.T) {
	userID := uuid.New()
	otherUser := uuid.New()
	goalID := uuid.New()
	vaultID := uuid.New()

	svc := NewSavingsScheduleService(
		&memoryScheduleRepo{schedules: map[uuid.UUID]savingsschedule.SavingsSchedule{}},
		&memoryGoalRepo{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
			goalID: {ID: goalID, UserID: userID},
		}},
		&memoryVaultRepo{vaults: map[uuid.UUID]vault.Vault{
			vaultID: {ID: vaultID, UserID: otherUser},
		}},
		decimal.Zero,
	)
	_, err := svc.Create(context.Background(), userID, goalID, CreateSavingsScheduleInput{
		Amount:    decimal.RequireFromString("10"),
		Frequency: "monthly",
		VaultID:   vaultID,
	})
	if err != savingsschedule.ErrUnauthorizedVault {
		t.Fatalf("Create() error = %v, want ErrUnauthorizedVault", err)
	}
}
