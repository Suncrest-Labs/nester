package service

import (
	"context"
	"database/sql"
	"fmt"
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

	// Crossing 0% -> 50% trips two milestones, 25 and 50, and
	// notifyMilestonesAsync dispatches one detached goroutine per milestone.
	// They race, so waiting for "at least one notification" can return as soon
	// as the 25% one lands and leave the 50% assertions below reading state
	// that has not been written yet. Wait for the specific condition this test
	// actually asserts on instead of for a count.
	waitFor(t, 10*time.Second, func() bool {
		return pushCallForMilestone(push, 50) && persistence.Count() >= 2
	}, func() string {
		return fmt.Sprintf("push=%d persisted=%d calls=%+v",
			push.CallCount(), persistence.Count(), push.SnapshotCalls())
	})

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

// waitFor polls until done returns true or the timeout elapses.
//
// Polling rather than a fixed sleep because the work being waited on is a
// detached goroutine with no completion signal to synchronise on; describe is
// only evaluated on failure, so the message reports the state at the moment
// the wait gave up rather than a stale snapshot.
func waitFor(t *testing.T, timeout time.Duration, done func() bool, describe func() string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if done() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %s waiting for notifications; %s", timeout, describe())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pushCallForMilestone reports whether a push for the given milestone has been
// recorded. Matched on the payload rather than the title so the check does not
// break when copy is reworded.
func pushCallForMilestone(push *notifications.RecordingPushSender, milestone int) bool {
	for _, call := range push.SnapshotCalls() {
		if value, ok := call.Payload["milestone"]; ok {
			if asInt, ok := value.(int); ok && asInt == milestone {
				return true
			}
		}
	}
	return false
}
