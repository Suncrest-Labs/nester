package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

type fakeChainVerifier struct {
	events map[string]VerifiedVaultEvent
	err    error
}

func (f *fakeChainVerifier) VerifyVaultEvent(_ context.Context, txHash, contractID, eventType string) (VerifiedVaultEvent, error) {
	if f.err != nil {
		return VerifiedVaultEvent{}, f.err
	}
	ev, ok := f.events[txHash]
	if !ok {
		return VerifiedVaultEvent{}, vault.ErrUnverifiedChainTx
	}
	if contractID != "" && ev.ContractID != "" && ev.ContractID != contractID {
		return VerifiedVaultEvent{}, vault.ErrUnverifiedChainTx
	}
	if eventType != "" && ev.EventType != "" && ev.EventType != eventType {
		return VerifiedVaultEvent{}, vault.ErrUnverifiedChainTx
	}
	ev.TxHash = txHash
	return ev, nil
}

type recordingWithdrawInvoker struct {
	calls atomic.Int32
	hash  string
}

func (r *recordingWithdrawInvoker) DepositToVault(context.Context, string, int64) error { return nil }
func (r *recordingWithdrawInvoker) WithdrawFromVault(context.Context, string, int64, int) (string, error) {
	r.calls.Add(1)
	if r.hash == "" {
		return "on-chain-hash", nil
	}
	return r.hash, nil
}
func (r *recordingWithdrawInvoker) PreviewDeposit(context.Context, string, int64) (int64, error) {
	return 0, nil
}
func (r *recordingWithdrawInvoker) PreviewWithdraw(context.Context, string, int64) (int64, error) {
	return 0, nil
}
func (r *recordingWithdrawInvoker) HarvestVault(context.Context, string, string, bool) (string, error) {
	return "", nil
}
func (r *recordingWithdrawInvoker) EmergencyWithdrawAll(context.Context, string) error { return nil }

func TestRecordWithdrawal_ForgedAmountUsesContractEvent(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)
	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID: userID, ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if _, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	svc.SetChainEventVerifier(&fakeChainVerifier{events: map[string]VerifiedVaultEvent{
		"hash-real": {Amount: decimal.RequireFromString("40"), EventType: "withdraw", ContractID: created.ContractAddress},
	}})

	updated, err := svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("10"), // forged smaller than the event
		TxHash:  "hash-real",
	})
	if err != nil {
		t.Fatalf("RecordWithdrawal: %v", err)
	}
	if !updated.CurrentBalance.Equal(decimal.RequireFromString("60")) {
		t.Fatalf("balance = %s, want 60 (event amount 40, not request body 10)", updated.CurrentBalance)
	}

	txns, err := repo.ListUserVaultTransactions(context.Background(), userID, created.ID)
	if err != nil {
		t.Fatalf("ListUserVaultTransactions: %v", err)
	}
	var recorded decimal.Decimal
	for _, txn := range txns {
		if txn.Type == "withdrawal" {
			recorded = txn.Amount
		}
	}
	if !recorded.Equal(decimal.RequireFromString("40")) {
		t.Fatalf("recorded withdrawal = %s, want 40 from the contract event", recorded)
	}
}

func TestRecordWithdrawal_ReplayedHashRejected(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)
	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID: userID, ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if _, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	svc.SetChainEventVerifier(&fakeChainVerifier{events: map[string]VerifiedVaultEvent{
		"hash-once": {Amount: decimal.RequireFromString("25"), EventType: "withdraw", ContractID: created.ContractAddress},
	}})

	if _, err := svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("25"), TxHash: "hash-once",
	}); err != nil {
		t.Fatalf("first withdrawal: %v", err)
	}
	_, err = svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("25"), TxHash: "hash-once",
	})
	if !errors.Is(err, vault.ErrDuplicateTransaction) {
		t.Fatalf("replay err = %v, want ErrDuplicateTransaction", err)
	}

	updated, err := svc.GetVault(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetVault: %v", err)
	}
	if !updated.CurrentBalance.Equal(decimal.RequireFromString("75")) {
		t.Fatalf("balance = %s, want 75 (replay must not debit again)", updated.CurrentBalance)
	}
}

func TestRecordWithdrawal_PartialWithdrawalLeavesRemainder(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)
	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID: userID, ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if _, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	svc.SetChainEventVerifier(&fakeChainVerifier{events: map[string]VerifiedVaultEvent{
		"hash-partial": {Amount: decimal.RequireFromString("30"), EventType: "withdraw", ContractID: created.ContractAddress},
	}})

	updated, err := svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("30"), TxHash: "hash-partial",
	})
	if err != nil {
		t.Fatalf("RecordWithdrawal: %v", err)
	}
	if !updated.CurrentBalance.Equal(decimal.RequireFromString("70")) {
		t.Fatalf("balance = %s, want 70 after partial withdrawal", updated.CurrentBalance)
	}
	if !updated.TotalDeposited.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("total_deposited = %s, want 100 (withdrawals never reverse deposits)", updated.TotalDeposited)
	}
}

func TestRecordWithdrawal_ExceedingPositionRejectedBeforeSubmit(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)
	invoker := &recordingWithdrawInvoker{}
	svc.SetDepositInvoker(invoker)

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID: userID, ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	// This test needs a funded position, not an operator-funded deposit, but
	// a server-submitted deposit is refused by default now (nester#1152), so
	// the setup opts in explicitly for this one vault.
	svc.SetOperatorFundedDepositPolicy(allowOperatorFundedForTest(created.ID))
	if _, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("50"),
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	_, err = svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("80"),
	})
	if !errors.Is(err, vault.ErrWithdrawalExceedsPosition) {
		t.Fatalf("err = %v, want ErrWithdrawalExceedsPosition", err)
	}
	if invoker.calls.Load() != 0 {
		t.Fatalf("invoker called %d times, want 0 (must reject before on-chain submit)", invoker.calls.Load())
	}
}

func TestRecordWithdrawal_RequiresHashWhenVerifierWired(t *testing.T) {
	userID := uuid.New()
	repo := newMemoryVaultRepository(userID)
	svc := NewVaultService(repo)
	// Deposits are verified too now (nester#1075), so the seed needs a hash
	// the verifier recognises.
	svc.SetChainEventVerifier(&fakeChainVerifier{events: map[string]VerifiedVaultEvent{
		"seed-deposit": {EventType: "deposit", Amount: decimal.RequireFromString("50")},
	}})

	created, err := svc.CreateVault(context.Background(), CreateVaultInput{
		UserID: userID, ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if _, err := svc.RecordDeposit(context.Background(), RecordDepositInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("50"), TxHash: "seed-deposit",
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	_, err = svc.RecordWithdrawal(context.Background(), RecordWithdrawalInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("10"),
	})
	if !errors.Is(err, vault.ErrTxHashRequired) {
		t.Fatalf("err = %v, want ErrTxHashRequired", err)
	}
}
