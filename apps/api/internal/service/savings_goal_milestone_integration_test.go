package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/jobqueue"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"

	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

func applySavingsGoalMilestoneMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	// The base helper now applies the complete migration chain, so the
	// per-feature additions this function used to layer on are covered.
	applySavingsGoalIntegrationMigrations(t, db)
}

func applySavingsGoalIntegrationMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	// The full migration chain in numeric order — see testutil.ApplyAllMigrations
	// for why no per-test subset is used.
	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "migrations"))
}

func openSavingsGoalIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedSavingsGoalIntegrationUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, wallet_address, display_name) VALUES ($1, $2, $3)`,
		userID.String(),
		"G"+userID.String(), // final schema: wallet_address NOT NULL UNIQUE, email dropped by 010
		"Integration User",
	); err != nil {
		t.Fatalf("seed user failed: %v", err)
	}
	return userID
}

func TestSavingsGoalMilestoneIntegration(t *testing.T) {
	db := openSavingsGoalIntegrationDB(t)
	applySavingsGoalMilestoneMigrations(t, db)
	if _, err := db.Exec(`TRUNCATE TABLE savings_goals, allocations, vaults, users, device_tokens, outbox, jobs RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}

	ctx := context.Background()
	userID := seedSavingsGoalIntegrationUser(t, db)
	vaultRepo := postgres.NewVaultRepository(db)
	goalRepo := postgres.NewSavingsGoalRepository(db)
	notificationRepo := postgres.NewNotificationRepository(db)

	createdVault, err := vaultRepo.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-MILESTONE",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	if _, err := notificationRepo.UpsertDeviceToken(ctx, userID, "ExponentPushToken[milestone]", "expo"); err != nil {
		t.Fatalf("UpsertDeviceToken() error = %v", err)
	}

	push := &notifications.RecordingPushSender{}
	persistence := &notifications.RecordingPersistenceStore{}
	dispatcher := notifications.New(
		[]notifications.Channel{notifications.NewPushChannel(push, notificationRepo)},
		notificationRepo,
		persistence,
	)
	svc := NewSavingsGoalService(goalRepo, vaultRepo, DispatcherGoalMilestoneNotifier{Dispatcher: dispatcher})

	deadline := time.Now().UTC().Add(365 * 24 * time.Hour)
	goal, err := svc.Create(ctx, userID, CreateSavingsGoalInput{
		TargetAmount: decimal.NewFromInt(100),
		Currency:     "USDC",
		Deadline:     deadline,
		Description:  "Rainy day fund",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := vaultRepo.UpdateVaultBalances(ctx, createdVault.ID, decimal.NewFromInt(50), decimal.NewFromInt(50)); err != nil {
		t.Fatalf("UpdateVaultBalances() error = %v", err)
	}

	enriched, err := svc.Get(ctx, userID, goal.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if enriched.ProgressPct < 50 {
		t.Fatalf("progress_pct = %v, want >= 50", enriched.ProgressPct)
	}

	// --- the crash window ---
	//
	// The milestone is recorded and the intent to notify is durable, but
	// nothing has been delivered: delivery is the relay's job now. This is
	// exactly the instant the old code lost notifications, because its
	// notify-goroutine died with the process. Assert on it directly.
	if push.CallCount() != 0 {
		t.Fatalf("push sent %d notifications inline, want 0 — delivery belongs to the relay", push.CallCount())
	}
	if n := pendingOutboxCount(t, db); n != 2 {
		t.Fatalf("undelivered outbox events = %d, want 2 (25%% and 50%%)", n)
	}

	// --- restart ---
	//
	// A fresh relay and worker, as a restarted process would have. The
	// notification the crash "lost" is delivered from the durable row.
	harness := newOutboxHarness(t, db, map[string]jobqueue.Handler{
		GoalMilestoneJobType: NewGoalMilestoneJobHandler(
			DispatcherGoalMilestoneNotifier{Dispatcher: dispatcher}, nil),
	})
	harness.drain(ctx, t, 8)

	if push.CallCount() < 1 || persistence.Count() < 1 {
		t.Fatalf("after restart: push=%d persisted=%d, want at least 1 of each", push.CallCount(), persistence.Count())
	}
	if n := pendingOutboxCount(t, db); n != 0 {
		t.Fatalf("undelivered outbox events after drain = %d, want 0", n)
	}
	if n := dispatchedOutboxCount(t, db); n != 2 {
		t.Fatalf("dispatched outbox events = %d, want 2", n)
	}

	stored, err := goalRepo.GetByID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if !savingsgoal.ContainsMilestone(stored.NotifiedMilestones, 25) ||
		!savingsgoal.ContainsMilestone(stored.NotifiedMilestones, 50) {
		t.Fatalf("notified_milestones = %v, want 25 and 50", stored.NotifiedMilestones)
	}

	calls := push.SnapshotCalls()
	if len(calls) == 0 {
		t.Fatal("expected push notification record")
	}
	found50 := false
	for _, call := range calls {
		if call.Title == "Halfway there!" {
			found50 = true
		}
	}
	if !found50 {
		t.Fatalf("push calls = %+v, want 50%% milestone notification", calls)
	}
}
