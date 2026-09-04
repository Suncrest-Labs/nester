package caps

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// stubTotals is an in-memory caps.Totals for tests.
type stubTotals struct {
	userTotals  map[uuid.UUID]decimal.Decimal
	globalTotal decimal.Decimal
}

func (s *stubTotals) UserDepositTotal(_ context.Context, userID uuid.UUID) (decimal.Decimal, error) {
	if v, ok := s.userTotals[userID]; ok {
		return v, nil
	}
	return decimal.Zero, nil
}

func (s *stubTotals) GlobalDepositTotal(_ context.Context) (decimal.Decimal, error) {
	return s.globalTotal, nil
}

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestChecker_PerUserCap_UnderCapAllowed(t *testing.T) {
	userID := uuid.New()
	totals := &stubTotals{userTotals: map[uuid.UUID]decimal.Decimal{userID: dec("900")}}
	checker := NewChecker(Config{PerUserCap: dec("1000")}, totals, nil)

	if err := checker.CheckDeposit(context.Background(), userID, dec("50")); err != nil {
		t.Fatalf("expected deposit under cap to be allowed, got %v", err)
	}
}

func TestChecker_PerUserCap_AtCapAllowed(t *testing.T) {
	userID := uuid.New()
	totals := &stubTotals{userTotals: map[uuid.UUID]decimal.Decimal{userID: dec("900")}}
	checker := NewChecker(Config{PerUserCap: dec("1000")}, totals, nil)

	// 900 + 100 == 1000 exactly: landing on the cap must be allowed, not
	// rejected — only strictly exceeding it is refused.
	if err := checker.CheckDeposit(context.Background(), userID, dec("100")); err != nil {
		t.Fatalf("expected deposit landing exactly on cap to be allowed, got %v", err)
	}
}

func TestChecker_PerUserCap_OverCapRejected(t *testing.T) {
	userID := uuid.New()
	totals := &stubTotals{userTotals: map[uuid.UUID]decimal.Decimal{userID: dec("900")}}
	checker := NewChecker(Config{PerUserCap: dec("1000")}, totals, nil)

	err := checker.CheckDeposit(context.Background(), userID, dec("100.01"))
	if err == nil {
		t.Fatal("expected deposit exceeding cap to be rejected")
	}
	if !errors.Is(err, ErrPerUserCapExceeded) {
		t.Fatalf("expected ErrPerUserCapExceeded, got %v", err)
	}
	var capErr *CapExceededError
	if !errors.As(err, &capErr) {
		t.Fatalf("expected *CapExceededError, got %T", err)
	}
	if capErr.Kind != KindPerUser {
		t.Fatalf("kind = %v, want KindPerUser", capErr.Kind)
	}
	if !capErr.CurrentTotal.Equal(dec("900")) {
		t.Fatalf("current total = %v, want 900", capErr.CurrentTotal)
	}
}

func TestChecker_GlobalCap_BoundaryConditions(t *testing.T) {
	userID := uuid.New()

	t.Run("under cap allowed", func(t *testing.T) {
		totals := &stubTotals{globalTotal: dec("49000")}
		checker := NewChecker(Config{GlobalCap: dec("50000")}, totals, nil)
		if err := checker.CheckDeposit(context.Background(), userID, dec("500")); err != nil {
			t.Fatalf("expected allowed, got %v", err)
		}
	})

	t.Run("at cap allowed", func(t *testing.T) {
		totals := &stubTotals{globalTotal: dec("49000")}
		checker := NewChecker(Config{GlobalCap: dec("50000")}, totals, nil)
		if err := checker.CheckDeposit(context.Background(), userID, dec("1000")); err != nil {
			t.Fatalf("expected allowed at exact cap, got %v", err)
		}
	})

	t.Run("over cap rejected", func(t *testing.T) {
		totals := &stubTotals{globalTotal: dec("49000")}
		checker := NewChecker(Config{GlobalCap: dec("50000")}, totals, nil)
		err := checker.CheckDeposit(context.Background(), userID, dec("1000.01"))
		if !errors.Is(err, ErrGlobalCapExceeded) {
			t.Fatalf("expected ErrGlobalCapExceeded, got %v", err)
		}
	})
}

