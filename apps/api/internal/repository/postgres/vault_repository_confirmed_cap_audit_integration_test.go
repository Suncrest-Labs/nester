package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/balanceaudit"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/caps"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// TestApplyConfirmedDeposit_AppendsAuditEntry is the durability half of the
// CodeRabbit finding for this branch: a confirmed on-chain deposit previously
// updated the vault balance without ever appending a balance-audit entry.
// Exactly one audit entry must exist per confirmed deposit, with
// BalanceBefore/BalanceAfter matching the real before/after balances.
func TestApplyConfirmedDeposit_AppendsAuditEntry(t *testing.T) {
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
		ContractAddress: "CA-INT-CONFIRMED-DEP-AUDIT",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	capWarning, err := repository.ApplyConfirmedDeposit(ctx, created.ID, userID, decimal.RequireFromString("30"), "confirmed-dep-hash-1", nil)
	if err != nil {
		t.Fatalf("ApplyConfirmedDeposit() error = %v", err)
	}
	if capWarning != nil {
		t.Fatalf("ApplyConfirmedDeposit() capWarning = %v, want nil (no capCheck installed)", capWarning)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("30")) {
		t.Fatalf("CurrentBalance = %s, want 30", fetched.CurrentBalance)
	}

	entries, total, err := auditRepo.ListByVault(ctx, created.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListByVault() error = %v", err)
	}
	if len(entries) != 1 || total != 1 {
		t.Fatalf("expected exactly 1 audit entry for the confirmed deposit, got %d (total=%d)", len(entries), total)
	}
	entry := entries[0]
	if entry.Operation != balanceaudit.OperationDeposit {
		t.Fatalf("Operation = %s, want deposit", entry.Operation)
	}
	if !entry.BalanceBefore.IsZero() || !entry.BalanceAfter.Equal(decimal.RequireFromString("30")) {
		t.Fatalf("audit entry before/after = %s/%s, want 0/30", entry.BalanceBefore, entry.BalanceAfter)
	}
	if entry.ChainReference != "confirmed-dep-hash-1" {
		t.Fatalf("ChainReference = %q, want confirmed-dep-hash-1", entry.ChainReference)
	}

	// Idempotency: replaying the same hash must not double-credit or
	// double-audit.
	if _, err := repository.ApplyConfirmedDeposit(ctx, created.ID, userID, decimal.RequireFromString("30"), "confirmed-dep-hash-1", nil); err != nil {
		t.Fatalf("ApplyConfirmedDeposit() retry error = %v", err)
	}
	fetched, err = repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("30")) {
		t.Fatalf("CurrentBalance after replay = %s, want unchanged 30", fetched.CurrentBalance)
	}
	_, total, err = auditRepo.ListByVault(ctx, created.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListByVault() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("audit entries after replay = %d, want still 1 (idempotent)", total)
	}
}

// TestApplyConfirmedWithdrawal_AppendsAuditEntry mirrors the deposit test for
// withdrawals.
func TestApplyConfirmedWithdrawal_AppendsAuditEntry(t *testing.T) {
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
		ContractAddress: "CA-INT-CONFIRMED-WD-AUDIT",
		TotalDeposited:  decimal.RequireFromString("50"),
		CurrentBalance:  decimal.RequireFromString("50"),
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	if err := repository.ApplyConfirmedWithdrawal(ctx, created.ID, userID, decimal.RequireFromString("20"), "confirmed-wd-hash-1"); err != nil {
		t.Fatalf("ApplyConfirmedWithdrawal() error = %v", err)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("30")) {
		t.Fatalf("CurrentBalance = %s, want 30", fetched.CurrentBalance)
	}

	entries, total, err := auditRepo.ListByVault(ctx, created.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListByVault() error = %v", err)
	}
	if len(entries) != 1 || total != 1 {
		t.Fatalf("expected exactly 1 audit entry for the confirmed withdrawal, got %d (total=%d)", len(entries), total)
	}
	entry := entries[0]
	if entry.Operation != balanceaudit.OperationWithdrawal {
		t.Fatalf("Operation = %s, want withdrawal", entry.Operation)
	}
	if !entry.BalanceBefore.Equal(decimal.RequireFromString("50")) || !entry.BalanceAfter.Equal(decimal.RequireFromString("30")) {
		t.Fatalf("audit entry before/after = %s/%s, want 50/30", entry.BalanceBefore, entry.BalanceAfter)
	}
}

