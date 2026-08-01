package service

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
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

// applySavingsGoalLifecycleMigrations applies every migration needed for the
// full current savings_goals schema (status/completion, name/emoji, share
// token, vault linking, auto-compound) so GetByID/List round-trip cleanly.
func applySavingsGoalLifecycleMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	applySavingsGoalIntegrationMigrations(t, db)
	for _, name := range []string{
		"026_create_savings_goals.up.sql",
		"029_create_device_tokens.up.sql",
		"037_add_savings_goal_category.up.sql",
		"038_add_savings_goal_notified_milestones.up.sql",
		"040_savings_goal_velocity_pause_completion.up.sql",
		"041_add_savings_goal_archived_status.up.sql",
		"045_add_savings_goal_name_emoji.up.sql",
		"052_add_savings_goal_share_token.up.sql",
		"053_add_savings_goal_icon_color.up.sql",
		"054_add_savings_goal_vault_id.up.sql",
		"055_add_savings_goal_auto_compound.up.sql",
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

// TestSavingsGoalDepositMilestoneCompletionLifecycle_Integration exercises the
// full deposit -> milestone -> completion lifecycle end to end: a goal linked
// to a vault progresses through 25/50/75/100% as its vault balance grows,
// each crossing fires a milestone notification (side effect verified via the
// notifications dispatcher), and reaching 100% auto-completes the goal.
func TestSavingsGoalDepositMilestoneCompletionLifecycle_Integration(t *testing.T) {
	db := openSavingsGoalIntegrationDB(t)
	applySavingsGoalLifecycleMigrations(t, db)
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
		ContractAddress: "CA-LIFECYCLE",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	if _, err := notificationRepo.UpsertDeviceToken(ctx, userID, "ExponentPushToken[lifecycle]", "expo"); err != nil {
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
		Description:  "New laptop",
		VaultID:      &createdVault.ID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if goal.Status != savingsgoal.GoalStatusActive && goal.Status != "" {
		t.Fatalf("initial goal status = %q, want active", goal.Status)
	}

	// Deposit in four steps, crossing each of the 25/50/75/100% milestones.
	for _, balance := range []int64{25, 50, 75, 100} {
		if err := vaultRepo.UpdateVaultBalances(ctx, createdVault.ID, decimal.NewFromInt(balance), decimal.NewFromInt(balance)); err != nil {
			t.Fatalf("UpdateVaultBalances(%d) error = %v", balance, err)
		}
		if _, err := svc.Get(ctx, userID, goal.ID); err != nil {
			t.Fatalf("Get() after depositing %d error = %v", balance, err)
		}
	}

	// Notifications are dispatched asynchronously (goroutine per milestone);
	// wait for all four to land rather than racing the assertions.
	deadlineWait := time.After(3 * time.Second)
	for {
		if push.CallCount() >= 4 && persistence.Count() >= 4 {
			break
		}
		select {
		case <-deadlineWait:
			t.Fatalf("timed out waiting for milestone notifications; push=%d persisted=%d", push.CallCount(), persistence.Count())
		default:
			time.Sleep(20 * time.Millisecond)
		}
	}

	// Completion: reaching the target auto-completes the goal (#716).
	final, err := svc.Get(ctx, userID, goal.ID)
	if err != nil {
		t.Fatalf("Get() final error = %v", err)
	}
	if final.Status != savingsgoal.GoalStatusCompleted {
		t.Fatalf("final status = %q, want %q", final.Status, savingsgoal.GoalStatusCompleted)
	}
	if final.CompletedAt == nil {
		t.Fatal("final CompletedAt = nil, want non-nil after auto-completion")
	}
	if final.ProgressPct != 100 {
		t.Fatalf("final progress_pct = %v, want 100", final.ProgressPct)
	}

	// Milestone side effects persisted on the goal row.
	stored, err := goalRepo.GetByID(ctx, goal.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	for _, milestone := range []int{25, 50, 75, 100} {
		if !savingsgoal.ContainsMilestone(stored.NotifiedMilestones, milestone) {
			t.Fatalf("notified_milestones = %v, want to contain %d", stored.NotifiedMilestones, milestone)
		}
	}
	if stored.Status != savingsgoal.GoalStatusCompleted {
		t.Fatalf("persisted status = %q, want %q", stored.Status, savingsgoal.GoalStatusCompleted)
	}

	// Notification side effects: one push per milestone, titled correctly.
	wantTitles := map[string]bool{
		"Great start!":     false,
		"Halfway there!":   false,
		"Almost there!":    false,
		"Goal achieved! 🎉": false,
	}
	for _, call := range push.SnapshotCalls() {
		if _, ok := wantTitles[call.Title]; ok {
			wantTitles[call.Title] = true
		}
	}
	for title, seen := range wantTitles {
		if !seen {
			t.Fatalf("missing push notification for milestone titled %q", title)
		}
	}
	if persistence.Count() < 4 {
		t.Fatalf("persisted notification count = %d, want >= 4", persistence.Count())
	}
}
