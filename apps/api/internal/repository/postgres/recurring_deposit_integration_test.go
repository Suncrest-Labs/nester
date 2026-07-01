package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/scheduler"
)

// directDepositRecorder satisfies scheduler.DepositRecorder via the postgres
// repository directly — no import of the service package (avoids import cycle).
type directDepositRecorder struct{ repo *VaultRepository }

func (d *directDepositRecorder) RecordScheduledDeposit(ctx context.Context, userID, vaultID uuid.UUID, amount decimal.Decimal, scheduleID uuid.UUID) error {
	return d.repo.RecordDeposit(ctx, vaultID, vault.TransactionRecord{
		UserID:               userID,
		Amount:               amount,
		TransactionHash:      fmt.Sprintf("scheduled-%s", scheduleID),
		SharesMintedOrBurned: decimal.Zero,
		SharePriceAtTime:     decimal.NewFromInt(1),
	})
}

// directGoalProgressChecker satisfies scheduler.GoalProgressChecker.
type directGoalProgressChecker struct{ repo *SavingsGoalRepository }

func (d *directGoalProgressChecker) IsGoalCompleted(ctx context.Context, goalID, userID uuid.UUID) (bool, string, error) {
	goal, err := d.repo.GetByID(ctx, goalID)
	if err != nil {
		return false, "", err
	}
	if goal.UserID != userID {
		return false, "", savingsgoal.ErrGoalNotFound
	}
	bal, err := d.repo.SumVaultBalance(ctx, userID, string(goal.Currency))
	if err != nil {
		return false, "", err
	}
	name := goal.Description
	if name == "" {
		name = "your goal"
	}
	return bal.GreaterThanOrEqual(goal.TargetAmount), name, nil
}

func (d *directGoalProgressChecker) IsGoalPausedOrArchived(ctx context.Context, goalID, userID uuid.UUID) (bool, error) {
	goal, err := d.repo.GetByID(ctx, goalID)
	if err != nil {
		return false, err
	}
	if goal.UserID != userID {
		return false, savingsgoal.ErrGoalNotFound
	}
	return goal.Status == savingsgoal.GoalStatusPaused || goal.Status == savingsgoal.GoalStatusArchived, nil
}

func applySavingsScheduleMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	applyIntegrationMigrations(t, db)
	for _, name := range []string{
		"026_create_savings_goals.up.sql",
		"037_add_savings_goal_category.up.sql",
		"039_create_savings_schedules.up.sql",
		"053_add_savings_goal_vault_id.up.sql",
	} {
		applyMigrationFile(t, db, name)
	}
}

func applyMigrationFile(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "migrations", name)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if _, err := db.Exec(string(contents)); err != nil {
		t.Fatalf("applying migration %q failed: %v", name, err)
	}
}

func seedIntegrationSavingsGoal(t *testing.T, db *sql.DB, userID uuid.UUID) uuid.UUID {
	t.Helper()
	goalID := uuid.New()
	deadline := time.Now().Add(365 * 24 * time.Hour).UTC()
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO savings_goals (id, user_id, target_amount, currency, deadline, description, category)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		goalID.String(),
		userID.String(),
		"1000",
		"USDC",
		deadline,
		"Vacation Fund",
		"travel",
	)
	if err != nil {
		t.Fatalf("seed savings goal: %v", err)
	}
	return goalID
}

func TestRecurringDepositJobIntegration(t *testing.T) {
	db := openIntegrationDB(t)
	applySavingsScheduleMigrations(t, db)
	resetSavingsScheduleTables(t, db)

	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)
	goalID := seedIntegrationSavingsGoal(t, db, userID)

	scheduleRepo := NewSavingsScheduleRepository(db)
	vaultRepo := NewVaultRepository(db)
	goalRepo := NewSavingsGoalRepository(db)

	past := time.Now().UTC().Add(-2 * time.Hour)
	schedule := &savingsschedule.SavingsSchedule{
		ID:        uuid.New(),
		UserID:    userID,
		GoalID:    goalID,
		VaultID:   vaultID,
		Amount:    decimal.RequireFromString("50"),
		Currency:  "USDC",
		Frequency: savingsschedule.FrequencyWeekly,
		NextRunAt: past,
		IsActive:  true,
	}
	if err := scheduleRepo.Create(ctx, schedule); err != nil {
		t.Fatalf("Create schedule: %v", err)
	}

	now := time.Now().UTC()
	depositSvc := &directDepositRecorder{repo: vaultRepo}
	goalProgressSvc := &directGoalProgressChecker{repo: goalRepo}

	job := scheduler.NewRecurringDepositJob(
		scheduler.RecurringDepositConfig{Enabled: true},
		scheduleRepo,
		depositSvc,
		goalProgressSvc,
		nil,
		nil,
	)
	job.SetClock(func() time.Time { return now })
	job.Tick(ctx)

	v, err := vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		t.Fatalf("GetVault: %v", err)
	}
	if !v.TotalDeposited.Equal(decimal.RequireFromString("50")) {
		t.Fatalf("total deposited = %s, want 50", v.TotalDeposited)
	}

	updated, err := scheduleRepo.GetByID(ctx, schedule.ID)
	if err != nil {
		t.Fatalf("GetByID schedule: %v", err)
	}
	if updated.LastRunAt == nil {
		t.Fatal("expected last_run_at to be set")
	}
	if !updated.NextRunAt.After(now) {
		t.Fatalf("next_run_at = %v, expected after %v", updated.NextRunAt, now)
	}
	wantNext := scheduler.NextRunAt(now, "weekly")
	if !updated.NextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at = %v, want %v", updated.NextRunAt, wantNext)
	}
}

func resetSavingsScheduleTables(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE savings_schedules, savings_goals, settlements, allocations, vaults, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}
}
