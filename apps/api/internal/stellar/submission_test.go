package stellar

import (
	"testing"
	"time"
)

// The scenarios in this file are the three the issue requires, expressed
// against the rules that actually decide whether money can move twice. They
// need no database, no RPC, and no clock, so they are deterministic and they
// exercise the real decision function rather than a re-implementation of it.

var (
	// A submission created at 12:00, valid for five minutes, submitted
	// immediately. Every scenario below varies only what the chain says.
	submittedAt   = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	validUntil    = submittedAt.Add(5 * time.Minute)
	insideWindow  = submittedAt.Add(2 * time.Minute)
	pastTheWindow = validUntil.Add(1 * time.Minute)
)

func testIntent() SubmissionIntent {
	at := submittedAt
	return SubmissionIntent{
		ID:                   "11111111-1111-1111-1111-111111111111",
		IdempotencyReference: "vault-deposit-abc",
		TransactionHash:      "a1b2c3",
		ValidUntil:           validUntil,
		SourceAccount:        "GABC",
		DomainAction:         "deposit",
		State:                SubmissionPending,
		CreatedAt:            submittedAt,
		SubmittedAt:          &at,
	}
}

// chainAt builds a view of a chain whose clock reads now and whose history
// reaches back far enough to be conclusive.
func chainAt(now time.Time) ChainView {
	return ChainView{
		LatestLedgerCloseTime: now,
		OldestLedgerCloseTime: submittedAt.Add(-1 * time.Hour),
	}
}

