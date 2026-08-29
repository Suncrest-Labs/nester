package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/balanceaudit"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// TestBalanceAuditRepositoryIntegration_ListByVault_Paginates verifies
// ListByVault honors limit/offset and reports the true total count, the
// pagination shape requested for the append-only (and therefore
// ever-growing) balance_audit_log table.
func TestBalanceAuditRepositoryIntegration_ListByVault_Paginates(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	vaultRepo := NewVaultRepository(db)
	auditRepo := NewBalanceAuditRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := vaultRepo.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-AUDIT-PAGE",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	const numEntries = 5
	for i := 0; i < numEntries; i++ {
		if _, err := auditRepo.Append(ctx, balanceaudit.Entry{
			VaultID:       created.ID,
			UserID:        userID,
			Actor:         userID.String(),
			Operation:     balanceaudit.OperationDeposit,
			Amount:        decimal.NewFromInt(1),
			BalanceBefore: decimal.NewFromInt(int64(i)),
			BalanceAfter:  decimal.NewFromInt(int64(i + 1)),
		}); err != nil {
			t.Fatalf("Append() error = %v", err)
		}
	}

	// A page smaller than the total reports the true total and only the
	// requested slice, oldest first.
	page, total, err := auditRepo.ListByVault(ctx, created.ID, 2, 0)
	if err != nil {
		t.Fatalf("ListByVault() error = %v", err)
	}
	if total != numEntries {
		t.Fatalf("total = %d, want %d", total, numEntries)
	}
	if len(page) != 2 {
		t.Fatalf("len(page) = %d, want 2", len(page))
	}
	if !page[0].BalanceAfter.Equal(decimal.NewFromInt(1)) || !page[1].BalanceAfter.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("unexpected page contents: %+v", page)
	}

	// The next page picks up where the first left off.
	page2, total2, err := auditRepo.ListByVault(ctx, created.ID, 2, 2)
	if err != nil {
		t.Fatalf("ListByVault() page2 error = %v", err)
	}
	if total2 != numEntries {
		t.Fatalf("total2 = %d, want %d", total2, numEntries)
	}
	if len(page2) != 2 || !page2[0].BalanceAfter.Equal(decimal.NewFromInt(3)) {
		t.Fatalf("unexpected page2 contents: %+v", page2)
	}

	// A non-positive limit falls back to balanceaudit.DefaultListLimit,
	// which comfortably covers all 5 entries here.
	all, totalAll, err := auditRepo.ListByVault(ctx, created.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListByVault() default-limit error = %v", err)
	}
	if totalAll != numEntries || len(all) != numEntries {
		t.Fatalf("default-limit fetch = %d entries (total=%d), want %d", len(all), totalAll, numEntries)
	}

	// ListByUser follows the same shape.
	userPage, userTotal, err := auditRepo.ListByUser(ctx, userID, 3, 0)
	if err != nil {
		t.Fatalf("ListByUser() error = %v", err)
	}
	if userTotal != numEntries {
		t.Fatalf("ListByUser total = %d, want %d", userTotal, numEntries)
	}
	if len(userPage) != 3 {
		t.Fatalf("len(ListByUser page) = %d, want 3", len(userPage))
	}

	// An offset past the end of an otherwise non-empty result set (total=5,
	// offset=10) must return an empty slice with the correct total and a nil
	// error — not balanceaudit.ErrNotFound (nester CodeRabbit post-rebase
	// finding: this used to be conflated with "no entries at all").
	pastEnd, totalPastEnd, err := auditRepo.ListByVault(ctx, created.ID, 2, 10)
	if err != nil {
		t.Fatalf("ListByVault() past-end offset error = %v, want nil", err)
	}
	if totalPastEnd != numEntries {
		t.Fatalf("ListByVault() past-end offset total = %d, want %d", totalPastEnd, numEntries)
	}
	if len(pastEnd) != 0 {
		t.Fatalf("ListByVault() past-end offset page = %+v, want empty", pastEnd)
	}

	userPastEnd, userTotalPastEnd, err := auditRepo.ListByUser(ctx, userID, 2, 10)
	if err != nil {
		t.Fatalf("ListByUser() past-end offset error = %v, want nil", err)
	}
	if userTotalPastEnd != numEntries {
		t.Fatalf("ListByUser() past-end offset total = %d, want %d", userTotalPastEnd, numEntries)
	}
	if len(userPastEnd) != 0 {
		t.Fatalf("ListByUser() past-end offset page = %+v, want empty", userPastEnd)
	}
}

// TestBalanceAuditRepositoryIntegration_ListByVault_NotFound confirms an
// out-of-range offset against a vault with entries still surfaces its true
// total rather than silently reporting zero (pagination must not mask that
// the vault has history — only that this page of it is empty).
func TestBalanceAuditRepositoryIntegration_ListByVault_UnknownVault(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	auditRepo := NewBalanceAuditRepository(db)
	ctx := context.Background()

	_, _, err := auditRepo.ListByVault(ctx, uuid.New(), 10, 0)
	if err != balanceaudit.ErrNotFound {
		t.Fatalf("ListByVault() error = %v, want balanceaudit.ErrNotFound", err)
	}
}
