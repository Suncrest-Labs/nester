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

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/scheduler"
)

// directDepositRecorder satisfies scheduler.DepositRecorder via the postgres
// repository directly — no import of the service package (avoids import cycle).
// The transaction hash folds in occurrenceAt (the schedule's NextRunAt for
// this occurrence) rather than just scheduleID, mirroring the #846 fix in
// service.ScheduledDepositService.RecordScheduledDeposit — see that
// function's doc comment for why a hash constant across every occurrence of
// a schedule silently broke all deposits after the first.
type directDepositRecorder struct{ repo *VaultRepository }

func (d *directDepositRecorder) RecordScheduledDeposit(ctx context.Context, userID, vaultID uuid.UUID, amount decimal.Decimal, scheduleID uuid.UUID, occurrenceAt time.Time) error {
	return d.repo.RecordDeposit(ctx, vaultID, vault.TransactionRecord{
		UserID:               userID,
		Amount:               amount,
		TransactionHash:      fmt.Sprintf("scheduled-%s-%d", scheduleID, occurrenceAt.UTC().Unix()),
		SharesMintedOrBurned: decimal.Zero,
		SharePriceAtTime:     decimal.NewFromInt(1),
	})
}

// capturingEnqueuer stands in for the real jobqueue.Client in this
// integration test: it records the EnqueueInput the sweep loop produces so
// the test can hand it directly to a RecurringDepositJobHandler, simulating
// what jobqueue.Worker would do when it dequeues and dispatches the job —
// without needing the full worker/lease machinery for this test's purpose
// (proving the sweep -> enqueue -> handler -> ledger/schedule path is wired
// correctly end to end against a real Postgres instance).
type capturingEnqueuer struct {
	calls []jobqueue.EnqueueInput
}

func (c *capturingEnqueuer) Enqueue(_ context.Context, in jobqueue.EnqueueInput) (jobqueue.Job, error) {
	c.calls = append(c.calls, in)
	return jobqueue.Job{ID: uuid.New(), Type: in.Type, Payload: in.Payload}, nil
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
	queue := &capturingEnqueuer{}

	job := scheduler.NewRecurringDepositJob(
		scheduler.RecurringDepositConfig{Enabled: true},
		scheduleRepo,
		queue,
		goalProgressSvc,
		nil,
	)
	job.SetClock(func() time.Time { return now })
	job.Tick(ctx)

	// The sweep loop only enqueues; it does not touch the ledger or the
	// schedule itself (that happens in the handler below, mirroring how
	// main.go wires jobWorker.Register + jobWorker.Run to actually execute
	// enqueued jobs).
	if len(queue.calls) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(queue.calls))
	}
	handler := scheduler.NewRecurringDepositJobHandler(depositSvc, scheduleRepo, nil, nil)
	if err := handler.Handle(ctx, jobqueue.Job{Type: queue.calls[0].Type, Payload: queue.calls[0].Payload}); err != nil {
		t.Fatalf("handler.Handle: %v", err)
	}

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
	// The handler advances NextRunAt from the occurrence's own due time
	// (schedule.NextRunAt, i.e. `past`) rather than from "now" — each
	// occurrence's next run is computed relative to when it was due, not
	// relative to whenever the async job happened to execute.
	wantNext := scheduler.NextRunAt(past, "weekly")
	if !updated.NextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at = %v, want %v", updated.NextRunAt, wantNext)
	}
}

// TestRecurringDepositJobIntegration_SecondOccurrenceSucceeds is the
// regression test for the #846 idempotency bug: previously
// service.ScheduledDepositService built the ledger transaction hash from
// scheduleID alone, which is constant across every occurrence of the same
// schedule. Since vault_transactions.transaction_hash has a UNIQUE index
// and RecordDeposit does a bare INSERT with no ON CONFLICT, only a
// schedule's FIRST-ever occurrence could ever be recorded — every later
// occurrence hit the unique-constraint violation and silently failed
// forever. This test runs the sweep -> enqueue -> handler path twice, for
// two distinct occurrences of the same schedule, and asserts BOTH deposits
// land (total_deposited reflects both, not just the first).
func TestRecurringDepositJobIntegration_SecondOccurrenceSucceeds(t *testing.T) {
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

	occurrence1 := time.Now().UTC().Add(-8 * 24 * time.Hour) // due a week+ago
	schedule := &savingsschedule.SavingsSchedule{
		ID:        uuid.New(),
		UserID:    userID,
		GoalID:    goalID,
		VaultID:   vaultID,
		Amount:    decimal.RequireFromString("50"),
		Currency:  "USDC",
		Frequency: savingsschedule.FrequencyWeekly,
		NextRunAt: occurrence1,
		IsActive:  true,
	}
	if err := scheduleRepo.Create(ctx, schedule); err != nil {
		t.Fatalf("Create schedule: %v", err)
	}

	depositSvc := &directDepositRecorder{repo: vaultRepo}
	goalProgressSvc := &directGoalProgressChecker{repo: goalRepo}
	handler := scheduler.NewRecurringDepositJobHandler(depositSvc, scheduleRepo, nil, nil)

	runOnce := func(clock time.Time) {
		queue := &capturingEnqueuer{}
		job := scheduler.NewRecurringDepositJob(scheduler.RecurringDepositConfig{Enabled: true}, scheduleRepo, queue, goalProgressSvc, nil)
		job.SetClock(func() time.Time { return clock })
		job.Tick(ctx)
		if len(queue.calls) != 1 {
			t.Fatalf("expected 1 enqueued job at clock=%v, got %d", clock, len(queue.calls))
		}
		if err := handler.Handle(ctx, jobqueue.Job{Type: queue.calls[0].Type, Payload: queue.calls[0].Payload}); err != nil {
			t.Fatalf("handler.Handle at clock=%v: %v", clock, err)
		}
	}

	// First occurrence.
	runOnce(time.Now().UTC())

	v, err := vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		t.Fatalf("GetVault after first occurrence: %v", err)
	}
	if !v.TotalDeposited.Equal(decimal.RequireFromString("50")) {
		t.Fatalf("total deposited after first occurrence = %s, want 50", v.TotalDeposited)
	}

	// The schedule has now advanced to its next NextRunAt (~occurrence1+7d);
	// simulate that also being due by ticking again from a clock far enough
	// in the future.
	runOnce(occurrence1.AddDate(0, 0, 8))

	v, err = vaultRepo.GetVault(ctx, vaultID)
	if err != nil {
		t.Fatalf("GetVault after second occurrence: %v", err)
	}
	if !v.TotalDeposited.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("total deposited after second occurrence = %s, want 100 (both occurrences must land — this is the #846 regression check)", v.TotalDeposited)
	}
}

func resetSavingsScheduleTables(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE TABLE savings_schedules, savings_goals, settlements, allocations, vaults, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}
}
