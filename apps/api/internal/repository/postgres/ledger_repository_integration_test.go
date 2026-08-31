package postgres

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/ledger"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

func applyLedgerMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`
		DO $$
		DECLARE r record;
		BEGIN
			FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
				EXECUTE 'DROP TABLE IF EXISTS public.' || quote_ident(r.tablename) || ' CASCADE';
			END LOOP;
		END$$;
	`); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
	// Need vaults and users for FKs
	migrations := []string{
		"001_create_users_table.up.sql",
		"002_create_vaults_table.up.sql",
		"005_create_allocations_table.up.sql",
		// 014 alters settlements, so the table has to exist first.
		"006_create_settlements_table.up.sql",
		// resetIntegrationTables truncates settlements, allocations,
		// vault_transactions, yield_harvests, vaults and users, so every one
		// of them has to be created here or the TRUNCATE fails outright.
		"008_add_vault_transactions.up.sql",
		"014_add_missing_columns.up.sql",
		"042_create_yield_harvests.up.sql",
		"113_create_ledger_accounts.up.sql",
		"114_create_ledger_entries.up.sql",
		"115_create_ledger_balances.up.sql",
		"116_create_ledger_reconciliation.up.sql",
	}
	for _, name := range migrations {
		path := filepath.Join("..", "..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q: %v", name, err)
		}
	}
}

