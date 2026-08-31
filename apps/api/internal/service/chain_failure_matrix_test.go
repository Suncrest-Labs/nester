package service

import (
	"context"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/chaintest"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
)

// Chain-boundary failure matrix for the money path (#1090).
//
// Every deposit and withdrawal reaches a user's balance through exactly one
// gate: TransactionService.ReconcileTransaction asks the chain whether the
// transaction closed successfully, and only then calls VaultBalanceApplier.
// Everything upstream of that — building, signing, submitting — produces a
// pending row and moves no money.
//
// This file drives that gate through every way the chain boundary fails and
// asserts the persisted state each time. The invariant under test is single
// and absolute:
//
//	A balance is credited if and only if the chain reported the transaction
//	as closed and successful.
//
// A failure to *reach* the chain must therefore leave the transaction pending
// and the balance untouched — never credited, and never marked failed either,
// because "we could not ask" is not "it did not happen". Marking it failed
// would strand a transaction that did in fact land.
//
// Determinism: no test sleeps. The timeout and slow-response scenarios are
// resolved by a context deadline the test owns, against a fake that never
// answers until told to, so the outcome does not depend on machine speed.

// probeDeadline bounds a scenario in which the chain never answers. It is the
// caller's deadline, not a wait for something to happen: the fake is parked
// until the request context is cancelled, so this fires every time.
const probeDeadline = 150 * time.Millisecond

// chainScenario is one way the chain boundary behaves during reconciliation.
type chainScenario struct {
	name  string
	fault chaintest.Fault

	// landed marks the transaction as closed on-chain before the poll, so a
	// scenario can model "the chain has the answer, we just cannot read it".
	landed          bool
	landedSuccessed bool

	// wantStatus is the status that must be persisted after the poll.
	wantStatus transaction.TransactionStatus
	// wantCredits is how many times the balance applier may have been called.
	wantCredits int
	// wantReconcileError records whether the poll surfaced an error at all.
	wantReconcileError bool
}

// creditingScenarios is the matrix. Ordered failure-first so the file reads as
// "here is everything that must NOT credit", then the two cases that must.
var creditingScenarios = []chainScenario{
	{
		// The node never answers. We do not know the outcome, so the
		// transaction stays pending for the next poll.
		name:               "timeout does not credit",
		fault:              chaintest.FaultTimeout,
		landed:             true,
		landedSuccessed:    true,
		wantStatus:         transaction.StatusPending,
		wantCredits:        0,
		wantReconcileError: true,
	},
	{
		// A 5xx carrying a JSON body is the trap: a client that decodes
		// without checking the status sees successful=false, created_at="" —
		// a syntactically fine "failed transaction" that never happened.
		name:               "http 5xx does not credit",
		fault:              chaintest.FaultServerError,
		landed:             true,
		landedSuccessed:    true,
		wantStatus:         transaction.StatusPending,
		wantCredits:        0,
		wantReconcileError: true,
	},
	{
		name:               "malformed response does not credit",
		fault:              chaintest.FaultMalformed,
		landed:             true,
		landedSuccessed:    true,
		wantStatus:         transaction.StatusPending,
		wantCredits:        0,
		wantReconcileError: true,
	},
	{
		// The answer arrives after we stopped listening. A late success is
		// not a success: nothing may act on a response the client abandoned.
		name:               "slow response does not credit",
		fault:              chaintest.FaultSlow,
		landed:             true,
		landedSuccessed:    true,
		wantStatus:         transaction.StatusPending,
		wantCredits:        0,
		wantReconcileError: true,
	},
	{
		name:               "lost response does not credit",
		fault:              chaintest.FaultLostResponse,
		landed:             true,
		landedSuccessed:    true,
		wantStatus:         transaction.StatusPending,
		wantCredits:        0,
		wantReconcileError: true,
	},
	{
		// The chain answered, and its answer is "not ingested". Pending is
		// correct and is not an error — this is the ordinary in-flight case.
		name:               "chain has not seen the transaction does not credit",
		fault:              chaintest.FaultNone,
		landed:             false,
		wantStatus:         transaction.StatusPending,
		wantCredits:        0,
		wantReconcileError: false,
	},
	{
		// Submitted successfully, reverted in the ledger. Terminal, and the
		// balance must never move.
		name:            "confirmed failure marks failed without crediting",
		fault:           chaintest.FaultNone,
		landed:          true,
		landedSuccessed: false,
		wantStatus:      transaction.StatusFailed,
		wantCredits:     0,
	},
	{
		// The only path that may credit.
		name:            "confirmed success credits exactly once",
		fault:           chaintest.FaultNone,
		landed:          true,
		landedSuccessed: true,
		wantStatus:      transaction.StatusCompleted,
		wantCredits:     1,
	},
}