func assertOutcome(t *testing.T, got, want ChainOutcome) {
	t.Helper()
	if got != want {
		t.Fatalf("outcome = %s, want %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// Scenario 1 — response lost after the transaction succeeded
// ---------------------------------------------------------------------------

// The dangerous one. The chain accepted and executed the transaction, but the
// response never reached us. A system that treats the timeout as failure and
// resubmits moves the user's money twice.
func TestScenarioResponseLostAfterSuccess(t *testing.T) {
	intent := testIntent()

	// Immediately after the lost response the chain has not been asked yet,
	// so there is no outcome and the record must stay pending.
	assertOutcome(t, DetermineOutcome(TxStatusNotFound, chainAt(insideWindow), intent), OutcomeUnknown)
	if got := OutcomeUnknown.State(); got != SubmissionPending {
		t.Fatalf("state after a lost response = %s, want pending", got)
	}
	if OutcomeUnknown.PermitsNewAttempt() {
		t.Fatal("a lost response permitted a retry; this is the double-submit")
	}

	// The reconciler asks the chain, which reports the transaction landed.
	outcome := DetermineOutcome(TxStatusSuccess, chainAt(insideWindow), intent)
	assertOutcome(t, outcome, OutcomeLanded)

	if got := outcome.State(); got != SubmissionLanded {
		t.Fatalf("state = %s, want landed", got)
	}
	// The invariant: a landed transaction is never sent again, whatever else
	// happens.
	if outcome.PermitsNewAttempt() {
		t.Fatal("a landed transaction permitted a retry")
	}
}

// A success discovered long after the fact is still a success. Expiry
// reasoning must never override an actual answer from the chain.
func TestLandedTransactionIsNeverReopenedByExpiry(t *testing.T) {
	intent := testIntent()

	outcome := DetermineOutcome(TxStatusSuccess, chainAt(pastTheWindow), intent)
	assertOutcome(t, outcome, OutcomeLanded)
	if outcome.PermitsNewAttempt() {
		t.Fatal("an expired-but-landed transaction permitted a retry")
	}
}

// ---------------------------------------------------------------------------
// Scenario 2 — response lost after the transaction failed
// ---------------------------------------------------------------------------

// The chain included the transaction and it failed. The operation did not
// take effect, so a fresh attempt is legitimate — but only once the chain has
// actually said so. The ordering is what is under test.
func TestScenarioResponseLostAfterFailure(t *testing.T) {
	intent := testIntent()

	// Before the chain answers: no retry.
	before := DetermineOutcome(TxStatusNotFound, chainAt(insideWindow), intent)
	assertOutcome(t, before, OutcomeUnknown)
	if before.PermitsNewAttempt() {
		t.Fatal("a retry was permitted before the chain was consulted")
	}

	// The chain reports the transaction landed and failed.
	outcome := DetermineOutcome(TxStatusFailed, chainAt(insideWindow), intent)
	assertOutcome(t, outcome, OutcomeRejected)

	if got := outcome.State(); got != SubmissionRejected {
		t.Fatalf("state = %s, want rejected", got)
	}

	// Only now is a fresh attempt safe: the envelope's sequence number is
	// spent, so the original can never land after the fact.
	if !outcome.PermitsNewAttempt() {
		t.Fatal("a chain-confirmed failure did not permit a fresh attempt")
	}
}

// ---------------------------------------------------------------------------
// Scenario 3 — RPC unavailable throughout
// ---------------------------------------------------------------------------

// Submission could not be confirmed and reconciliation cannot confirm either.
// Unknown must remain unknown: the record stays pending, and nothing is
// marked landed or rejected merely because the upstream is down.
func TestScenarioRPCUnavailableThroughout(t *testing.T) {
	intent := testIntent()

	// An unreachable RPC yields no status and no view of the chain at all.
	noView := ChainView{}

	for _, status := range []TransactionStatus{TxStatusNotFound, ""} {
		outcome := DetermineOutcome(status, noView, intent)
		assertOutcome(t, outcome, OutcomeUnknown)

		if got := outcome.State(); got != SubmissionPending {
			t.Fatalf("status %q with no chain view produced state %s, want pending", status, got)
		}
		if outcome.PermitsNewAttempt() {
			t.Fatalf("status %q with no chain view permitted a retry", status)
		}
	}

	// Repeated failed reconciliation attempts must not accumulate into
	// permission. There is no number of unknowns that becomes a proof.
	for i := 0; i < 100; i++ {
		if DetermineOutcome(TxStatusNotFound, noView, intent).PermitsNewAttempt() {
			t.Fatalf("attempt %d turned repeated unknowns into permission to retry", i)
		}
	}
}

// A partial view — the RPC answered but omitted the ledger times — is not
// enough to reason from either.
func TestPartialChainViewIsNotProof(t *testing.T) {
	intent := testIntent()

	views := map[string]ChainView{
		"no times at all": {},
		"only latest":     {LatestLedgerCloseTime: pastTheWindow},
		"only oldest":     {OldestLedgerCloseTime: submittedAt.Add(-time.Hour)},
	}

	for name, view := range views {
		t.Run(name, func(t *testing.T) {
			outcome := DetermineOutcome(TxStatusNotFound, view, intent)
			assertOutcome(t, outcome, OutcomeUnknown)
			if outcome.PermitsNewAttempt() {
				t.Fatal("an incomplete chain view was treated as proof")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NOT_FOUND — the branch where a naive implementation goes wrong
// ---------------------------------------------------------------------------

// Inside its validity window a missing transaction may still be included.
// This is the ordinary state immediately after a timeout, and treating it as
// proof of non-landing is precisely the double-submit bug.
func TestNotFoundInsideTheValidityWindowIsNotProof(t *testing.T) {
	intent := testIntent()

	for _, now := range []time.Time{
		submittedAt,
		insideWindow,
		validUntil, // exactly at maxTime: still includable
	} {
		outcome := DetermineOutcome(TxStatusNotFound, chainAt(now), intent)
		if outcome != OutcomeUnknown {
			t.Fatalf("at %s outcome = %s, want unknown", now, outcome)
		}
		if outcome.PermitsNewAttempt() {
			t.Fatalf("at %s a still-includable transaction permitted a retry", now)
		}
	}
}

// Once the chain's own clock has passed the transaction's signed maxTime, the
// network is guaranteed to refuse it. That — not elapsed time on our side —
// is what makes NOT_FOUND conclusive.
func TestNotFoundPastTheValidityWindowIsProof(t *testing.T) {
	intent := testIntent()

	outcome := DetermineOutcome(TxStatusNotFound, chainAt(pastTheWindow), intent)
	assertOutcome(t, outcome, OutcomeProvenNotLanded)

	if got := outcome.State(); got != SubmissionExpired {
		t.Fatalf("state = %s, want expired", got)
	}
	if !outcome.PermitsNewAttempt() {
		t.Fatal("a provably un-landable transaction did not permit a fresh attempt")
	}
}

// Expiry is judged by the chain's clock, not ours. A local clock running fast
// must not be able to manufacture proof — that would be a timing assumption
// wearing the costume of chain evidence.
func TestExpiryUsesTheChainsClockNotOurs(t *testing.T) {
	intent := testIntent()

	// The chain is still inside the window, however late it is locally.
	view := ChainView{
		LatestLedgerCloseTime: insideWindow,
		OldestLedgerCloseTime: submittedAt.Add(-time.Hour),
	}

	outcome := DetermineOutcome(TxStatusNotFound, view, intent)
	assertOutcome(t, outcome, OutcomeUnknown)
	if outcome.PermitsNewAttempt() {
		t.Fatal("the chain was still inside the window but a retry was permitted")
	}
}

// The long-outage case, and the subtlest one. If the RPC's history no longer
// reaches back to when the transaction could have landed, a landed
// transaction and a never-submitted one both read NOT_FOUND. That is not
// proof, it is amnesia, and it must be terminal-but-not-retryable rather than
// permission to submit again.
func TestNotFoundBeyondTheRPCsMemoryIsNotProof(t *testing.T) {
	intent := testIntent()

	view := ChainView{
		LatestLedgerCloseTime: pastTheWindow,
		// History begins after the transaction was submitted, so the RPC
		// could no longer see it even if it had landed.
		OldestLedgerCloseTime: submittedAt.Add(1 * time.Minute),
	}

	outcome := DetermineOutcome(TxStatusNotFound, view, intent)
	assertOutcome(t, outcome, OutcomeUndeterminable)

	if got := outcome.State(); got != SubmissionUnresolvable {
		t.Fatalf("state = %s, want unresolvable", got)
	}
	// The point of the whole test: forgetting is not proving.
	if outcome.PermitsNewAttempt() {
		t.Fatal("an unresolvable submission permitted a retry; this can double-submit")
	}
}

// The boundary of the memory check: history reaching back to exactly the
// moment of submission is still conclusive.
func TestRPCMemoryBoundaryIsInclusive(t *testing.T) {
	intent := testIntent()

	view := ChainView{
		LatestLedgerCloseTime: pastTheWindow,
		OldestLedgerCloseTime: submittedAt,
	}

	assertOutcome(t, DetermineOutcome(TxStatusNotFound, view, intent), OutcomeProvenNotLanded)
}

// A transaction with no upper time bound can never be proven dead, because
// there is no moment after which the network must refuse it. It must never
// be retried, however long it has been missing.
func TestTransactionWithoutTimeBoundsIsNeverProvenDead(t *testing.T) {
	intent := testIntent()
	intent.ValidUntil = time.Time{}

	farFuture := chainAt(submittedAt.Add(30 * 24 * time.Hour))

	outcome := DetermineOutcome(TxStatusNotFound, farFuture, intent)
	assertOutcome(t, outcome, OutcomeUnknown)
	if outcome.PermitsNewAttempt() {
		t.Fatal("a transaction with no maxTime was treated as expired")
	}
}

// ---------------------------------------------------------------------------
// The invariant, stated directly
// ---------------------------------------------------------------------------

// Exhaustive over the outcome set: exactly two values permit another attempt,
// and both mean the chain has said no. If someone adds an outcome and wires
// it into PermitsNewAttempt without thinking, this fails.
func TestOnlyChainProofPermitsANewAttempt(t *testing.T) {
	permitted := map[ChainOutcome]bool{
		OutcomeUnknown:         false,
		OutcomeLanded:          false,
		OutcomeUndeterminable:  false,
		OutcomeRejected:        true,
		OutcomeProvenNotLanded: true,
	}

	for outcome, want := range permitted {
		if got := outcome.PermitsNewAttempt(); got != want {
			t.Errorf("%s.PermitsNewAttempt() = %v, want %v", outcome, got, want)
		}
	}

	// And nothing outside the known set may permit one either.
	for value := -5; value < 50; value++ {
		outcome := ChainOutcome(value)
		if _, known := permitted[outcome]; known {
			continue
		}
		if outcome.PermitsNewAttempt() {
			t.Errorf("unrecognised outcome %d permitted a retry", value)
		}
	}
}

// Only an actual answer from the chain moves a record out of pending.
func TestOnlyChainAnswersLeavePending(t *testing.T) {
	staysPending := []ChainOutcome{OutcomeUnknown, ChainOutcome(99)}
	for _, outcome := range staysPending {
		if got := outcome.State(); got != SubmissionPending {
			t.Errorf("%s.State() = %s, want pending", outcome, got)
		}
	}

	resolves := map[ChainOutcome]SubmissionState{
		OutcomeLanded:          SubmissionLanded,
		OutcomeRejected:        SubmissionRejected,
		OutcomeProvenNotLanded: SubmissionExpired,
		OutcomeUndeterminable:  SubmissionUnresolvable,
	}
	for outcome, want := range resolves {
		if got := outcome.State(); got != want {
			t.Errorf("%s.State() = %s, want %s", outcome, got, want)
		}
		if !want.Terminal() {
			t.Errorf("%s is a resolved state but reports non-terminal", want)
		}
	}

	if SubmissionPending.Terminal() {
		t.Error("pending must not be terminal; the reconciler would stop looking at it")
	}
}

// ---------------------------------------------------------------------------
// Transaction identity
// ---------------------------------------------------------------------------

// The hash must be the chain's, derived from the signed envelope, so the
// reconciler can ask about the exact transaction rather than matching on
// account, amount, or timing.
func TestIdentifyTransactionRejectsGarbage(t *testing.T) {
	for _, envelope := range []string{"", "not-base64-xdr", "AAAA"} {
		if _, err := IdentifyTransaction(envelope, "Test SDF Network ; September 2015"); err == nil {
			t.Errorf("IdentifyTransaction(%q) = nil error, want a parse failure", envelope)
		}
	}
}

// EarliestInclusion is what the RPC-memory check is measured against. It must
// prefer the submission time, and fall back to creation for an intent that
// was never sent.
func TestEarliestInclusion(t *testing.T) {
	intent := testIntent()
	if got := intent.EarliestInclusion(); !got.Equal(submittedAt) {
		t.Fatalf("EarliestInclusion() = %s, want the submission time %s", got, submittedAt)
	}

	never := testIntent()
	never.SubmittedAt = nil
	never.CreatedAt = submittedAt.Add(-time.Minute)
	if got := never.EarliestInclusion(); !got.Equal(never.CreatedAt) {
		t.Fatalf("EarliestInclusion() = %s, want the creation time %s", got, never.CreatedAt)
	}
}
