package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

// fakeBalanceApplier records confirmed balance applications and can be made to
// fail to exercise the retry/no-double-apply path.
type fakeBalanceApplier struct {
	mu          sync.Mutex
	deposits    []balanceCall
	withdrawals []balanceCall
	err         error
}

type balanceCall struct {
	vaultID uuid.UUID
	amount  decimal.Decimal
	hash    string
}

func (f *fakeBalanceApplier) ApplyConfirmedDeposit(_ context.Context, vaultID uuid.UUID, amount decimal.Decimal, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.deposits = append(f.deposits, balanceCall{vaultID, amount, hash})
	return nil
}

func (f *fakeBalanceApplier) ApplyConfirmedWithdrawal(_ context.Context, vaultID uuid.UUID, amount decimal.Decimal, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.withdrawals = append(f.withdrawals, balanceCall{vaultID, amount, hash})
	return nil
}

func (f *fakeBalanceApplier) depositCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deposits)
}

func (f *fakeBalanceApplier) withdrawalCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.withdrawals)
}

func newConfirmedHorizon(t *testing.T, hash string, successful bool) string {
	t.Helper()
	resp := horizonTransactionResponse{CreatedAt: time.Now().UTC().Format(time.RFC3339), Successful: successful}
	if !successful {
		resp.ResultXdr = "AAAAreverted"
	}
	srv := horizonStub(t, map[string]horizonTransactionResponse{hash: resp})
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestConfirmedDeposit_CreditsBalanceThenMarksCompleted(t *testing.T) {
	tx := newPendingTx(transaction.TypeDeposit, "dep-confirm", time.Minute)
	repo := newFakeTransactionRepo(tx)
	svc := NewTransactionService(repo, newConfirmedHorizon(t, "dep-confirm", true))
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)

	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)
	poller.Tick(context.Background())

	stored, _ := repo.GetByHash(context.Background(), "dep-confirm")
	if stored.Status != transaction.StatusCompleted {
		t.Fatalf("status = %q, want completed", stored.Status)
	}
	if applier.depositCount() != 1 {
		t.Fatalf("expected 1 deposit credit, got %d", applier.depositCount())
	}
	if applier.deposits[0].vaultID != tx.VaultID || !applier.deposits[0].amount.Equal(tx.Amount) || applier.deposits[0].hash != "dep-confirm" {
		t.Errorf("unexpected deposit credit: %+v", applier.deposits[0])
	}
}

func TestConfirmedWithdrawal_DebitsBalance(t *testing.T) {
	tx := newPendingTx(transaction.TypeWithdrawal, "wd-confirm", time.Minute)
	repo := newFakeTransactionRepo(tx)
	svc := NewTransactionService(repo, newConfirmedHorizon(t, "wd-confirm", true))
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)

	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)
	poller.Tick(context.Background())

	stored, _ := repo.GetByHash(context.Background(), "wd-confirm")
	if stored.Status != transaction.StatusCompleted {
		t.Fatalf("status = %q, want completed", stored.Status)
	}
	if applier.withdrawalCount() != 1 || applier.depositCount() != 0 {
		t.Fatalf("expected 1 withdrawal debit and 0 deposits, got %d / %d", applier.withdrawalCount(), applier.depositCount())
	}
}

// Criterion 5: Soroban returned a successful submission (the tx exists on
// Horizon) but the ledger result is a failure. The DB must show failed, and
// the balance must never be touched.
func TestSuccessfulSubmitButFailedLedger_MarksFailedAndLeavesBalanceUntouched(t *testing.T) {
	tx := newPendingTx(transaction.TypeDeposit, "dep-revert", time.Minute)
	repo := newFakeTransactionRepo(tx)
	svc := NewTransactionService(repo, newConfirmedHorizon(t, "dep-revert", false))
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)

	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)
	poller.Tick(context.Background())

	stored, _ := repo.GetByHash(context.Background(), "dep-revert")
	if stored.Status != transaction.StatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}
	if stored.Status == transaction.StatusCompleted {
		t.Fatal("a failed ledger result must never be recorded as completed")
	}
	if stored.ErrorReason == "" {
		t.Error("expected a failure reason to be recorded")
	}
	if applier.depositCount() != 0 || applier.withdrawalCount() != 0 {
		t.Fatalf("balance must not move for a failed transaction; deposits=%d withdrawals=%d",
			applier.depositCount(), applier.withdrawalCount())
	}
}

// If the balance application fails, the transaction must stay pending so the
// next poll retries — it must NOT be marked completed (which would strand the
// balance forever). The idempotent applier makes the retry safe.
func TestBalanceApplyFailure_LeavesTransactionPendingForRetry(t *testing.T) {
	tx := newPendingTx(transaction.TypeDeposit, "dep-retry", time.Minute)
	repo := newFakeTransactionRepo(tx)
	svc := NewTransactionService(repo, newConfirmedHorizon(t, "dep-retry", true))
	applier := &fakeBalanceApplier{err: errors.New("vault db unavailable")}
	svc.SetBalanceApplier(applier)

	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)

	poller.Tick(context.Background())
	stored, _ := repo.GetByHash(context.Background(), "dep-retry")
	if stored.Status != transaction.StatusPending {
		t.Fatalf("after a failed balance apply, status = %q, want pending", stored.Status)
	}

	// Recover and retry — now it should confirm and credit.
	applier.mu.Lock()
	applier.err = nil
	applier.mu.Unlock()

	poller.Tick(context.Background())
	stored, _ = repo.GetByHash(context.Background(), "dep-retry")
	if stored.Status != transaction.StatusCompleted {
		t.Fatalf("after recovery, status = %q, want completed", stored.Status)
	}
	if applier.depositCount() != 1 {
		t.Fatalf("expected exactly 1 successful credit after retry, got %d", applier.depositCount())
	}
}

// Initial registration must always be pending and must never move balance.
func TestRegisterTransaction_AlwaysPendingNoBalance(t *testing.T) {
	repo := newFakeTransactionRepo()
	svc := NewTransactionService(repo, "")
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)

	tx, err := svc.RegisterTransaction(context.Background(), RegisterTransactionInput{
		VaultID:  uuid.New(),
		Type:     transaction.TypeDeposit,
		Amount:   decimal.NewFromInt(100),
		Currency: "USDC",
		TxHash:   "reg-1",
	})
	if err != nil {
		t.Fatalf("RegisterTransaction: %v", err)
	}
	if tx.Status != transaction.StatusPending {
		t.Fatalf("status = %q, want pending", tx.Status)
	}
	if applier.depositCount() != 0 || applier.withdrawalCount() != 0 {
		t.Fatal("registration must not move balance")
	}
}
