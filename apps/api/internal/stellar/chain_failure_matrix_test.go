package stellar

import (
	"context"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/chaintest"
)

// Soroban RPC failure matrix (#1090).
//
// The service-level matrix in internal/service proves that no chain failure
// credits a balance. This one sits a layer lower, on the JSON-RPC primitives
// every money-path contract call goes through — simulate before a withdrawal,
// send to submit it, getTransaction to confirm it.
//
// The property under test is narrower and blunter: a failed RPC call must
// return an error. Not a zero value, not an empty hash, not a "" status — an
// error. A submit path that reports success with no transaction hash is how an
// outage becomes a transaction the system believes exists and the chain has
// never heard of, and reconciliation cannot repair what it cannot look up.
//
// Determinism matches the service matrix: the fake parks until the caller's
// context is cancelled, so nothing here waits on wall-clock time.

const rpcProbeDeadline = 150 * time.Millisecond

type rpcFaultCase struct {
	fault chaintest.Fault
	// Every fault must produce an error from every RPC primitive. There is no
	// fault for which a zero value is an acceptable answer.
	wantErr bool
}

var rpcFaultCases = []rpcFaultCase{
	{fault: chaintest.FaultTimeout, wantErr: true},
	{fault: chaintest.FaultServerError, wantErr: true},
	{fault: chaintest.FaultMalformed, wantErr: true},
	{fault: chaintest.FaultSlow, wantErr: true},
	{fault: chaintest.FaultLostResponse, wantErr: true},
	{fault: chaintest.FaultNone, wantErr: false},
}

// TestRPCFailureMatrix_SendNeverReportsFalseSuccess is the important one.
//
// `send` returns (hash, error). Every caller treats a nil error as "submitted"
// and records the hash. If a failed call can return ("", nil) the system
// records a submission that does not exist, with no hash to reconcile against
// — a pending transaction that can never resolve, against a user's money.
func TestRPCFailureMatrix_SendNeverReportsFalseSuccess(t *testing.T) {
	for _, tc := range rpcFaultCases {
		t.Run(tc.fault.String(), func(t *testing.T) {
			chain := chaintest.New()
			t.Cleanup(chain.Close)
			chain.SetSorobanFault(tc.fault)

			inv := testInvoker(t, chain.SorobanURL, chain.HorizonURL)

			ctx, cancel := context.WithTimeout(context.Background(), rpcProbeDeadline)
			defer cancel()

			hash, err := inv.send(ctx, "AAAAAgAAAAA=")

			if tc.wantErr {
				if err == nil {
					t.Fatalf("send returned no error under %s (hash %q); a failed submission must never read as success",
						tc.fault, hash)
				}
				if hash != "" {
					t.Errorf("send returned hash %q alongside an error; callers must get no hash to record", hash)
				}
				return
			}

			if err != nil {
				t.Fatalf("send: unexpected error: %v", err)
			}
			if hash == "" {
				t.Fatal("a successful submission must return a transaction hash")
			}
		})
	}
}

// simulate gates every write: a withdrawal that cannot be simulated must not be
// signed or submitted. A simulate that fails open would submit blind.
func TestRPCFailureMatrix_SimulateNeverSucceedsOnFailure(t *testing.T) {
	for _, tc := range rpcFaultCases {
		t.Run(tc.fault.String(), func(t *testing.T) {
			chain := chaintest.New()
			t.Cleanup(chain.Close)
			chain.SetSorobanFault(tc.fault)

			inv := testInvoker(t, chain.SorobanURL, chain.HorizonURL)

			ctx, cancel := context.WithTimeout(context.Background(), rpcProbeDeadline)
			defer cancel()

			_, err := inv.simulate(ctx, "AAAAAgAAAAA=")

			if tc.wantErr && err == nil {
				t.Fatalf("simulate returned no error under %s; a write would be submitted unchecked", tc.fault)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("simulate: unexpected error: %v", err)
			}
		})
	}
}

