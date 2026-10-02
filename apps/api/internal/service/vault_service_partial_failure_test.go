package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

// failOnceInvoker succeeds on-chain unconditionally, modeling the scenario
// where DepositToVault/WithdrawFromVault land on chain before the downstream
// repository write fails (nester#1315).
type failOnceInvoker struct{}

func (failOnceInvoker) DepositToVault(context.Context, string, int64) error { return nil }
func (failOnceInvoker) WithdrawFromVault(_ context.Context, _ string, _ int64, _ int) (string, error) {
	return "on-chain-hash", nil
}
func (failOnceInvoker) PreviewDeposit(context.Context, string, int64) (int64, error)  { return 0, nil }
func (failOnceInvoker) PreviewWithdraw(context.Context, string, int64) (int64, error) { return 0, nil }
func (failOnceInvoker) HarvestVault(context.Context, string, string, bool) (string, error) {
	return "", nil
}
func (failOnceInvoker) EmergencyWithdrawAll(context.Context, string) error { return nil }

var errRepoWriteFailed = errors.New("repository write failed")

// recordFailureRepo wraps memoryVaultRepository and fails the configured
// RecordDeposit/RecordWithdrawal call after the on-chain leg has already
// succeeded, simulating a downstream error mid-transaction.
type recordFailureRepo struct {
	*memoryVaultRepository
	failDeposit    bool
	failWithdrawal bool
}

func (r *recordFailureRepo) RecordDeposit(ctx context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	if r.failDeposit {
		return errRepoWriteFailed
	}
	return r.memoryVaultRepository.RecordDeposit(ctx, id, record)
}

func (r *recordFailureRepo) RecordWithdrawal(ctx context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	if r.failWithdrawal {
		return errRepoWriteFailed
	}
	return r.memoryVaultRepository.RecordWithdrawal(ctx, id, record)
}

// TestRecordDeposit_OnChainSucceedsRepositoryFails documents current behavior
// when a deposit lands on chain but the repository write that records it
// fails: the funds moved, but RecordDeposit returns an error and the vault's
// balances are left unchanged, so the deposit is invisible to the user and
// not retried or compensated (nester#1315).
func TestRecordDeposit_OnChainSucceedsRepositoryFails(t *testing.T) {
	userID := uuid.New()
	base := newMemoryVaultRepository(userID)
	repo := &recordFailureRepo{memoryVaultRepository: base, failDeposit: true}

	service := NewVaultService(repo)
	service.SetDepositInvoker(failOnceInvoker{})

	created, err := service.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CA123",
		Currency:        "usdc",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	service.SetOperatorFundedDepositPolicy(NewOperatorFundedDepositPolicy(
		true, []uuid.UUID{created.ID}, decimal.NewFromInt(1000), discardLogger(),
	))

	_, err = service.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
	})
	if !errors.Is(err, errRepoWriteFailed) {
		t.Fatalf("RecordDeposit() error = %v, want %v", err, errRepoWriteFailed)
	}

	after, err := repo.GetVault(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !after.CurrentBalance.IsZero() {
		t.Fatalf("expected balance unchanged at zero after failed record, got %s", after.CurrentBalance)
	}
}

// TestRecordWithdrawal_OnChainSucceedsRepositoryFails mirrors the deposit case
// for withdrawals: the contract already burned shares and returned a tx hash
// before the repository write fails, so the on-chain withdrawal has no
// corresponding ledger entry (nester#1315).
func TestRecordWithdrawal_OnChainSucceedsRepositoryFails(t *testing.T) {
	userID := uuid.New()
	base := newMemoryVaultRepository(userID)
	repo := &recordFailureRepo{memoryVaultRepository: base, failWithdrawal: true}

	service := NewVaultService(repo)
	service.SetDepositInvoker(failOnceInvoker{})

	created, err := service.CreateVault(context.Background(), CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CA123",
		Currency:        "usdc",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	service.SetOperatorFundedDepositPolicy(NewOperatorFundedDepositPolicy(
		true, []uuid.UUID{created.ID}, decimal.NewFromInt(1000), discardLogger(),
	))

	if _, err := service.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("seed RecordDeposit() error = %v", err)
	}
	repo.failWithdrawal = true

	_, err = service.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("40"),
	})
	if !errors.Is(err, errRepoWriteFailed) {
		t.Fatalf("RecordWithdrawal() error = %v, want %v", err, errRepoWriteFailed)
	}

	after, err := repo.GetVault(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetVault() error = %v", err)
	}
	if !after.CurrentBalance.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("expected balance unchanged at 100 after failed record, got %s", after.CurrentBalance)
	}
}
