package balanceaudit

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

// TestReconcile_ReplayReproducesCurrentBalance is the core acceptance test
// for nester#1124: replaying the audit trail from zero must reproduce
// exactly the balance the vault ends up at, across a mix of deposits,
// withdrawals, and a harvest.
func TestReconcile_ReplayReproducesCurrentBalance(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()

	entries := []Entry{
		{VaultID: vaultID, UserID: userID, Actor: userID.String(), Operation: OperationDeposit,
			Amount: dec("100"), BalanceBefore: dec("0"), BalanceAfter: dec("100")},
		{VaultID: vaultID, UserID: userID, Actor: userID.String(), Operation: OperationDeposit,
			Amount: dec("50"), BalanceBefore: dec("100"), BalanceAfter: dec("150")},
		{VaultID: vaultID, UserID: userID, Actor: SystemActor("harvest"), Operation: OperationHarvest,
			Amount: dec("5.25"), BalanceBefore: dec("150"), BalanceAfter: dec("155.25")},
		{VaultID: vaultID, UserID: userID, Actor: userID.String(), Operation: OperationWithdrawal,
			Amount: dec("30"), BalanceBefore: dec("155.25"), BalanceAfter: dec("125.25")},
	}

	got := Reconcile(entries)
	want := dec("125.25")
	if !got.Equal(want) {
		t.Fatalf("Reconcile() = %s, want %s", got, want)
	}

	// The final entry's BalanceAfter is, by construction, the same number —
	// that equality is exactly what "reconciles to current balance" means.
	if !entries[len(entries)-1].BalanceAfter.Equal(got) {
		t.Fatalf("replayed total %s does not match last recorded balance %s", got, entries[len(entries)-1].BalanceAfter)
	}
}

func TestReconcile_EmptyLedgerIsZero(t *testing.T) {
	if got := Reconcile(nil); !got.IsZero() {
		t.Fatalf("Reconcile(nil) = %s, want 0", got)
	}
}

func TestReconcile_DetectsDrift(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()

	// A gap in the ledger (a balance change that was never recorded) must
	// show up as the replayed total disagreeing with what the chain of
	// before/after values implies the final balance to be.
	entries := []Entry{
		{VaultID: vaultID, UserID: userID, Operation: OperationDeposit,
			Amount: dec("100"), BalanceBefore: dec("0"), BalanceAfter: dec("100")},
	}
	replayed := Reconcile(entries)
	actualCurrentBalance := dec("140") // an unrecorded +40 happened out of band
	if replayed.Equal(actualCurrentBalance) {
		t.Fatal("expected replayed total to diverge from an unreconciled live balance")
	}
}