func TestLedger_Deposit_PostingIsAtomicAndBalanced(t *testing.T) {
	db := openIntegrationDB(t)
	applyLedgerMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewLedgerRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	// Get or create accounts
	userAcc, err := repo.GetOrCreateAccount(ctx, ledger.AccountTypeUserVaultPosition, &vaultID, &userID, nil, "USDC")
	if err != nil {
		t.Fatalf("GetOrCreateAccount user: %v", err)
	}
	vaultAcc, err := repo.GetOrCreateAccount(ctx, ledger.AccountTypeVaultAssetPool, &vaultID, nil, nil, "USDC")
	if err != nil {
		t.Fatalf("GetOrCreateAccount vault: %v", err)
	}
	suspenseAcc, err := repo.GetOrCreateAccount(ctx, ledger.AccountTypeSystemSuspense, nil, nil, nil, "USDC")
	if err != nil {
		t.Fatalf("GetOrCreateAccount suspense: %v", err)
	}

	amountStroops := int64(100_000_000) // 10 USDC
	txID := uuid.New()
	entries := []ledger.Entry{
		{TransactionID: txID, AccountID: userAcc.ID, Amount: amountStroops, Direction: "debit", AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit", DomainEventID: "test-deposit-1"},
		{TransactionID: txID, AccountID: vaultAcc.ID, Amount: amountStroops, Direction: "debit", AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit", DomainEventID: "test-deposit-1"},
		{TransactionID: txID, AccountID: suspenseAcc.ID, Amount: -2 * amountStroops, Direction: "credit", AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit", DomainEventID: "test-deposit-1"},
	}

	if err := repo.PostEntries(ctx, entries); err != nil {
		t.Fatalf("PostEntries deposit error = %v", err)
	}

	// Check balances
	userBal, err := repo.GetUserVaultBalance(ctx, userID, vaultID)
	if err != nil {
		t.Fatalf("GetUserVaultBalance error = %v", err)
	}
	if userBal != amountStroops {
		t.Fatalf("user balance: got %d, want %d", userBal, amountStroops)
	}

	vaultBal, err := repo.GetVaultPoolBalance(ctx, vaultID)
	if err != nil {
		t.Fatalf("GetVaultPoolBalance error = %v", err)
	}
	if vaultBal != amountStroops {
		t.Fatalf("vault pool balance: got %d, want %d", vaultBal, amountStroops)
	}

	// Books must sum to zero
	sum, err := repo.SumAllEntries(ctx)
	if err != nil {
		t.Fatalf("SumAllEntries error = %v", err)
	}
	if sum != 0 {
		t.Fatalf("books unbalanced: sum=%d", sum)
	}

	// Cached balances must match recomputed
	mismatches, err := repo.RecomputeBalances(ctx)
	if err != nil {
		t.Fatalf("RecomputeBalances error = %v", err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("balance mismatches: %+v", mismatches)
	}
}

func TestLedger_Withdraw_Posting(t *testing.T) {
	db := openIntegrationDB(t)
	applyLedgerMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewLedgerRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	// First deposit
	userAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeUserVaultPosition, &vaultID, &userID, nil, "USDC")
	vaultAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeVaultAssetPool, &vaultID, nil, nil, "USDC")
	suspenseAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeSystemSuspense, nil, nil, nil, "USDC")

	depositStroops := int64(100_000_000)
	txID := uuid.New()
	depositEntries := []ledger.Entry{
		{TransactionID: txID, AccountID: userAcc.ID, Amount: depositStroops, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
		{TransactionID: txID, AccountID: vaultAcc.ID, Amount: depositStroops, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
		{TransactionID: txID, AccountID: suspenseAcc.ID, Amount: -2 * depositStroops, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
	}
	if err := repo.PostEntries(ctx, depositEntries); err != nil {
		t.Fatalf("deposit: %v", err)
	}

	// Withdraw half
	withdrawStroops := int64(50_000_000)
	txID2 := uuid.New()
	withdrawEntries := []ledger.Entry{
		{TransactionID: txID2, AccountID: userAcc.ID, Amount: -withdrawStroops, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "withdraw"},
		{TransactionID: txID2, AccountID: vaultAcc.ID, Amount: -withdrawStroops, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "withdraw"},
		{TransactionID: txID2, AccountID: suspenseAcc.ID, Amount: 2 * withdrawStroops, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "withdraw"},
	}
	if err := repo.PostEntries(ctx, withdrawEntries); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	userBal, _ := repo.GetUserVaultBalance(ctx, userID, vaultID)
	if userBal != depositStroops-withdrawStroops {
		t.Fatalf("user balance after withdraw: got %d, want %d", userBal, depositStroops-withdrawStroops)
	}

	sum, _ := repo.SumAllEntries(ctx)
	if sum != 0 {
		t.Fatalf("books unbalanced after withdraw: %d", sum)
	}
}

func TestLedger_Harvest_Posting(t *testing.T) {
	db := openIntegrationDB(t)
	applyLedgerMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewLedgerRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	userAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeUserVaultPosition, &vaultID, &userID, nil, "USDC")
	vaultAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeVaultAssetPool, &vaultID, nil, nil, "USDC")
	suspenseAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeSystemSuspense, nil, nil, nil, "USDC")
	feeAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeFee, nil, nil, nil, "USDC")
	adapter := "blend"
	yieldAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeYieldSource, nil, nil, &adapter, "USDC")

	// Deposit 10 USDC
	deposit := int64(100_000_000)
	txID := uuid.New()
	if err := repo.PostEntries(ctx, []ledger.Entry{
		{TransactionID: txID, AccountID: userAcc.ID, Amount: deposit, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
		{TransactionID: txID, AccountID: vaultAcc.ID, Amount: deposit, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
		{TransactionID: txID, AccountID: suspenseAcc.ID, Amount: -2 * deposit, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
	}); err != nil {
		t.Fatalf("deposit: %v", err)
	}

	// Harvest: net 9 USDC (90_000_000 stroops), fee 1 USDC (10_000_000), gross 10 USDC
	netYield := int64(90_000_000)
	fee := int64(10_000_000)
	gross := int64(100_000_000)

	txID2 := uuid.New()
	harvestEntries := []ledger.Entry{
		{TransactionID: txID2, AccountID: vaultAcc.ID, Amount: netYield, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
		{TransactionID: txID2, AccountID: userAcc.ID, Amount: netYield, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
		{TransactionID: txID2, AccountID: feeAcc.ID, Amount: fee, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
		{TransactionID: txID2, AccountID: yieldAcc.ID, Amount: -gross, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
		{TransactionID: txID2, AccountID: suspenseAcc.ID, Amount: -netYield, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
	}
	if err := repo.PostEntries(ctx, harvestEntries); err != nil {
		t.Fatalf("harvest: %v", err)
	}

	sum, _ := repo.SumAllEntries(ctx)
	if sum != 0 {
		t.Fatalf("books unbalanced after harvest: %d", sum)
	}

	userBal, _ := repo.GetUserVaultBalance(ctx, userID, vaultID)
	if userBal != deposit+netYield {
		t.Fatalf("user balance after harvest: got %d, want %d", userBal, deposit+netYield)
	}

	vaultBal, _ := repo.GetVaultPoolBalance(ctx, vaultID)
	if vaultBal != deposit+netYield {
		t.Fatalf("vault pool after harvest: got %d, want %d", vaultBal, deposit+netYield)
	}
}

func TestLedger_Rebalance_Posting(t *testing.T) {
	db := openIntegrationDB(t)
	applyLedgerMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewLedgerRepository(db)
	ctx := context.Background()

	fromAdapter := "blend"
	toAdapter := "aave"
	fromAcc, err := repo.GetOrCreateAccount(ctx, ledger.AccountTypeYieldSource, nil, nil, &fromAdapter, "USDC")
	if err != nil {
		t.Fatalf("from account: %v", err)
	}
	toAcc, err := repo.GetOrCreateAccount(ctx, ledger.AccountTypeYieldSource, nil, nil, &toAdapter, "USDC")
	if err != nil {
		t.Fatalf("to account: %v", err)
	}

	// Simulate allocating 100 USDC to blend initially
	// For that we need a vault pool? But for rebalance test we just test movement between yield sources.
	amount := int64(50_000_000) // 5 USDC
	txID := uuid.New()
	entries := []ledger.Entry{
		{TransactionID: txID, AccountID: fromAcc.ID, Amount: -amount, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "rebalance"},
		{TransactionID: txID, AccountID: toAcc.ID, Amount: amount, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "rebalance"},
	}
	if err := repo.PostEntries(ctx, entries); err != nil {
		t.Fatalf("rebalance: %v", err)
	}

	sum, _ := repo.SumAllEntries(ctx)
	if sum != 0 {
		t.Fatalf("books unbalanced after rebalance: %d", sum)
	}

	mismatches, _ := repo.RecomputeBalances(ctx)
	if len(mismatches) != 0 {
		t.Fatalf("mismatches after rebalance: %+v", mismatches)
	}
}

// Property-style test: random sequences must keep books balanced to zero.
func TestLedger_Property_BooksAlwaysSumToZero(t *testing.T) {
	db := openIntegrationDB(t)
	applyLedgerMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewLedgerRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	userAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeUserVaultPosition, &vaultID, &userID, nil, "USDC")
	vaultAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeVaultAssetPool, &vaultID, nil, nil, "USDC")
	suspenseAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeSystemSuspense, nil, nil, nil, "USDC")
	feeAcc, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeFee, nil, nil, nil, "USDC")
	adapterBlend := "blend"
	adapterAave := "aave"
	yieldBlend, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeYieldSource, nil, nil, &adapterBlend, "USDC")
	yieldAave, _ := repo.GetOrCreateAccount(ctx, ledger.AccountTypeYieldSource, nil, nil, &adapterAave, "USDC")

	// Helper to create random but valid transactions
	// We'll simulate 100 random operations
	operations := []func() []ledger.Entry{
		func() []ledger.Entry { // deposit
			amt := int64(10_000_000 + (int64(uuid.New().ID()) % 90_000_000))
			txID := uuid.New()
			return []ledger.Entry{
				{TransactionID: txID, AccountID: userAcc.ID, Amount: amt, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
				{TransactionID: txID, AccountID: vaultAcc.ID, Amount: amt, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
				{TransactionID: txID, AccountID: suspenseAcc.ID, Amount: -2 * amt, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "deposit"},
			}
		},
		func() []ledger.Entry { // withdraw (if possible, but we allow negative for test - ledger allows)
			amt := int64(5_000_000 + (int64(uuid.New().ID()) % 20_000_000))
			txID := uuid.New()
			return []ledger.Entry{
				{TransactionID: txID, AccountID: userAcc.ID, Amount: -amt, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "withdraw"},
				{TransactionID: txID, AccountID: vaultAcc.ID, Amount: -amt, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "withdraw"},
				{TransactionID: txID, AccountID: suspenseAcc.ID, Amount: 2 * amt, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "withdraw"},
			}
		},
		func() []ledger.Entry { // harvest
			netYield := int64(1_000_000 + (int64(uuid.New().ID()) % 10_000_000))
			fee := int64(100_000 + (int64(uuid.New().ID()) % 1_000_000))
			gross := netYield + fee
			txID := uuid.New()
			return []ledger.Entry{
				{TransactionID: txID, AccountID: vaultAcc.ID, Amount: netYield, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
				{TransactionID: txID, AccountID: userAcc.ID, Amount: netYield, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
				{TransactionID: txID, AccountID: feeAcc.ID, Amount: fee, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
				{TransactionID: txID, AccountID: yieldBlend.ID, Amount: -gross, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
				{TransactionID: txID, AccountID: suspenseAcc.ID, Amount: -netYield, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "harvest"},
			}
		},
		func() []ledger.Entry { // rebalance
			amt := int64(5_000_000 + (int64(uuid.New().ID()) % 20_000_000))
			txID := uuid.New()
			return []ledger.Entry{
				{TransactionID: txID, AccountID: yieldBlend.ID, Amount: -amt, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "rebalance"},
				{TransactionID: txID, AccountID: yieldAave.ID, Amount: amt, AssetCode: "USDC", AssetUnit: "stroops", DomainEventType: "rebalance"},
			}
		},
	}

	for i := 0; i < 100; i++ {
		op := operations[i%len(operations)]
		entries := op()
		if err := repo.PostEntries(ctx, entries); err != nil {
			t.Fatalf("iteration %d post failed: %v", i, err)
		}
		sum, err := repo.SumAllEntries(ctx)
		if err != nil {
			t.Fatalf("iteration %d sum failed: %v", i, err)
		}
		if sum != 0 {
			t.Fatalf("iteration %d: books unbalanced, sum=%d", i, sum)
		}
	}

	// Final check: cached balances must match recomputed
	mismatches, err := repo.RecomputeBalances(ctx)
	if err != nil {
		t.Fatalf("final recompute: %v", err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("final mismatches: %+v", mismatches)
	}
}

// Test vault repository integration with ledger: deposit via vault repo should also create ledger entries.
func TestVaultRepository_Deposit_CreatesLedgerEntries(t *testing.T) {
	db := openIntegrationDB(t)
	applyLedgerMigrations(t, db)
	resetIntegrationTables(t, db)

	vaultRepo := NewVaultRepository(db)
	ledgerRepo := NewLedgerRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	amount := decimal.RequireFromString("10.0") // 10 USDC
	vaultRecord := vault.TransactionRecord{
		UserID:               userID,
		Amount:               amount,
		TransactionHash:      uuid.NewString(),
		SharesMintedOrBurned: decimal.RequireFromString("10"),
		SharePriceAtTime:     decimal.RequireFromString("1"),
		FeeCharged:           decimal.Zero,
	}

	if err := vaultRepo.RecordDeposit(ctx, vaultID, vaultRecord); err != nil {
		t.Fatalf("RecordDeposit error = %v", err)
	}

	// Ledger should have entries
	sum, err := ledgerRepo.SumAllEntries(ctx)
	if err != nil {
		t.Fatalf("SumAllEntries error = %v", err)
	}
	if sum != 0 {
		t.Fatalf("books not balanced after vault deposit: sum=%d", sum)
	}

	userBal, err := ledgerRepo.GetUserVaultBalance(ctx, userID, vaultID)
	if err != nil {
		t.Fatalf("GetUserVaultBalance error = %v", err)
	}
	// 10 USDC = 100_000_000 stroops
	if userBal != 100_000_000 {
		t.Fatalf("user vault balance after vault deposit: got %d, want 100000000", userBal)
	}

	mismatches, err := ledgerRepo.RecomputeBalances(ctx)
	if err != nil {
		t.Fatalf("RecomputeBalances error = %v", err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("mismatches after vault deposit: %+v", mismatches)
	}
}
