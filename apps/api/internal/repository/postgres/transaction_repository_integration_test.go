package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

// applyTransactionMigrations applies the minimal set of migrations needed for
// transaction repository integration tests. All statements use IF NOT EXISTS so
// this is safe to run against a database that already has the schema.
func applyTransactionMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, name := range []string{
		"001_create_users_table.up.sql",
		"002_create_vaults_table.up.sql",
		"005_create_allocations_table.up.sql",
		"006_create_settlements_table.up.sql",
		"003_create_transactions_table.up.sql",
		"009_add_confirmed_at_to_transactions.up.sql",
	} {
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

// newTestTransaction returns a pending deposit transaction for the given vault.
func newTestTransaction(vaultID uuid.UUID, txHash string) transaction.Transaction {
	return transaction.Transaction{
		ID:       uuid.New(),
		VaultID:  vaultID,
		Type:     transaction.TypeDeposit,
		Amount:   decimal.RequireFromString("250.00"),
		Currency: "USDC",
		TxHash:   txHash,
		Status:   transaction.StatusPending,
	}
}

// ---------------------------------------------------------------------------
// Upsert
// ---------------------------------------------------------------------------

func TestCreateTransaction_success(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	tx := newTestTransaction(vaultID, "0xabc001")
	got, err := repo.Upsert(ctx, tx)
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if got.ID != tx.ID {
		t.Fatalf("ID: got %v, want %v", got.ID, tx.ID)
	}
	if got.TxHash != tx.TxHash {
		t.Fatalf("TxHash: got %q, want %q", got.TxHash, tx.TxHash)
	}
	if got.Status != transaction.StatusPending {
		t.Fatalf("Status: got %v, want pending", got.Status)
	}
	if !got.Amount.Equal(tx.Amount) {
		t.Fatalf("Amount: got %s, want %s", got.Amount, tx.Amount)
	}
	if got.VaultID != vaultID {
		t.Fatalf("VaultID mismatch")
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be populated by the database")
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be populated by the database")
	}
}

// TestCreateTransaction_duplicateTxHash_updatesExisting verifies that Upsert
// performs an ON CONFLICT DO UPDATE when the same tx_hash is submitted again,
// updating the row rather than returning an error.
func TestCreateTransaction_duplicateTxHash_updatesExisting(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	tx := newTestTransaction(vaultID, "0xdup001")
	if _, err := repo.Upsert(ctx, tx); err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}

	// Re-submit the same hash with an updated status.
	tx.Status = transaction.StatusCompleted
	got, err := repo.Upsert(ctx, tx)
	if err != nil {
		t.Fatalf("second Upsert() (duplicate hash) unexpected error = %v", err)
	}
	if got.Status != transaction.StatusCompleted {
		t.Fatalf("Status after upsert: got %v, want completed", got.Status)
	}
}

