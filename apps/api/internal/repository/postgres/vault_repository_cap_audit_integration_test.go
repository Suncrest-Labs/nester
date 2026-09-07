package postgres

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/balanceaudit"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/caps"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// TestVaultRepositoryIntegrationRecordDepositWithAudit_AtomicWithBalance
// verifies the balance credit and the audit-ledger append commit together:
// after a deposit, exactly one audit row exists reflecting the same
// before/after balances as the vault itself (nester#1124 durability
// finding).
func TestVaultRepositoryIntegrationRecordDepositWithAudit_AtomicWithBalance(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	auditRepo := NewBalanceAuditRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-AUDIT-DEP",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	entry, err := repository.RecordDepositWithAudit(ctx, created.ID, vault.TransactionRecord{
		UserID:               userID,
		Amount:               decimal.RequireFromString("42"),
		SharesMintedOrBurned: decimal.RequireFromString("42"),
		SharePriceAtTime:     decimal.NewFromInt(1),
	}, nil, balanceaudit.Entry{
		UserID:    userID,
		Actor:     userID.String(),
		Operation: balanceaudit.OperationDeposit,
		Amount:    decimal.RequireFromString("42"),
	})
	if err != nil {
		t.Fatalf("RecordDepositWithAudit() error = %v", err)
	}
	if !entry.BalanceBefore.IsZero() || !entry.BalanceAfter.Equal(decimal.RequireFromString("42")) {
		t.Fatalf("audit entry before/after = %s/%s, want 0/42", entry.BalanceBefore, entry.BalanceAfter)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("42")) {
		t.Fatalf("CurrentBalance = %s, want 42", fetched.CurrentBalance)
	}

	entries, total, err := auditRepo.ListByVault(ctx, created.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListByVault() error = %v", err)
	}
	if len(entries) != 1 || total != 1 {
		t.Fatalf("expected 1 audit entry, got %d (total=%d)", len(entries), total)
	}
	reconciled, err := balanceaudit.Reconcile(entries)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !reconciled.Equal(fetched.CurrentBalance) {
		t.Fatalf("Reconcile() = %s, want %s (vault's live balance)", reconciled, fetched.CurrentBalance)
	}
}

// TestVaultRepositoryIntegrationRecordDepositWithAudit_ConcurrentDepositsRespectCap
// is the concurrency proof for the TOCTOU finding (nester#1119, CodeRabbit):
// many concurrent deposits, each individually under the per-user cap, must
// never collectively push the user's total over the cap once the atomic
// (RecordDepositWithAudit) path is used.
func TestVaultRepositoryIntegrationRecordDepositWithAudit_ConcurrentDepositsRespectCap(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-CAP-RACE",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	perUserCap := decimal.NewFromInt(100)
	checker := caps.NewChecker(caps.Config{PerUserCap: perUserCap}, nil, nil)

	const attempts = 30
	const depositAmount = 10 // 30 * 10 = 300 attempted, well over the 100 cap if unserialized

	var wg sync.WaitGroup
	var accepted, rejected int32
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wg.Done()
			amount := decimal.NewFromInt(depositAmount)
			capCheck := func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error {
				return checker.EvaluateTotals(ctx, userID, amount, currentUserTotal, currentGlobalTotal)
			}
			_, err := repository.RecordDepositWithAudit(ctx, created.ID, vault.TransactionRecord{
				UserID:               userID,
				Amount:               amount,
				SharesMintedOrBurned: amount,
				SharePriceAtTime:     decimal.NewFromInt(1),
			}, capCheck, balanceaudit.Entry{
				UserID:    userID,
				Actor:     userID.String(),
				Operation: balanceaudit.OperationDeposit,
				Amount:    amount,
			})
			if err != nil {
				atomic.AddInt32(&rejected, 1)
				return
			}
			atomic.AddInt32(&accepted, 1)
		}()
	}
	wg.Wait()

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if fetched.CurrentBalance.GreaterThan(perUserCap) {
		t.Fatalf("balance %s exceeded per-user cap %s after %d accepted deposits", fetched.CurrentBalance, perUserCap, accepted)
	}
	if int(accepted)+int(rejected) != attempts {
		t.Fatalf("accepted(%d)+rejected(%d) != attempts(%d)", accepted, rejected, attempts)
	}
	t.Logf("accepted=%d rejected=%d finalBalance=%s", accepted, rejected, fetched.CurrentBalance)
}

