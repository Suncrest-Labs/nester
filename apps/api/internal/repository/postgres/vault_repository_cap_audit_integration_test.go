package postgres

import (
	"context"
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

	entries, err := auditRepo.ListByVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListByVault() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if !balanceaudit.Reconcile(entries).Equal(fetched.CurrentBalance) {
		t.Fatalf("Reconcile() = %s, want %s (vault's live balance)", balanceaudit.Reconcile(entries), fetched.CurrentBalance)
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