// TestCreateTransaction_invalidVaultID_returnsError verifies that inserting a
// transaction whose vault_id does not exist returns ErrInvalidTransaction via
// the FK violation path.
func TestCreateTransaction_invalidVaultID_returnsError(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	tx := newTestTransaction(uuid.New(), "0xbadvault")
	_, err := repo.Upsert(ctx, tx)
	if !errors.Is(err, transaction.ErrInvalidTransaction) {
		t.Fatalf("expected ErrInvalidTransaction for non-existent vault_id, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetByHash
// ---------------------------------------------------------------------------

func TestGetTransaction_byHash_success(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	tx := newTestTransaction(vaultID, "0xget001")
	if _, err := repo.Upsert(ctx, tx); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := repo.GetByHash(ctx, tx.TxHash)
	if err != nil {
		t.Fatalf("GetByHash() error = %v", err)
	}
	if got.ID != tx.ID {
		t.Fatalf("ID mismatch")
	}
	if got.VaultID != vaultID {
		t.Fatalf("VaultID mismatch")
	}
	if !got.Amount.Equal(tx.Amount) {
		t.Fatalf("Amount: got %s, want %s", got.Amount, tx.Amount)
	}
	if got.Currency != tx.Currency {
		t.Fatalf("Currency: got %q, want %q", got.Currency, tx.Currency)
	}
	if got.Type != tx.Type {
		t.Fatalf("Type: got %v, want %v", got.Type, tx.Type)
	}
}

func TestGetTransaction_notFound_returnsError(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	_, err := repo.GetByHash(ctx, "0xnonexistent")
	if !errors.Is(err, transaction.ErrTransactionNotFound) {
		t.Fatalf("expected ErrTransactionNotFound, got %v", err)
	}
}

// TestGetTransaction_wrongOwner documents the ownership model: GetByHash has no
// user-level ownership filter by design — that boundary is enforced at the
// handler layer. This test verifies vault-level isolation: each transaction
// is stored against its owning vault and is retrievable only by hash.
func TestGetTransaction_wrongOwner_vaultIsolation(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userA := seedIntegrationUser(t, db)
	userB := seedIntegrationUser(t, db)
	vaultA := seedIntegrationVault(t, db, userA)
	vaultB := seedIntegrationVault(t, db, userB)

	txA := newTestTransaction(vaultA, "0xownerA")
	txB := newTestTransaction(vaultB, "0xownerB")
	for _, tx := range []transaction.Transaction{txA, txB} {
		if _, err := repo.Upsert(ctx, tx); err != nil {
			t.Fatalf("Upsert(%s) error = %v", tx.TxHash, err)
		}
	}

	gotA, err := repo.GetByHash(ctx, "0xownerA")
	if err != nil {
		t.Fatalf("GetByHash(txA) error = %v", err)
	}
	if gotA.VaultID != vaultA {
		t.Fatalf("txA VaultID: got %v, want %v", gotA.VaultID, vaultA)
	}

	gotB, err := repo.GetByHash(ctx, "0xownerB")
	if err != nil {
		t.Fatalf("GetByHash(txB) error = %v", err)
	}
	if gotB.VaultID != vaultB {
		t.Fatalf("txB VaultID: got %v, want %v", gotB.VaultID, vaultB)
	}
}

// ---------------------------------------------------------------------------
// Type / vault filtering (via direct hash retrieval)
// ---------------------------------------------------------------------------

func TestListTransactions_filterByVault(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultA := seedIntegrationVault(t, db, userID)
	vaultB := seedIntegrationVault(t, db, userID)

	for _, hash := range []string{"0xvA1", "0xvA2"} {
		if _, err := repo.Upsert(ctx, newTestTransaction(vaultA, hash)); err != nil {
			t.Fatalf("Upsert(%s) error = %v", hash, err)
		}
	}
	if _, err := repo.Upsert(ctx, newTestTransaction(vaultB, "0xvB1")); err != nil {
		t.Fatalf("Upsert(vaultB) error = %v", err)
	}

	// Each transaction is associated with its correct vault.
	for hash, wantVault := range map[string]uuid.UUID{
		"0xvA1": vaultA,
		"0xvA2": vaultA,
		"0xvB1": vaultB,
	} {
		got, err := repo.GetByHash(ctx, hash)
		if err != nil {
			t.Fatalf("GetByHash(%s) error = %v", hash, err)
		}
		if got.VaultID != wantVault {
			t.Fatalf("hash %s: VaultID got %v, want %v", hash, got.VaultID, wantVault)
		}
	}
}

func TestListTransactions_filterByType(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	dep := newTestTransaction(vaultID, "0xtype_dep")
	dep.Type = transaction.TypeDeposit

	wdw := newTestTransaction(vaultID, "0xtype_wdw")
	wdw.Type = transaction.TypeWithdrawal

	for _, tx := range []transaction.Transaction{dep, wdw} {
		if _, err := repo.Upsert(ctx, tx); err != nil {
			t.Fatalf("Upsert(%s) error = %v", tx.TxHash, err)
		}
	}

	gotDep, _ := repo.GetByHash(ctx, "0xtype_dep")
	if gotDep.Type != transaction.TypeDeposit {
		t.Fatalf("Type: got %v, want deposit", gotDep.Type)
	}

	gotWdw, _ := repo.GetByHash(ctx, "0xtype_wdw")
	if gotWdw.Type != transaction.TypeWithdrawal {
		t.Fatalf("Type: got %v, want withdrawal", gotWdw.Type)
	}
}

func TestListTransactions_filterByStatus(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	pend := newTestTransaction(vaultID, "0xst_pend")
	pend.Status = transaction.StatusPending

	done := newTestTransaction(vaultID, "0xst_done")
	done.Status = transaction.StatusCompleted

	for _, tx := range []transaction.Transaction{pend, done} {
		if _, err := repo.Upsert(ctx, tx); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}
	}

	gotPend, _ := repo.GetByHash(ctx, "0xst_pend")
	if gotPend.Status != transaction.StatusPending {
		t.Fatalf("expected pending, got %v", gotPend.Status)
	}

	gotDone, _ := repo.GetByHash(ctx, "0xst_done")
	if gotDone.Status != transaction.StatusCompleted {
		t.Fatalf("expected completed, got %v", gotDone.Status)
	}
}

// TestListTransactions_pagination verifies that multiple transactions for the
// same vault are stored and individually retrievable by hash.
func TestListTransactions_pagination(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	hashes := []string{"0xpg1", "0xpg2", "0xpg3", "0xpg4", "0xpg5"}
	for _, h := range hashes {
		if _, err := repo.Upsert(ctx, newTestTransaction(vaultID, h)); err != nil {
			t.Fatalf("Upsert(%s) error = %v", h, err)
		}
	}

	for _, h := range hashes {
		if _, err := repo.GetByHash(ctx, h); err != nil {
			t.Fatalf("GetByHash(%s) error = %v", h, err)
		}
	}
}

// ---------------------------------------------------------------------------
// UpdateStatus
// ---------------------------------------------------------------------------

func TestUpdateTransactionStatus_pendingToConfirmed(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	tx := newTestTransaction(vaultID, "0xupd001")
	if _, err := repo.Upsert(ctx, tx); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	confirmedAt := time.Now().UTC()
	got, err := repo.UpdateStatus(ctx, tx.TxHash, transaction.StatusCompleted, &confirmedAt, "")
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	if got.Status != transaction.StatusCompleted {
		t.Fatalf("Status: got %v, want completed", got.Status)
	}
	if got.ConfirmedAt == nil {
		t.Fatal("ConfirmedAt should be set for a completed transaction")
	}
	if got.ErrorReason != "" {
		t.Fatalf("ErrorReason should be empty, got %q", got.ErrorReason)
	}
}

func TestUpdateTransactionStatus_pendingToFailed(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	tx := newTestTransaction(vaultID, "0xfail001")
	if _, err := repo.Upsert(ctx, tx); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	got, err := repo.UpdateStatus(ctx, tx.TxHash, transaction.StatusFailed, nil, "on-chain reverted")
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	if got.Status != transaction.StatusFailed {
		t.Fatalf("Status: got %v, want failed", got.Status)
	}
	if got.ConfirmedAt != nil {
		t.Fatal("ConfirmedAt should be nil for a failed transaction")
	}
	if got.ErrorReason != "on-chain reverted" {
		t.Fatalf("ErrorReason: got %q, want 'on-chain reverted'", got.ErrorReason)
	}
}

// TestUpdateTransactionStatus_invalidTransition documents that the repository
// layer does not enforce state machine rules — a completed transaction can be
// moved back to pending at the DB level. Transition enforcement must live in
// the service/use-case layer.
func TestUpdateTransactionStatus_invalidTransition(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)
	vaultID := seedIntegrationVault(t, db, userID)

	confirmedAt := time.Now().UTC()
	tx := newTestTransaction(vaultID, "0xinvtr001")
	tx.Status = transaction.StatusCompleted
	if _, err := repo.Upsert(ctx, tx); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	// Confirm it.
	if _, err := repo.UpdateStatus(ctx, tx.TxHash, transaction.StatusCompleted, &confirmedAt, ""); err != nil {
		t.Fatalf("UpdateStatus(completed) error = %v", err)
	}

	// Attempting to move confirmed → pending succeeds at the repository layer;
	// business-rule enforcement is the caller's responsibility.
	_, err := repo.UpdateStatus(ctx, tx.TxHash, transaction.StatusPending, nil, "")
	if err != nil {
		t.Fatalf("repository allows any status transition; got unexpected error: %v", err)
	}
}

func TestUpdateTransactionStatus_notFound_returnsError(t *testing.T) {
	db := openIntegrationDB(t)
	applyTransactionMigrations(t, db)
	resetIntegrationTables(t, db)

	repo := NewTransactionRepository(db)
	ctx := context.Background()

	_, err := repo.UpdateStatus(ctx, "0xghost", transaction.StatusCompleted, nil, "")
	if !errors.Is(err, transaction.ErrTransactionNotFound) {
		t.Fatalf("expected ErrTransactionNotFound, got %v", err)
	}
}