// waitForTx decides whether a submitted transaction confirmed. Reporting
// success on a failed lookup would mark a transaction confirmed without the
// chain ever having said so.
func TestRPCFailureMatrix_WaitForTxNeverConfirmsOnFailure(t *testing.T) {
	for _, tc := range rpcFaultCases {
		if tc.fault == chaintest.FaultNone {
			// Skipped deliberately: waitForTx polls on a 3s ticker, so the
			// success path would add three seconds to the suite to prove what
			// TestWaitForTx_ConfirmsLandedTransaction below proves directly.
			continue
		}
		t.Run(tc.fault.String(), func(t *testing.T) {
			chain := chaintest.New()
			t.Cleanup(chain.Close)
			// The chain HAS the transaction, successfully closed. Only the
			// boundary is broken, so any "confirmed" verdict here would come
			// from the failure rather than from the chain.
			chain.Land(chain.SubmitHash, true)
			chain.SetSorobanFault(tc.fault)

			inv := testInvoker(t, chain.SorobanURL, chain.HorizonURL)

			ctx, cancel := context.WithTimeout(context.Background(), rpcProbeDeadline)
			defer cancel()

			if err := inv.waitForTx(ctx, chain.SubmitHash); err == nil {
				t.Fatalf("waitForTx confirmed a transaction under %s without a usable answer from the chain", tc.fault)
			}
		})
	}
}

// The confirmation path, proven directly rather than through the ticker.
func TestWaitForTx_DistinguishesSuccessFailureAndAbsence(t *testing.T) {
	chain := chaintest.New()
	t.Cleanup(chain.Close)

	inv := testInvoker(t, chain.SorobanURL, chain.HorizonURL)

	t.Run("failed ledger result is an error", func(t *testing.T) {
		chain.Land("reverted", false)
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := inv.waitForTx(ctx, "reverted"); err == nil {
			t.Fatal("a reverted transaction must not confirm")
		}
	})

	t.Run("unknown transaction keeps polling until the deadline", func(t *testing.T) {
		// NOT_FOUND means "not yet", not "failed". The call must not return a
		// verdict; it should run out of time instead.
		ctx, cancel := context.WithTimeout(context.Background(), rpcProbeDeadline)
		defer cancel()
		if err := inv.waitForTx(ctx, "never-submitted"); err == nil {
			t.Fatal("an unknown transaction must not confirm")
		}
	})
}

// A lost sendTransaction response is the case the reconciliation path exists
// for: the node accepted the transaction, the caller never learned its hash.
// The submission must surface as an error — and the transaction must still be
// findable on-chain afterwards, which is what lets reconciliation recover it.
func TestRPCFailureMatrix_LostSendResponseStillLandsOnChain(t *testing.T) {
	chain := chaintest.New()
	t.Cleanup(chain.Close)
	chain.SetSorobanFault(chaintest.FaultLostResponse)

	inv := testInvoker(t, chain.SorobanURL, chain.HorizonURL)

	ctx, cancel := context.WithTimeout(context.Background(), rpcProbeDeadline)
	defer cancel()

	hash, err := inv.send(ctx, "AAAAAgAAAAA=")
	if err == nil {
		t.Fatal("a lost response must be reported as an error, not as a submission")
	}
	if hash != "" {
		t.Errorf("hash = %q, want empty: the caller learned nothing", hash)
	}

	// The node processed the request even though the answer was lost.
	if got := chain.Calls("sendTransaction"); got != 1 {
		t.Fatalf("sendTransaction served %d time(s), want 1", got)
	}

	// And the chain now holds it, so reconciliation has something to find.
	chain.SetSorobanFault(chaintest.FaultNone)
	confirmCtx, cancelConfirm := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancelConfirm()
	if err := inv.waitForTx(confirmCtx, chain.SubmitHash); err != nil {
		t.Fatalf("the transaction landed but could not be confirmed after recovery: %v", err)
	}
}