func TestChainFailureMatrix_BalanceIsCreditedOnlyOnConfirmation(t *testing.T) {
	for _, txType := range []transaction.TransactionType{transaction.TypeDeposit, transaction.TypeWithdrawal} {
		for _, sc := range creditingScenarios {
			t.Run(string(txType)+"/"+sc.fault.String()+"/"+sc.name, func(t *testing.T) {
				chain := chaintest.New()
				t.Cleanup(chain.Close)

				const hash = "matrix-tx"
				if sc.landed {
					chain.Land(hash, sc.landedSuccessed)
				}
				chain.SetHorizonFault(sc.fault)

				tx := newPendingTx(txType, hash, time.Minute)
				repo := newFakeTransactionRepo(tx)
				svc := NewTransactionService(repo, chain.HorizonURL)
				applier := &fakeBalanceApplier{}
				svc.SetBalanceApplier(applier)

				ctx, cancel := context.WithTimeout(context.Background(), probeDeadline)
				defer cancel()

				_, changed, err := svc.ReconcileTransaction(ctx, tx)
				if sc.wantReconcileError && err == nil {
					t.Errorf("expected reconciliation to surface an error for %s, got nil", sc.fault)
				}
				if !sc.wantReconcileError && err != nil {
					t.Errorf("unexpected reconciliation error: %v", err)
				}

				stored, getErr := repo.GetByHash(context.Background(), hash)
				if getErr != nil {
					t.Fatalf("GetByHash: %v", getErr)
				}

				if stored.Status != sc.wantStatus {
					t.Errorf("persisted status = %q, want %q", stored.Status, sc.wantStatus)
				}
				if changed != (sc.wantStatus != transaction.StatusPending) {
					t.Errorf("changed = %t, want %t", changed, sc.wantStatus != transaction.StatusPending)
				}

				credits := applier.depositCount() + applier.withdrawalCount()
				if credits != sc.wantCredits {
					t.Errorf("balance applied %d time(s), want %d", credits, sc.wantCredits)
				}

				// The invariant, restated as an assertion that does not depend
				// on the table being filled in correctly: money moved only if
				// the chain said the transaction closed successfully.
				chainConfirmedSuccess := sc.landed && sc.landedSuccessed && sc.fault == chaintest.FaultNone
				if credits > 0 && !chainConfirmedSuccess {
					t.Fatalf("balance was credited without a confirmed successful transaction (fault=%s)", sc.fault)
				}

				// A transaction the chain never resolved must stay retryable.
				if sc.wantStatus == transaction.StatusPending && stored.ConfirmedAt != nil {
					t.Error("a pending transaction must not carry a confirmation time")
				}
			})
		}
	}
}

// The poller is the unattended path: nobody is watching, so a chain outage that
// silently marked transactions terminal would be discovered only as a wrong
// balance. Same matrix, driven through Tick.
func TestChainFailureMatrix_PollerNeverCreditsOnChainFailure(t *testing.T) {
	for _, sc := range creditingScenarios {
		if sc.fault == chaintest.FaultNone {
			continue // covered by the direct-reconcile matrix above
		}
		t.Run(sc.fault.String(), func(t *testing.T) {
			chain := chaintest.New()
			t.Cleanup(chain.Close)

			const hash = "poller-tx"
			chain.Land(hash, true) // the chain holds a successful transaction
			chain.SetHorizonFault(sc.fault)

			tx := newPendingTx(transaction.TypeDeposit, hash, time.Minute)
			repo := newFakeTransactionRepo(tx)
			svc := NewTransactionService(repo, chain.HorizonURL)
			applier := &fakeBalanceApplier{}
			svc.SetBalanceApplier(applier)

			poller := NewTransactionPoller(
				TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
				svc, nil, nil,
			)

			ctx, cancel := context.WithTimeout(context.Background(), probeDeadline)
			defer cancel()
			poller.Tick(ctx)

			stored, _ := repo.GetByHash(context.Background(), hash)
			if stored.Status != transaction.StatusPending {
				t.Errorf("status = %q, want pending: an unreadable chain is not an answer", stored.Status)
			}
			if applier.depositCount() != 0 {
				t.Fatalf("balance credited %d time(s) while the chain was unreachable", applier.depositCount())
			}
		})
	}
}

