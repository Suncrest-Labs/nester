package service

import (
	"context"
	"errors"
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

func (m *memoryScheduleRepo) GetActiveByGoal(_ context.Context, goalID, userID uuid.UUID) (*savingsschedule.SavingsSchedule, error) {
	for _, s := range m.schedules {
		if s.GoalID == goalID && s.UserID == userID && s.IsActive {
			return &s, nil
		}
	}
	return nil, nil
}

func (m *memoryScheduleRepo) CancelActiveByGoal(_ context.Context, goalID, userID uuid.UUID) error {
	for id, s := range m.schedules {
		if s.GoalID == goalID && s.UserID == userID && s.IsActive {
			s.IsActive = false
			m.schedules[id] = s
			return nil
		}
	}
	return savingsschedule.ErrScheduleNotFound
}

func (m *memoryScheduleRepo) Update(_ context.Context, schedule *savingsschedule.SavingsSchedule) error {
	if _, ok := m.schedules[schedule.ID]; !ok {
		return savingsschedule.ErrScheduleNotFound
	}
	m.schedules[schedule.ID] = *schedule
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
func (m *memoryGoalRepo) ListByUser(context.Context, uuid.UUID, string, string) ([]savingsgoal.SavingsGoal, error) {
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
func (m *memoryGoalRepo) Restore(context.Context, uuid.UUID, uuid.UUID) error    { return nil }
func (m *memoryGoalRepo) GetByIDIncludingDeleted(_ context.Context, id uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	return m.GetByID(context.Background(), id)
}
func (m *memoryGoalRepo) ListDeletedOlderThan(context.Context, time.Time) ([]savingsgoal.SavingsGoal, error) {
	return nil, nil
}
func (m *memoryGoalRepo) HardDelete(context.Context, uuid.UUID) error { return nil }
func (m *memoryGoalRepo) SumVaultBalance(context.Context, uuid.UUID, string) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *memoryGoalRepo) MarkCompleted(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (m *memoryGoalRepo) SumRecentDeposits(context.Context, uuid.UUID, string, time.Time) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *memoryGoalRepo) UpdateStatus(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (m *memoryGoalRepo) RecordGoalDeposits(context.Context, []savingsgoal.GoalDeposit) error {
	return nil
}
func (m *memoryGoalRepo) SumGoalDeposits(context.Context, uuid.UUID) (decimal.Decimal, error) {
	return decimal.Zero, nil
}
func (m *memoryGoalRepo) ListContributions(_ context.Context, _, _ uuid.UUID, _ interface{}) ([]savingsgoal.GoalContribution, int, string, error) {
	return nil, 0, "", nil
}
func (m *memoryGoalRepo) UpdateMilestones(_ context.Context, goalID uuid.UUID, milestones []int) error {
	g, ok := m.goals[goalID]
	if !ok {
		return savingsgoal.ErrGoalNotFound
	}
	g.NotifiedMilestones = append([]int(nil), milestones...)
	m.goals[goalID] = g
	return nil
}
func (m *memoryGoalRepo) UpdateDeadlineReminders(_ context.Context, goalID uuid.UUID, reminders []int) error {
	g, ok := m.goals[goalID]
	if !ok {
		return savingsgoal.ErrGoalNotFound
	}
	g.DeadlineRemindersSent = append([]int(nil), reminders...)
	m.goals[goalID] = g
	return nil
}
func (m *memoryGoalRepo) ListActiveApproachingDeadline(context.Context, int) ([]savingsgoal.SavingsGoal, error) {
	return nil, nil
}
func (m *memoryGoalRepo) SetShareToken(_ context.Context, goalID, userID uuid.UUID, token uuid.UUID) error {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	g.ShareToken = &token
	g.IsShared = true
	m.goals[goalID] = g
	return nil
}
func (m *memoryGoalRepo) ClearShareToken(_ context.Context, goalID, userID uuid.UUID) error {
	g, ok := m.goals[goalID]
	if !ok || g.UserID != userID {
		return savingsgoal.ErrGoalNotFound
	}
	g.ShareToken = nil
	g.IsShared = false
	m.goals[goalID] = g
	return nil
}
func (m *memoryGoalRepo) GetByShareToken(_ context.Context, token uuid.UUID) (*savingsgoal.SavingsGoal, error) {
	for _, g := range m.goals {
		if g.ShareToken != nil && *g.ShareToken == token {
			return &g, nil
		}
	}
	return nil, savingsgoal.ErrGoalNotFound
}

func (m *memoryGoalRepo) UpdateOnchainLink(_ context.Context, goalID uuid.UUID, onchainGoalID, onchainStatus string) error {
	g, ok := m.goals[goalID]
	if !ok {
		return savingsgoal.ErrGoalNotFound
	}
	g.OnchainGoalID = &onchainGoalID
	g.OnchainStatus = &onchainStatus
	m.goals[goalID] = g
	return nil
}

type memoryVaultRepo struct {
	vaults map[uuid.UUID]vault.Vault
}

func (m *memoryVaultRepo) CreateVault(_ context.Context, v vault.Vault) (vault.Vault, error) {
	m.vaults[v.ID] = v
	return v, nil
}
func (m *memoryVaultRepo) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	v, ok := m.vaults[id]
	if !ok {
		return vault.Vault{}, vault.ErrVaultNotFound
	}
	return v, nil
}
func (m *memoryVaultRepo) ListUserVaults(context.Context, uuid.UUID, vault.UserListFilter) ([]vault.Vault, int, error) {
	return nil, 0, nil
}
func (m *memoryVaultRepo) ListVaults(context.Context, vault.ListFilter) ([]vault.Vault, int, error) {
	return nil, 0, nil
}
func (m *memoryVaultRepo) RecordDeposit(context.Context, uuid.UUID, vault.TransactionRecord) error {
	return nil
}
func (m *memoryVaultRepo) UpdateVaultBalances(_ context.Context, id uuid.UUID, td, cb decimal.Decimal) error {
	v, ok := m.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	v.TotalDeposited = td
	v.CurrentBalance = cb
	m.vaults[id] = v
	return nil
}
func (m *memoryVaultRepo) ReplaceAllocations(context.Context, uuid.UUID, []vault.Allocation) error {
	return nil
}
func (m *memoryVaultRepo) UpdateVault(_ context.Context, id uuid.UUID, addr string, status vault.VaultStatus) error {
	v, ok := m.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	v.ContractAddress = addr
	v.Status = status
	m.vaults[id] = v
	return nil
}
func (m *memoryVaultRepo) UpdateHarvestFrequency(_ context.Context, id uuid.UUID, frequency string) error {
	v, ok := m.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	v.HarvestFrequency = frequency
	m.vaults[id] = v
	return nil
}
func (m *memoryVaultRepo) RecordWithdrawal(context.Context, uuid.UUID, vault.TransactionRecord) error {
	return nil
}
func (m *memoryVaultRepo) RecordHarvest(context.Context, vault.HarvestRecordInput) error {
	return nil
}
func (m *memoryVaultRepo) SoftDeleteVault(_ context.Context, id uuid.UUID) error {
	delete(m.vaults, id)
	return nil
}
func (m *memoryVaultRepo) ListDeposits(context.Context, uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, nil
}
func (m *memoryVaultRepo) RecordRebalance(context.Context, vault.RebalanceRecordInput, vault.TransactionRecord, vault.TransactionRecord) error {
	return nil
}

func (m *memoryVaultRepo) ListUserVaultTransactions(context.Context, uuid.UUID, uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, nil
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

func TestSavingsScheduleService_Create_RejectsArchivedGoal(t *testing.T) {
	userID := uuid.New()
	goalID := uuid.New()
	vaultID := uuid.New()

	svc := NewSavingsScheduleService(
		&memoryScheduleRepo{schedules: map[uuid.UUID]savingsschedule.SavingsSchedule{}},
		&memoryGoalRepo{goals: map[uuid.UUID]savingsgoal.SavingsGoal{
			goalID: {ID: goalID, UserID: userID, Status: savingsgoal.GoalStatusArchived},
		}},
		&memoryVaultRepo{vaults: map[uuid.UUID]vault.Vault{
			vaultID: {ID: vaultID, UserID: userID},
		}},
		decimal.Zero,
	)
	_, err := svc.Create(context.Background(), userID, goalID, CreateSavingsScheduleInput{
		Amount:    decimal.RequireFromString("10"),
		Frequency: "monthly",
		VaultID:   vaultID,
	})
	if !errors.Is(err, savingsgoal.ErrGoalArchived) {
		t.Fatalf("Create() error = %v, want ErrGoalArchived", err)
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