func TestChecker_DisabledCapsAllowEverything(t *testing.T) {
	userID := uuid.New()
	totals := &stubTotals{
		userTotals:  map[uuid.UUID]decimal.Decimal{userID: dec("1000000")},
		globalTotal: dec("1000000"),
	}
	checker := NewChecker(Config{}, totals, nil) // zero caps == disabled

	if err := checker.CheckDeposit(context.Background(), userID, dec("999999")); err != nil {
		t.Fatalf("expected disabled caps to allow everything, got %v", err)
	}
}

func TestChecker_NilCheckerIsNoop(t *testing.T) {
	var checker *Checker
	if err := checker.CheckDeposit(context.Background(), uuid.New(), dec("100")); err != nil {
		t.Fatalf("nil checker should be a no-op, got %v", err)
	}
}

func TestChecker_WarnsOnceWhenCrossingThreshold(t *testing.T) {
	userID := uuid.New()
	totals := &stubTotals{userTotals: map[uuid.UUID]decimal.Decimal{userID: dec("750")}}

	var warnings []Warning
	checker := NewChecker(Config{
		PerUserCap:        dec("1000"),
		WarnThresholdsPct: []int{80, 90},
	}, totals, func(_ context.Context, w Warning) {
		warnings = append(warnings, w)
	})

	// 750 -> 850 crosses the 80% (800) line but not 90% (900).
	if err := checker.CheckDeposit(context.Background(), userID, dec("100")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly 1 warning, got %d: %+v", len(warnings), warnings)
	}
	if warnings[0].ThresholdPct != 80 {
		t.Fatalf("expected 80%% threshold warning, got %d", warnings[0].ThresholdPct)
	}
}

func TestChecker_NoWarnWhenAlreadyPastThreshold(t *testing.T) {
	userID := uuid.New()
	// Already at 850 (past the 80% line of 800); a further deposit that
	// stays under 90% must not re-fire the 80% warning.
	totals := &stubTotals{userTotals: map[uuid.UUID]decimal.Decimal{userID: dec("850")}}

	var warnings []Warning
	checker := NewChecker(Config{
		PerUserCap:        dec("1000"),
		WarnThresholdsPct: []int{80, 90},
	}, totals, func(_ context.Context, w Warning) {
		warnings = append(warnings, w)
	})

	if err := checker.CheckDeposit(context.Background(), userID, dec("10")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %+v", warnings)
	}
}

func TestChecker_ZeroOrNegativeAmountSkipped(t *testing.T) {
	tests := []struct {
		name   string
		amount decimal.Decimal
	}{
		{name: "zero", amount: decimal.Zero},
		{name: "negative", amount: dec("-100")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uuid.New()
			totals := &stubTotals{userTotals: map[uuid.UUID]decimal.Decimal{userID: dec("1000")}}
			checker := NewChecker(Config{PerUserCap: dec("1000")}, totals, nil)

			if err := checker.CheckDeposit(context.Background(), userID, tt.amount); err != nil {
				t.Fatalf("expected %s-amount check to be a no-op, got %v", tt.name, err)
			}
		})
	}
}

// TestChecker_NoWarnWhenRejectedByLaterCap is a regression test for a
// nester CodeRabbit post-rebase finding: warnings must not be emitted until
// every enabled cap check has succeeded. A deposit that crosses the
// per-user warn threshold but is then rejected by the global cap must emit
// no warning at all — the per-user check alone used to fire its warning
// immediately, before the global check ran and rejected the deposit.
func TestChecker_NoWarnWhenRejectedByLaterCap(t *testing.T) {
	userID := uuid.New()
	totals := &stubTotals{
		userTotals:  map[uuid.UUID]decimal.Decimal{userID: dec("750")},
		globalTotal: dec("49950"),
	}

	var warnings []Warning
	checker := NewChecker(Config{
		PerUserCap:        dec("1000"),
		GlobalCap:         dec("50000"),
		WarnThresholdsPct: []int{80, 90},
	}, totals, func(_ context.Context, w Warning) {
		warnings = append(warnings, w)
	})

	// Per-user: 750 -> 850 crosses the 80% (800) warn line and stays under
	// the 1000 cap. Global: 49950 -> 50050 exceeds the 50000 cap, so the
	// whole deposit is rejected — the per-user warning must not fire.
	err := checker.CheckDeposit(context.Background(), userID, dec("100"))
	if err == nil {
		t.Fatal("expected the global cap to reject the deposit, got nil error")
	}
	var capErr *CapExceededError
	if !errors.As(err, &capErr) || capErr.Kind != KindGlobal {
		t.Fatalf("expected a global CapExceededError, got %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings when the deposit is ultimately rejected, got %+v", warnings)
	}
}