// The reconciliation case the issue names explicitly: the submission response
// was lost, so the caller never learned the hash landed — but it did. The next
// poll, once the boundary recovers, must converge on exactly one credit.
func TestChainFailureMatrix_LostResponseThenChainSuccessReconcilesExactlyOnce(t *testing.T) {
	chain := chaintest.New()
	t.Cleanup(chain.Close)

	const hash = "lost-then-found"
	tx := newPendingTx(transaction.TypeDeposit, hash, time.Minute)
	repo := newFakeTransactionRepo(tx)
	svc := NewTransactionService(repo, chain.HorizonURL)
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)

	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)

	// Pass 1: the transaction has landed on-chain, but the lookup response is
	// lost. Nothing may be concluded.
	chain.Land(hash, true)
	chain.SetHorizonFault(chaintest.FaultLostResponse)

	lostCtx, cancelLost := context.WithTimeout(context.Background(), probeDeadline)
	poller.Tick(lostCtx)
	cancelLost()

	stored, _ := repo.GetByHash(context.Background(), hash)
	if stored.Status != transaction.StatusPending {
		t.Fatalf("after a lost response, status = %q, want pending", stored.Status)
	}
	if applier.depositCount() != 0 {
		t.Fatalf("a lost response must not credit; credited %d time(s)", applier.depositCount())
	}

	// Pass 2: the boundary recovers. The chain still holds the transaction, so
	// reconciliation finds it and credits — once.
	chain.SetHorizonFault(chaintest.FaultNone)
	poller.Tick(context.Background())

	stored, _ = repo.GetByHash(context.Background(), hash)
	if stored.Status != transaction.StatusCompleted {
		t.Fatalf("after recovery, status = %q, want completed", stored.Status)
	}
	if applier.depositCount() != 1 {
		t.Fatalf("expected exactly 1 credit after recovery, got %d", applier.depositCount())
	}

	// Pass 3: a terminal transaction is never re-reconciled, so repeated polls
	// cannot double-credit.
	poller.Tick(context.Background())
	if applier.depositCount() != 1 {
		t.Fatalf("re-polling a completed transaction credited again: %d total", applier.depositCount())
	}
}

// A transaction the chain reports as reverted must never be retried into a
// credit by a later chain failure: terminal is terminal.
func TestChainFailureMatrix_FailedTransactionStaysFailedThroughOutage(t *testing.T) {
	chain := chaintest.New()
	t.Cleanup(chain.Close)

	const hash = "reverted-tx"
	chain.Land(hash, false)

	tx := newPendingTx(transaction.TypeDeposit, hash, time.Minute)
	repo := newFakeTransactionRepo(tx)
	svc := NewTransactionService(repo, chain.HorizonURL)
	applier := &fakeBalanceApplier{}
	svc.SetBalanceApplier(applier)

	poller := NewTransactionPoller(
		TransactionPollerConfig{Enabled: true, Interval: time.Hour, MinAge: 30 * time.Second},
		svc, nil, nil,
	)
	poller.Tick(context.Background())

	stored, _ := repo.GetByHash(context.Background(), hash)
	if stored.Status != transaction.StatusFailed {
		t.Fatalf("status = %q, want failed", stored.Status)
	}

	// Now break the boundary and poll again. The row is terminal, so no lookup
	// should even be attempted, and nothing may change.
	before := chain.Calls("horizon:transactions")
	chain.SetHorizonFault(chaintest.FaultServerError)
	poller.Tick(context.Background())

	stored, _ = repo.GetByHash(context.Background(), hash)
	if stored.Status != transaction.StatusFailed {
		t.Errorf("status = %q after an outage, want failed", stored.Status)
	}
	if applier.depositCount() != 0 {
		t.Fatalf("a reverted transaction was credited")
	}
	if got := chain.Calls("horizon:transactions"); got != before {
		t.Errorf("terminal transaction was re-queried %d time(s)", got-before)
	}
}
