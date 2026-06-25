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

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
)

func applySavingsGoalMilestoneMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	applySavingsGoalIntegrationMigrations(t, db)
	for _, name := range []string{
		"026_create_savings_goals.up.sql",
		"029_create_device_tokens.up.sql",
		"037_add_savings_goal_category.up.sql",
		"038_add_savings_goal_notified_milestones.up.sql",
	} {
		path := filepath.Join("..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q failed: %v", name, err)
		}
	}
}

func applySavingsGoalIntegrationMigrations(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, name := range []string{
		"001_create_users_table.up.sql",
		"004_create_vaults_table.up.sql",
		"005_create_allocations_table.up.sql",
		"006_create_settlements_table.up.sql",
		"007_update_users_table.up.sql",
	} {
		path := filepath.Join("..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q failed: %v", name, err)
		}
	}
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
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(),
		userID.String()+"@example.com",
		"Integration User",
	); err != nil {
		t.Fatalf("seed user failed: %v", err)
	}
	return userID
}

func TestSavingsGoalMilestoneIntegration(t *testing.T) {
	db := openSavingsGoalIntegrationDB(t)
	applySavingsGoalMilestoneMigrations(t, db)
	if _, err := db.Exec(`TRUNCATE TABLE savings_goals, allocations, vaults, users, device_tokens RESTART IDENTITY CASCADE`); err != nil {
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
	svc := NewSavingsGoalService(goalRepo, DispatcherGoalMilestoneNotifier{Dispatcher: dispatcher})

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

	deadlineWait := time.After(2 * time.Second)
	for {
		if push.CallCount() >= 1 && persistence.Count() >= 1 {
			break
		}
		select {
		case <-deadlineWait:
			t.Fatalf("timed out waiting for notification; push=%d persisted=%d", push.CallCount(), persistence.Count())
		default:
			time.Sleep(20 * time.Millisecond)
		}
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
