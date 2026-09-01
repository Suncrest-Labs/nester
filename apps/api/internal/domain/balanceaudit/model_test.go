package balanceaudit

import (
	"errors"
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

	got, err := Reconcile(entries)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
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
	got, err := Reconcile(nil)
	if err != nil {
		t.Fatalf("Reconcile(nil) error = %v", err)
	}
	if !got.IsZero() {
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
	replayed, err := Reconcile(entries)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	actualCurrentBalance := dec("140") // an unrecorded +40 happened out of band
	if replayed.Equal(actualCurrentBalance) {
		t.Fatal("expected replayed total to diverge from an unreconciled live balance")
	}
}

// TestReconcile_DetectsChainGap is the CodeRabbit-flagged case: a ledger
// whose recorded before/after values sum to the correct live balance by
// coincidence, even though the chain between two entries is broken (the
// second entry's BalanceBefore does not match the first entry's
// BalanceAfter — a balance change happened that was never recorded, or was
// recorded out of order). Reconcile must reject this instead of silently
// returning the coincidentally-correct sum.
func TestReconcile_DetectsChainGap(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()

	entries := []Entry{
		{VaultID: vaultID, UserID: userID, Operation: OperationDeposit,
			Amount: dec("100"), BalanceBefore: dec("0"), BalanceAfter: dec("100")},
		// Gap: an out-of-band change moved the balance from 100 to 50 before
		// this entry, but that change was never recorded. This entry's sum
		// contribution (75-50=25) still happens to make the total (125)
		// match a live balance of 125, which is exactly what the chain
		// check must catch.
		{VaultID: vaultID, UserID: userID, Operation: OperationWithdrawal,
			Amount: dec("25"), BalanceBefore: dec("50"), BalanceAfter: dec("75")},
	}

	if _, err := Reconcile(entries); !errors.Is(err, ErrReconciliationGap) {
		t.Fatalf("Reconcile() error = %v, want ErrReconciliationGap", err)
	}
}

// TestReconcile_OpeningBalanceEntryAccountsForPreexistingBalance verifies
// that a vault which already held a nonzero balance before the ledger
// existed reconciles correctly once migration 118's opening-balance entry is
// present — i.e. Reconcile only works because every vault's entry chain
// starts from a true balance-before-history of zero (nester#1124,
// CodeRabbit finding).
func TestReconcile_OpeningBalanceEntryAccountsForPreexistingBalance(t *testing.T) {
	vaultID := uuid.New()
	userID := uuid.New()

	entries := []Entry{
		// The vault already held 500 before the audit trail existed.
		{VaultID: vaultID, UserID: userID, Actor: "system:migration", Operation: OperationOpeningBalance,
			Amount: dec("500"), BalanceBefore: dec("0"), BalanceAfter: dec("500")},
		{VaultID: vaultID, UserID: userID, Actor: userID.String(), Operation: OperationDeposit,
			Amount: dec("25"), BalanceBefore: dec("500"), BalanceAfter: dec("525")},
	}

	got, err := Reconcile(entries)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	want := dec("525")
	if !got.Equal(want) {
		t.Fatalf("Reconcile() = %s, want %s (opening balance entry must be included)", got, want)
	}

	// Without the opening entry, replaying only the deposit would reconcile
	// to 25, not 525 — demonstrating why the opening entry is required.
	withoutOpening, err := Reconcile(entries[1:])
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if withoutOpening.Equal(want) {
		t.Fatalf("expected replay without the opening entry to diverge from %s, got %s", want, withoutOpening)
	}
}