// TestApplyConfirmedDeposit_WarnsButStillCreditsOverPerUserCap covers Finding 1:
// a confirmed on-chain deposit that would push a user's total over the
// per-user cap must still be credited (the money already moved on-chain) but
// must surface a non-nil capWarning so the caller can log/alert on it.
func TestApplyConfirmedDeposit_WarnsButStillCreditsOverPerUserCap(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-CONFIRMED-DEP-OVERCAP-USER",
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
	amount := decimal.NewFromInt(150) // over the 100 cap on its own
	capCheck := func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error {
		return checker.EvaluateTotals(ctx, userID, amount, currentUserTotal, currentGlobalTotal)
	}

	capWarning, err := repository.ApplyConfirmedDeposit(ctx, created.ID, userID, amount, "confirmed-dep-overcap-user", capCheck)
	if err != nil {
		t.Fatalf("ApplyConfirmedDeposit() error = %v, want nil (a confirmed deposit is never rejected)", err)
	}
	var capErr *caps.CapExceededError
	if capWarning == nil || !isCapExceeded(capWarning, &capErr) {
		t.Fatalf("ApplyConfirmedDeposit() capWarning = %v, want a *caps.CapExceededError", capWarning)
	}
	if capErr.Kind != caps.KindPerUser {
		t.Fatalf("capWarning.Kind = %s, want per_user", capErr.Kind)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.CurrentBalance.Equal(amount) {
		t.Fatalf("CurrentBalance = %s, want %s (confirmed deposit must still be credited despite exceeding the cap)", fetched.CurrentBalance, amount)
	}
}

// TestApplyConfirmedDeposit_WarnsButStillCreditsOverGlobalCap is the global-TVL
// counterpart of the per-user cap test above.
func TestApplyConfirmedDeposit_WarnsButStillCreditsOverGlobalCap(t *testing.T) {
	db := openIntegrationDB(t)
	applyIntegrationMigrations(t, db)
	resetIntegrationTables(t, db)

	repository := NewVaultRepository(db)
	ctx := context.Background()
	userID := seedIntegrationUser(t, db)

	created, err := repository.CreateVault(ctx, vault.Vault{
		ID:              uuid.New(),
		UserID:          userID,
		ContractAddress: "CA-INT-CONFIRMED-DEP-OVERCAP-GLOBAL",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          vault.StatusActive,
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	globalCap := decimal.NewFromInt(100)
	checker := caps.NewChecker(caps.Config{GlobalCap: globalCap}, nil, nil)
	amount := decimal.NewFromInt(150)
	capCheck := func(ctx context.Context, currentUserTotal, currentGlobalTotal decimal.Decimal) error {
		return checker.EvaluateTotals(ctx, userID, amount, currentUserTotal, currentGlobalTotal)
	}

	capWarning, err := repository.ApplyConfirmedDeposit(ctx, created.ID, userID, amount, "confirmed-dep-overcap-global", capCheck)
	if err != nil {
		t.Fatalf("ApplyConfirmedDeposit() error = %v, want nil", err)
	}
	var capErr *caps.CapExceededError
	if capWarning == nil || !isCapExceeded(capWarning, &capErr) {
		t.Fatalf("ApplyConfirmedDeposit() capWarning = %v, want a *caps.CapExceededError", capWarning)
	}
	if capErr.Kind != caps.KindGlobal {
		t.Fatalf("capWarning.Kind = %s, want global", capErr.Kind)
	}

	fetched, err := repository.GetVault(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !fetched.CurrentBalance.Equal(amount) {
		t.Fatalf("CurrentBalance = %s, want %s (confirmed deposit must still be credited despite exceeding the cap)", fetched.CurrentBalance, amount)
	}
}

func isCapExceeded(err error, target **caps.CapExceededError) bool {
	ce, ok := err.(*caps.CapExceededError)
	if ok {
		*target = ce
	}
	return ok
}