// TestParseCapValue table-drives the launch-cap env-var parser: blank/"0"
// must disable the cap (decimal.Zero, no error), a valid positive value must
// parse through, and negative or malformed input must error rather than
// silently producing the zero-value "disabled" decimal (nester CodeRabbit
// finding).
func TestParseCapValue(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    decimal.Decimal
		wantErr bool
	}{
		{name: "blank disables", input: "", want: decimal.Zero},
		{name: "zero disables", input: "0", want: decimal.Zero},
		{name: "whitespace disables", input: "   ", want: decimal.Zero},
		{name: "valid positive enables", input: "1500.25", want: decimal.RequireFromString("1500.25")},
		{name: "negative errors", input: "-1", wantErr: true},
		{name: "malformed errors", input: "abc", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCapValue(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseCapValue(%q) error = nil, want an error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseCapValue(%q) error = %v, want nil", tt.input, err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("ParseCapValue(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

// TestConcurrentDeposits_AtomicCheckAndCommitCannotBreachCaps is the
// concurrency proof for nester CodeRabbit's TOCTOU finding: CheckDeposit
// alone reads totals and returns before the caller's write commits, so two
// concurrent deposits can each pass the check and collectively exceed a
// cap. This test drives many goroutines through the *transactional* path —
// EvaluateTotals invoked while holding a lock that serializes the read and
// the "commit" (mirrors postgres.VaultRepository.RecordDepositWithCapCheck's
// advisory-lock + same-transaction pattern) — and asserts neither the
// per-user nor the global total is ever pushed over its cap.
func TestConcurrentDeposits_AtomicCheckAndCommitCannotBreachCaps(t *testing.T) {
	userID := uuid.New()
	perUserCap := decimal.NewFromInt(100)
	globalCap := decimal.NewFromInt(150)

	checker := NewChecker(Config{
		PerUserCap: perUserCap,
		GlobalCap:  globalCap,
	}, nil, nil)

	var mu sync.Mutex
	var userTotal, globalTotal decimal.Decimal
	var accepted, rejected int32

	const attempts = 50
	const depositAmount = 10 // 50 * 10 = 500 attempted, far over both caps if unserialized

	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			defer wg.Done()
			amount := decimal.NewFromInt(depositAmount)

			// Emulates a DB transaction holding an advisory lock across the
			// read-check-write: the totals read and the credit that follows
			// are atomic with respect to every other concurrent deposit.
			mu.Lock()
			defer mu.Unlock()

			if err := checker.EvaluateTotals(context.Background(), userID, amount, userTotal, globalTotal); err != nil {
				atomic.AddInt32(&rejected, 1)
				return
			}
			userTotal = userTotal.Add(amount)
			globalTotal = globalTotal.Add(amount)
			atomic.AddInt32(&accepted, 1)
		}()
	}
	wg.Wait()

	if userTotal.GreaterThan(perUserCap) {
		t.Fatalf("per-user total %s exceeded cap %s after %d accepted deposits", userTotal, perUserCap, accepted)
	}
	if globalTotal.GreaterThan(globalCap) {
		t.Fatalf("global total %s exceeded cap %s after %d accepted deposits", globalTotal, globalCap, accepted)
	}
	if accepted == 0 {
		t.Fatal("expected at least one deposit to be accepted")
	}
	if int(accepted)+int(rejected) != attempts {
		t.Fatalf("accepted(%d)+rejected(%d) != attempts(%d)", accepted, rejected, attempts)
	}
	t.Logf("accepted=%d rejected=%d userTotal=%s globalTotal=%s", accepted, rejected, userTotal, globalTotal)
}
