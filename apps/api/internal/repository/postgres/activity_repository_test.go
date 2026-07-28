package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/activity"
)

func seedActivityFixtures(t *testing.T, db *sql.DB, userID, vaultID uuid.UUID) {
	t.Helper()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	mustExec(t, db, `
		INSERT INTO vault_transactions (id, vault_id, user_id, type, amount, transaction_hash, memo, created_at)
		VALUES ($1, $2, $3, 'deposit', 100, 'txhash-deposit', 'first paycheck deposit', $4)
	`, uuid.New().String(), vaultID.String(), userID.String(), base.Add(1*time.Hour))

	mustExec(t, db, `
		INSERT INTO vault_transactions (id, vault_id, user_id, type, amount, transaction_hash, created_at)
		VALUES ($1, $2, $3, 'withdrawal', 40, 'txhash-withdrawal', $4)
	`, uuid.New().String(), vaultID.String(), userID.String(), base.Add(2*time.Hour))

	mustExec(t, db, `
		INSERT INTO vault_transactions (id, vault_id, user_id, type, amount, transaction_hash, created_at)
		VALUES ($1, $2, $3, 'rebalance', 15, 'txhash-rebalance', $4)
	`, uuid.New().String(), vaultID.String(), userID.String(), base.Add(3*time.Hour))

	mustExec(t, db, `
		INSERT INTO settlements (
			id, user_id, vault_id, amount, currency, fiat_currency, fiat_amount, exchange_rate,
			destination_type, destination_provider, destination_account_number, destination_account_name,
			status, notes, created_at
		) VALUES ($1, $2, $3, 25, 'USDC', 'NGN', 37500, 1500,
			'bank_transfer', 'bank', '0123456789', 'Ada Lovelace', 'confirmed', 'rent settlement payout', $4)
	`, uuid.New().String(), userID.String(), vaultID.String(), base.Add(4*time.Hour))

	mustExec(t, db, `
		INSERT INTO yield_harvests (id, user_id, vault_id, amount, currency, harvested_at)
		VALUES ($1, $2, $3, 5, 'USDC', $4)
	`, uuid.New().String(), userID.String(), vaultID.String(), base.Add(5*time.Hour))
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("seed exec failed (%s): %v", query, err)
	}
}

func TestActivityRepositoryIntegration_UnionAndOrdering(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)
	seedActivityFixtures(t, db, userID, vaultID)

	repo := NewActivityRepository(db)
	items, next, prev, err := repo.List(context.Background(), userID, activity.ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("got %d items, want 5 (deposit, withdrawal, rebalance, settlement, yield_earned)", len(items))
	}
	if next != "" || prev != "" {
		t.Fatalf("expected no cursors when the whole feed fits on one page, got next=%q prev=%q", next, prev)
	}

	// Most recent first: yield_earned (base+5h) ... deposit (base+1h).
	wantOrder := []activity.EventType{
		activity.EventYieldEarned,
		activity.EventSettlement,
		activity.EventRebalance,
		activity.EventWithdrawal,
		activity.EventDeposit,
	}
	for i, want := range wantOrder {
		if items[i].Type != want {
			t.Fatalf("item[%d].Type = %q, want %q (full order: %+v)", i, items[i].Type, want, items)
		}
	}

	for _, it := range items {
		if it.VaultName != "USDC Vault" {
			t.Fatalf("VaultName = %q, want fallback %q (vaults.name is NULL, COALESCE to currency + \" Vault\")", it.VaultName, "USDC Vault")
		}
	}
}

func TestActivityRepositoryIntegration_TypeFilter(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)
	seedActivityFixtures(t, db, userID, vaultID)

	repo := NewActivityRepository(db)
	items, _, _, err := repo.List(context.Background(), userID, activity.ListFilter{
		Limit: 10,
		Types: []activity.EventType{activity.EventDeposit, activity.EventSettlement},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (deposit + settlement only)", len(items))
	}
	for _, it := range items {
		if it.Type != activity.EventDeposit && it.Type != activity.EventSettlement {
			t.Fatalf("unexpected type in filtered result: %q", it.Type)
		}
	}
}

func TestActivityRepositoryIntegration_FullTextSearchUsesGIN(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)
	seedActivityFixtures(t, db, userID, vaultID)

	repo := NewActivityRepository(db)
	items, _, _, err := repo.List(context.Background(), userID, activity.ListFilter{
		Limit:  10,
		Search: "paycheck",
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 || items[0].Type != activity.EventDeposit {
		t.Fatalf("search %q got %+v, want exactly the deposit with memo containing 'paycheck'", "paycheck", items)
	}

	rows, err := db.QueryContext(context.Background(),
		`EXPLAIN (FORMAT JSON) SELECT id FROM vault_transactions WHERE user_id = $1 AND search_vector @@ plainto_tsquery('english', $2)`,
		userID.String(), "paycheck",
	)
	if err != nil {
		t.Fatalf("EXPLAIN query failed: %v", err)
	}
	defer rows.Close()
	var plan string
	for rows.Next() {
		if err := rows.Scan(&plan); err != nil {
			t.Fatalf("scan EXPLAIN output: %v", err)
		}
	}
	if !strings.Contains(plan, "idx_vault_transactions_search_vector") && !strings.Contains(strings.ToLower(plan), "bitmap index scan") {
		t.Fatalf("expected the search_vector GIN index to be used, got plan: %s", plan)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(plan), &parsed); err != nil {
		t.Fatalf("EXPLAIN output is not valid JSON: %v", err)
	}
}

func TestActivityRepositoryIntegration_KeysetPaginationForwardAndBackward(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)
	seedActivityFixtures(t, db, userID, vaultID)

	repo := NewActivityRepository(db)
	ctx := context.Background()

	// Walk forward two at a time and collect every item, then confirm no
	// row is skipped or duplicated across page boundaries.
	var forward []activity.Item
	cursor := ""
	for i := 0; i < 10; i++ {
		items, next, _, err := repo.List(ctx, userID, activity.ListFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("List() forward page %d error = %v", i, err)
		}
		if len(items) == 0 {
			break
		}
		forward = append(forward, items...)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(forward) != 5 {
		t.Fatalf("forward walk collected %d items, want 5", len(forward))
	}
	seen := map[uuid.UUID]bool{}
	for _, it := range forward {
		if seen[it.ID] {
			t.Fatalf("item %s returned twice during forward walk", it.ID)
		}
		seen[it.ID] = true
	}

	// Fetch page 2 (items[2:4] of the full order), then page backward from
	// its first row's Prev cursor and confirm it reconstructs page 1.
	page1, next1, _, err := repo.List(ctx, userID, activity.ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List() page1 error = %v", err)
	}
	page2, _, prev2, err := repo.List(ctx, userID, activity.ListFilter{Limit: 2, Cursor: next1})
	if err != nil {
		t.Fatalf("List() page2 error = %v", err)
	}
	if prev2 == "" {
		t.Fatal("expected page2 to carry a prev cursor back to page1")
	}
	back, _, _, err := repo.List(ctx, userID, activity.ListFilter{Limit: 2, Cursor: prev2})
	if err != nil {
		t.Fatalf("List() backward-from-page2 error = %v", err)
	}
	if len(back) != len(page1) {
		t.Fatalf("backward page length = %d, want %d", len(back), len(page1))
	}
	for i := range back {
		if back[i].ID != page1[i].ID {
			t.Fatalf("backward page did not reconstruct page1 at index %d: got %s want %s", i, back[i].ID, page1[i].ID)
		}
	}
	_ = page2
}
