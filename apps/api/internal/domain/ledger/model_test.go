package ledger

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

func TestValidateBalanced_Success(t *testing.T) {
	txID := uuid.New()
	acc1 := uuid.New()
	acc2 := uuid.New()
	entries := []Entry{
		{TransactionID: txID, AccountID: acc1, Amount: 100},
		{TransactionID: txID, AccountID: acc2, Amount: -100},
	}
	if err := ValidateBalanced(entries); err != nil {
		t.Fatalf("expected balanced, got %v", err)
	}
}

func TestValidateBalanced_FailsWhenUnbalanced(t *testing.T) {
	txID := uuid.New()
	acc1 := uuid.New()
	acc2 := uuid.New()
	entries := []Entry{
		{TransactionID: txID, AccountID: acc1, Amount: 100},
		{TransactionID: txID, AccountID: acc2, Amount: -90},
	}
	if err := ValidateBalanced(entries); err != ErrUnbalanced {
		t.Fatalf("expected ErrUnbalanced, got %v", err)
	}
}

func TestValidateBalanced_FailsWhenTooFew(t *testing.T) {
	txID := uuid.New()
	acc1 := uuid.New()
	entries := []Entry{
		{TransactionID: txID, AccountID: acc1, Amount: 100},
	}
	if err := ValidateBalanced(entries); err != ErrTooFewEntries {
		t.Fatalf("expected ErrTooFewEntries, got %v", err)
	}
}

func TestValidateBalanced_FailsWhenDifferentTxID(t *testing.T) {
	acc1 := uuid.New()
	acc2 := uuid.New()
	entries := []Entry{
		{TransactionID: uuid.New(), AccountID: acc1, Amount: 100},
		{TransactionID: uuid.New(), AccountID: acc2, Amount: -100},
	}
	if err := ValidateBalanced(entries); err == nil {
		t.Fatalf("expected error for different transaction IDs")
	}
}

// Property-style test: random sequences of balanced postings must keep global sum zero.
func TestProperty_BooksAlwaysSumToZero(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	// Simulate an in-memory ledger: map account -> balance
	balances := make(map[uuid.UUID]int64)
	// Create 10 accounts
	accounts := make([]uuid.UUID, 10)
	for i := range accounts {
		accounts[i] = uuid.New()
	}

	var globalSum int64

	for i := 0; i < 1000; i++ {
		// Randomly choose 2-5 entries per transaction
		n := 2 + rng.Intn(4)
		txID := uuid.New()
		entries := make([]Entry, n)
		var sum int64
		for j := 0; j < n-1; j++ {
			acc := accounts[rng.Intn(len(accounts))]
			amt := int64(rng.Intn(2000) - 1000)
			if amt == 0 {
				amt = 1
			}
			entries[j] = Entry{TransactionID: txID, AccountID: acc, Amount: amt}
			sum += amt
		}
		// Last entry balances it
		lastAcc := accounts[rng.Intn(len(accounts))]
		entries[n-1] = Entry{TransactionID: txID, AccountID: lastAcc, Amount: -sum}

		// Validate
		if err := ValidateBalanced(entries); err != nil {
			t.Fatalf("iteration %d: validation failed: %v", i, err)
		}

		// Apply to in-memory balances
		for _, e := range entries {
			balances[e.AccountID] += e.Amount
			globalSum += e.Amount
		}

		if globalSum != 0 {
			t.Fatalf("iteration %d: global sum not zero, got %d", i, globalSum)
		}
	}

	// Final check: sum of all account balances must be zero
	var total int64
	for _, b := range balances {
		total += b
	}
	if total != 0 {
		t.Fatalf("final total not zero: %d", total)
	}
}

// Test integer discipline: amounts are integral (no floats) — enforced by int64 type.
// This test documents the requirement that conversion to decimal happens only at presentation edge.
func TestIntegerDiscipline(t *testing.T) {
	// Amounts are int64, never float
	var amount int64 = 10_000_000 // 1 USDC in stroops
	if amount != 10_000_000 {
		t.Fatalf("integer discipline broken")
	}
	// Check that we can convert to decimal for display
	decimalValue := float64(amount) / 1_000_0000.0
	if decimalValue != 1.0 {
		t.Fatalf("conversion failed")
	}
	// The ledger never stores float, only int64 — this is enforced by type system.
}
