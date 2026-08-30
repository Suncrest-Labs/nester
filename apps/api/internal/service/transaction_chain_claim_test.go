package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
)

const claimVaultContract = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"

// fakeVaultLookup resolves a single vault, or fails, so the claim path can be
// exercised without a database.
type fakeVaultLookup struct {
	v   vault.Vault
	err error
}

func (f fakeVaultLookup) GetVault(_ context.Context, _ uuid.UUID) (vault.Vault, error) {
	if f.err != nil {
		return vault.Vault{}, f.err
	}
	return f.v, nil
}

// claimHorizonStub serves both the transaction lookup (always successful) and
// its operations, so a test can post a genuinely successful hash whose
// operations do or do not back the claim.
func claimHorizonStub(t *testing.T, hash string, ops []horizonOperation) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/transactions/")
		w.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(path, "/operations") {
			if strings.TrimSuffix(path, "/operations") != hash {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var payload horizonOperationsResponse
			payload.Embedded.Records = ops
			_ = json.NewEncoder(w).Encode(payload)
			return
		}

		if path != hash {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"successful":true,"created_at":"` +
			time.Now().UTC().Format(time.RFC3339) + `","result_xdr":""}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// newClaimService builds a deposit transaction for `claimed` units of USDC and
// a service whose Horizon reports the transaction successful with ops.
func newClaimService(t *testing.T, hash string, claimed decimal.Decimal, ops []horizonOperation) (*TransactionService, *fakeTransactionRepo, *fakeBalanceApplier, transaction.Transaction) {
	t.Helper()

	tx := newPendingTx(transaction.TypeDeposit, hash, time.Minute)
	tx.Amount = claimed
	repo := newFakeTransactionRepo(tx)

	svc := NewTransactionService(repo, claimHorizonStub(t, hash, ops))
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)
	svc.SetVaultLookup(fakeVaultLookup{v: vault.Vault{
		ID:              tx.VaultID,
		ContractAddress: claimVaultContract,
		Currency:        "USDC",
	}})
	return svc, repo, applier, tx
}

func paymentOp(to, code, amount string) horizonOperation {
	return horizonOperation{
		Type:        "payment",
		AssetType:   "credit_alphanum4",
		AssetCode:   code,
		AssetIssuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		Amount:      amount,
		To:          to,
	}
}

// assertRejected asserts the transaction was marked failed with the given
// typed reason and that no balance moved.
func assertRejected(t *testing.T, repo *fakeTransactionRepo, applier *fakeBalanceApplier, hash, wantReason string) {
	t.Helper()

	stored, err := repo.GetByHash(context.Background(), hash)
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.Status != transaction.StatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}
	if stored.ErrorReason != wantReason {
		t.Errorf("error reason = %q, want %q", stored.ErrorReason, wantReason)
	}
	if applier.depositCount() != 0 {
		t.Fatalf("balance was credited %d time(s) for a rejected claim", applier.depositCount())
	}
}

// A real, successful, but entirely unrelated transaction hash must not credit
// anything. This is the core nester#1145 exploit: no forged amount needed.
func TestReconcile_UnrelatedSuccessfulHash_IsRejected(t *testing.T) {
	const hash = "unrelated-success"
	svc, repo, applier, tx := newClaimService(t, hash, decimal.NewFromInt(500), []horizonOperation{
		paymentOp("GBSOMEONEELSEACCOUNTADDRESSXXXXXXXXXXXXXXXXXXXXXXXXXXXXX", "USDC", "500.0000000"),
	})

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}
	assertRejected(t, repo, applier, hash, transaction.ReasonDestinationMismatch)
}

// A transaction that really did pay the vault, but for less than claimed, must
// not credit the inflated figure.
func TestReconcile_InflatedAmount_IsRejected(t *testing.T) {
	const hash = "inflated-amount"
	svc, repo, applier, tx := newClaimService(t, hash, decimal.NewFromInt(1_000_000), []horizonOperation{
		paymentOp(claimVaultContract, "USDC", "1.0000000"),
	})

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}
	assertRejected(t, repo, applier, hash, transaction.ReasonAmountMismatch)
}

func TestReconcile_WrongAsset_IsRejected(t *testing.T) {
	const hash = "wrong-asset"
	svc, repo, applier, tx := newClaimService(t, hash, decimal.NewFromInt(100), []horizonOperation{
		paymentOp(claimVaultContract, "EURC", "100.0000000"),
	})

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}
	assertRejected(t, repo, applier, hash, transaction.ReasonAssetMismatch)
}

func TestReconcile_WrongDestination_IsRejected(t *testing.T) {
	const hash = "wrong-destination"
	svc, repo, applier, tx := newClaimService(t, hash, decimal.NewFromInt(100), []horizonOperation{
		paymentOp("CBQHNAXSI55GX2GN6D67GK7BHVPSLJUGZQEU7WJ5LKR5PNUCGLIMAO4K", "USDC", "100.0000000"),
	})

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}
	assertRejected(t, repo, applier, hash, transaction.ReasonDestinationMismatch)
}

// A transaction carrying no operations at all is rejected with the
// no-operation reason rather than being credited.
func TestReconcile_NoOperations_IsRejected(t *testing.T) {
	const hash = "no-operations"
	svc, repo, applier, tx := newClaimService(t, hash, decimal.NewFromInt(100), nil)

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}
	assertRejected(t, repo, applier, hash, transaction.ReasonNoMatchingOperation)
}

// Non-transfer operation types must not satisfy a claim even when they carry a
// matching-looking amount.
func TestReconcile_NonTransferOperation_IsRejected(t *testing.T) {
	const hash = "manage-data-only"
	svc, repo, applier, tx := newClaimService(t, hash, decimal.NewFromInt(100), []horizonOperation{
		{Type: "manage_data", AssetType: "credit_alphanum4", AssetCode: "USDC", Amount: "100.0000000", To: claimVaultContract},
	})

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}
	assertRejected(t, repo, applier, hash, transaction.ReasonDestinationMismatch)
}

// The matching case: the credited amount is the on-chain operation amount.
func TestReconcile_MatchingTransaction_CreditsOnChainAmount(t *testing.T) {
	const hash = "matching-deposit"
	svc, repo, applier, tx := newClaimService(t, hash, decimal.RequireFromString("250.5"), []horizonOperation{
		paymentOp("GBSOMEOTHERACCOUNTXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX", "USDC", "9999.0000000"),
		paymentOp(claimVaultContract, "USDC", "250.5000000"),
	})

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}

	stored, err := repo.GetByHash(context.Background(), hash)
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if stored.Status != transaction.StatusCompleted {
		t.Fatalf("status = %q, want completed", stored.Status)
	}
	if applier.depositCount() != 1 {
		t.Fatalf("expected exactly 1 credit, got %d", applier.depositCount())
	}
	if got := applier.deposits[0].amount; !got.Equal(decimal.RequireFromString("250.5")) {
		t.Errorf("credited %s, want the on-chain 250.5", got)
	}
}

// An operation must match the claim exactly, so a mismatch in either direction
// is rejected rather than silently crediting the smaller figure.
func TestReconcile_UnderstatedAmount_IsRejected(t *testing.T) {
	const hash = "understated-amount"
	svc, repo, applier, tx := newClaimService(t, hash, decimal.NewFromInt(1), []horizonOperation{
		paymentOp(claimVaultContract, "USDC", "100.0000000"),
	})

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}
	assertRejected(t, repo, applier, hash, transaction.ReasonAmountMismatch)
}

// When the vault cannot be resolved the transaction stays pending and nothing
// is credited: an unverifiable claim must never fall through to a credit.
func TestReconcile_VaultUnresolvable_StaysPendingAndCreditsNothing(t *testing.T) {
	const hash = "vault-missing"
	tx := newPendingTx(transaction.TypeDeposit, hash, time.Minute)
	repo := newFakeTransactionRepo(tx)

	svc := NewTransactionService(repo, claimHorizonStub(t, hash, []horizonOperation{
		paymentOp(claimVaultContract, "USDC", "100.0000000"),
	}))
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)
	svc.SetVaultLookup(fakeVaultLookup{err: errors.New("vault gone")})

	if _, changed, err := svc.ReconcileTransaction(context.Background(), tx); err == nil || changed {
		t.Fatalf("expected a transient error and no status change, got err=%v changed=%v", err, changed)
	}

	stored, _ := repo.GetByHash(context.Background(), hash)
	if stored.Status != transaction.StatusPending {
		t.Errorf("status = %q, want pending", stored.Status)
	}
	if applier.depositCount() != 0 {
		t.Errorf("credited %d time(s) despite an unresolvable vault", applier.depositCount())
	}
}

// Withdrawals move value out of the vault, not into its contract address, so
// they are not subject to the deposit claim check.
func TestReconcile_Withdrawal_IsNotClaimChecked(t *testing.T) {
	const hash = "withdrawal-tx"
	tx := newPendingTx(transaction.TypeWithdrawal, hash, time.Minute)
	repo := newFakeTransactionRepo(tx)

	svc := NewTransactionService(repo, claimHorizonStub(t, hash, nil))
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)
	svc.SetVaultLookup(fakeVaultLookup{v: vault.Vault{
		ID: tx.VaultID, ContractAddress: claimVaultContract, Currency: "USDC",
	}})

	if _, _, err := svc.ReconcileTransaction(context.Background(), tx); err != nil {
		t.Fatalf("ReconcileTransaction: %v", err)
	}
	if applier.withdrawalCount() != 1 {
		t.Fatalf("expected the withdrawal to still apply, got %d", applier.withdrawalCount())
	}
}
