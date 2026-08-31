package stellar

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/testutil"
)

// The double-credit integration test for nester#1147.
//
// It drives the two real writers against a real database — the API write path
// (VaultRepository.RecordDeposit) and the event indexer (applyIndexedEvent) —
// for the SAME on-chain transaction, and asserts the balance moves exactly
// once. Mocks would only prove the mocks agree with each other; the shared key
// is a database uniqueness guarantee, so it has to be exercised against the
// real constraint.

const (
	doubleCreditContract = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
	doubleCreditUserID   = "44444444-4444-4444-8444-444444444444"
	doubleCreditVaultID  = "55555555-5555-4555-8555-555555555555"
)

// seedDoubleCreditVault applies the full migration chain and seeds one user and
// one zero-balance vault whose contract address the indexer keys on.
func seedDoubleCreditVault(t *testing.T, db *sql.DB) {
	t.Helper()

	testutil.ApplyAllMigrations(t, db, filepath.Join("..", "..", "migrations"))

	if _, err := db.Exec(
		`INSERT INTO users (id, wallet_address, display_name) VALUES ($1, $2, $3)`,
		doubleCreditUserID, "GAKX7VYAJTQOZ4ZKQ7GCTAWOAKCXY6BFCHOEQ4RRWJBTAY3EKQXHTKZL", "double-credit-user",
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if _, err := db.Exec(`
INSERT INTO vaults (id, user_id, contract_address, currency, status, total_deposited, current_balance, yield_earned)
VALUES ($1, $2, $3, 'USDC', 'active', 0, 0, 0)`,
		doubleCreditVaultID, doubleCreditUserID, doubleCreditContract,
	); err != nil {
		t.Fatalf("seed vault: %v", err)
	}
}

func readVaultBalances(t *testing.T, db *sql.DB) (totalDeposited, currentBalance decimal.Decimal) {
	t.Helper()
	var total, current string
	if err := db.QueryRow(
		`SELECT total_deposited::text, current_balance::text FROM vaults WHERE id = $1`,
		doubleCreditVaultID,
	).Scan(&total, &current); err != nil {
		t.Fatalf("read balances: %v", err)
	}
	return decimal.RequireFromString(total), decimal.RequireFromString(current)
}

func countLedgerRowsForHash(t *testing.T, db *sql.DB, hash string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM vault_transactions WHERE transaction_hash = $1`, hash,
	).Scan(&n); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	return n
}

// An API deposit followed by the corresponding indexed event credits once.
// This is the exact sequence that previously double-counted every deposit.
func TestAPIDepositThenIndexedEvent_CreditsExactlyOnce(t *testing.T) {
	db := openBackfillIntegrationDB(t)
	seedDoubleCreditVault(t, db)

	const txHash = "0d1e2f3a4b5c6d7e8f90112233445566778899aabbccddeeff00112233445566"
	// The API path stores asset units; a contract event carries stroops for
	// the same movement (nester#1146). Both must resolve to one credit.
	const amount = "100"
	const amountStroops = "1000000000"

	// 1. The user records the deposit through the API.
	repo := postgres.NewVaultRepository(db)
	if err := repo.RecordDeposit(context.Background(), uuid.MustParse(doubleCreditVaultID), vault.TransactionRecord{
		UserID:               uuid.MustParse(doubleCreditUserID),
		Amount:               decimal.RequireFromString(amount),
		TransactionHash:      txHash,
		SharesMintedOrBurned: decimal.NewFromInt(100),
		SharePriceAtTime:     decimal.NewFromInt(1),
		FeeCharged:           decimal.Zero,
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	total, current := readVaultBalances(t, db)
	if !total.Equal(decimal.RequireFromString(amount)) || !current.Equal(decimal.RequireFromString(amount)) {
		t.Fatalf("after API deposit: total=%s current=%s, want %s/%s", total, current, amount, amount)
	}

	// 2. The indexer observes the resulting on-chain event for the same hash.
	processed, err := applyIndexedEvent(context.Background(), db, indexedEvent{
		ID:         "evt-double-credit",
		ContractID: doubleCreditContract,
		EventType:  "deposit",
		Ledger:     4242,
		TxHash:     txHash,
		Data:       map[string]any{"amount": amountStroops},
	})
	if err != nil {
		t.Fatalf("applyIndexedEvent: %v", err)
	}
	if !processed {
		t.Fatal("event should be marked processed so the cursor advances")
	}

	// 3. The balance must not have moved again.
	total, current = readVaultBalances(t, db)
	want := decimal.RequireFromString(amount)
	if !total.Equal(want) {
		t.Errorf("total_deposited = %s, want %s (deposit was counted twice)", total, want)
	}
	if !current.Equal(want) {
		t.Errorf("current_balance = %s, want %s (deposit was counted twice)", current, want)
	}
	if got := countLedgerRowsForHash(t, db, txHash); got != 1 {
		t.Errorf("ledger rows for hash = %d, want exactly 1", got)
	}
}

// The reverse order must hold too: when the indexer observes the deposit
// first, a later API record for the same hash must not add a second credit.
func TestIndexedEventThenAPIDeposit_CreditsExactlyOnce(t *testing.T) {
	db := openBackfillIntegrationDB(t)
	seedDoubleCreditVault(t, db)

	const txHash = "aabbccddeeff00112233445566778899aabbccddeeff001122334455667788990"
	const amount = "250"
	const amountStroops = "2500000000"

	processed, err := applyIndexedEvent(context.Background(), db, indexedEvent{
		ID:         "evt-indexer-first",
		ContractID: doubleCreditContract,
		EventType:  "deposit",
		Ledger:     4343,
		TxHash:     txHash,
		Data:       map[string]any{"amount": amountStroops},
	})
	if err != nil {
		t.Fatalf("applyIndexedEvent: %v", err)
	}
	if !processed {
		t.Fatal("event should be marked processed")
	}

	total, current := readVaultBalances(t, db)
	want := decimal.RequireFromString(amount)
	if !total.Equal(want) || !current.Equal(want) {
		t.Fatalf("after indexed event: total=%s current=%s, want %s", total, current, want)
	}

	// The API path now tries to record the same transaction. It must report
	// the duplicate rather than crediting again.
	repo := postgres.NewVaultRepository(db)
	err = repo.RecordDeposit(context.Background(), uuid.MustParse(doubleCreditVaultID), vault.TransactionRecord{
		UserID:               uuid.MustParse(doubleCreditUserID),
		Amount:               decimal.RequireFromString(amount),
		TransactionHash:      txHash,
		SharesMintedOrBurned: decimal.NewFromInt(250),
		SharePriceAtTime:     decimal.NewFromInt(1),
		FeeCharged:           decimal.Zero,
	})
	if err == nil {
		t.Fatal("RecordDeposit should report the already-claimed hash")
	}

	total, current = readVaultBalances(t, db)
	if !total.Equal(want) {
		t.Errorf("total_deposited = %s, want %s (credited twice)", total, want)
	}
	if !current.Equal(want) {
		t.Errorf("current_balance = %s, want %s (credited twice)", current, want)
	}
	if got := countLedgerRowsForHash(t, db, txHash); got != 1 {
		t.Errorf("ledger rows for hash = %d, want exactly 1", got)
	}
}

// Repeat delivery of the same event (a different event id for the same
// transaction, e.g. after a backfill overlap) must still credit only once.
func TestRedeliveredEventForSameTxHash_CreditsExactlyOnce(t *testing.T) {
	db := openBackfillIntegrationDB(t)
	seedDoubleCreditVault(t, db)

	const txHash = "ffeeddccbbaa99887766554433221100ffeeddccbbaa998877665544332211000"
	const amount = "40"
	const amountStroops = "400000000"

	for _, eventID := range []string{"evt-first-delivery", "evt-second-delivery"} {
		if _, err := applyIndexedEvent(context.Background(), db, indexedEvent{
			ID:         eventID,
			ContractID: doubleCreditContract,
			EventType:  "deposit",
			Ledger:     4444,
			TxHash:     txHash,
			Data:       map[string]any{"amount": amountStroops},
		}); err != nil {
			t.Fatalf("applyIndexedEvent(%s): %v", eventID, err)
		}
	}

	total, current := readVaultBalances(t, db)
	want := decimal.RequireFromString(amount)
	if !total.Equal(want) {
		t.Errorf("total_deposited = %s, want %s", total, want)
	}
	if !current.Equal(want) {
		t.Errorf("current_balance = %s, want %s", current, want)
	}
	if got := countLedgerRowsForHash(t, db, txHash); got != 1 {
		t.Errorf("ledger rows for hash = %d, want exactly 1", got)
	}
}