// TestVaultRepositoryIntegrationRecordWithdrawalWithAudit_OverWithdrawalReturnsDomainError
// is the CodeRabbit-flagged finding: RecordWithdrawalWithAudit locks the
// vault row and reads its balance before updating it, but must compare that
// balance against the requested amount itself and return
// vault.ErrWithdrawalExceedsPosition, rather than relying on a DB
// CHECK-constraint violation the repository doesn't map to a domain error.
func TestVaultRepositoryIntegrationRecordWithdrawalWithAudit_OverWithdrawalReturnsDomainError(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-AUDIT-OVERWITHDRAW",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	if _, err := repository.RecordDepositWithAudit(ctx, created.ID, vault.TransactionRecord{
		UserID:               userID,
		Amount:               decimal.RequireFromString("50"),
		SharesMintedOrBurned: decimal.RequireFromString("50"),
		SharePriceAtTime:     decimal.NewFromInt(1),
	}, nil, balanceaudit.Entry{
		UserID:    userID,
		Actor:     userID.String(),
		Operation: balanceaudit.OperationDeposit,
		Amount:    decimal.RequireFromString("50"),
	}); err != nil {
		t.Fatalf("RecordDepositWithAudit() error = %v", err)
	}

	_, err = repository.RecordWithdrawalWithAudit(ctx, created.ID, vault.TransactionRecord{
		UserID:               userID,
		Amount:               decimal.RequireFromString("75"),
		SharesMintedOrBurned: decimal.RequireFromString("75"),
		SharePriceAtTime:     decimal.NewFromInt(1),
	}, balanceaudit.Entry{
		UserID:    userID,
		Actor:     userID.String(),
		Operation: balanceaudit.OperationWithdrawal,
		Amount:    decimal.RequireFromString("75"),
	})
	if !errors.Is(err, vault.ErrWithdrawalExceedsPosition) {
		t.Fatalf("RecordWithdrawalWithAudit() error = %v, want ErrWithdrawalExceedsPosition", err)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("50")) {
		t.Fatalf("CurrentBalance = %s, want 50 (over-withdrawal must not partially apply)", fetched.CurrentBalance)
	}
}

// ledgerEntryCount returns how many ledger_entries rows exist for the given
// domain event (a transaction hash), so a test can assert the double-entry
// ledger was actually posted rather than only checking the domain tables.
func ledgerEntryCount(t *testing.T, db *sql.DB, domainEventID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM ledger_entries WHERE domain_event_id = $1`,
		domainEventID,
	).Scan(&count); err != nil {
		t.Fatalf("count ledger_entries: %v", err)
	}
	return count
}

// TestVaultRepositoryIntegrationWithAudit_PostsLedgerEntries guards against a
// regression CodeRabbit found: the *WithAudit methods (RecordDepositWithAudit,
// RecordWithdrawalWithAudit, RecordHarvestWithAudit) append the balance-audit
// entry and commit, but must ALSO post to the double-entry ledger
// (ledger_entries/ledger_balances) exactly like their non-audit siblings
// (RecordDeposit/RecordWithdrawal/RecordHarvest) do — otherwise, since the
// service layer type-asserts to the *WithAudit path whenever it's available,
// production traffic would silently stop feeding the ledger.
func TestVaultRepositoryIntegrationWithAudit_PostsLedgerEntries(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-LEDGER-AUDIT",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	// Deposit.
	const depositHash = "int-ledger-deposit"
	if _, err := repository.RecordDepositWithAudit(ctx, created.ID, vault.TransactionRecord{
		UserID:               userID,
		Amount:               decimal.RequireFromString("50"),
		TransactionHash:      depositHash,
		SharesMintedOrBurned: decimal.RequireFromString("50"),
		SharePriceAtTime:     decimal.NewFromInt(1),
	}, nil, balanceaudit.Entry{
		UserID:    userID,
		Actor:     userID.String(),
		Operation: balanceaudit.OperationDeposit,
		Amount:    decimal.RequireFromString("50"),
	}); err != nil {
		t.Fatalf("RecordDepositWithAudit() error = %v", err)
	}
	if count := ledgerEntryCount(t, db, depositHash); count == 0 {
		t.Fatalf("no ledger_entries posted for deposit %q via RecordDepositWithAudit", depositHash)
	}

	// Withdrawal.
	const withdrawalHash = "int-ledger-withdrawal"
	if _, err := repository.RecordWithdrawalWithAudit(ctx, created.ID, vault.TransactionRecord{
		UserID:               userID,
		Amount:               decimal.RequireFromString("10"),
		TransactionHash:      withdrawalHash,
		SharesMintedOrBurned: decimal.RequireFromString("10"),
		SharePriceAtTime:     decimal.NewFromInt(1),
	}, balanceaudit.Entry{
		UserID:    userID,
		Actor:     userID.String(),
		Operation: balanceaudit.OperationWithdrawal,
		Amount:    decimal.RequireFromString("10"),
	}); err != nil {
		t.Fatalf("RecordWithdrawalWithAudit() error = %v", err)
	}
	if count := ledgerEntryCount(t, db, withdrawalHash); count == 0 {
		t.Fatalf("no ledger_entries posted for withdrawal %q via RecordWithdrawalWithAudit", withdrawalHash)
	}

	// Harvest.
	const harvestHash = "int-ledger-harvest"
	if _, err := repository.RecordHarvestWithAudit(ctx, vault.HarvestRecordInput{
		VaultID:         created.ID,
		UserID:          userID,
		NetYield:        decimal.RequireFromString("5"),
		PerformanceFee:  decimal.RequireFromString("1"),
		Compounded:      true,
		TransactionHash: harvestHash,
	}, balanceaudit.Entry{
		UserID:    userID,
		Actor:     "system:harvest",
		Operation: balanceaudit.OperationHarvest,
		Amount:    decimal.RequireFromString("5"),
	}); err != nil {
		t.Fatalf("RecordHarvestWithAudit() error = %v", err)
	}
	if count := ledgerEntryCount(t, db, harvestHash); count == 0 {
		t.Fatalf("no ledger_entries posted for harvest %q via RecordHarvestWithAudit", harvestHash)
	}
}
