package stellar

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/stellar/go/txnbuild"
)

// Durable submission records (nester#1085).
//
// The problem: if Soroban RPC times out after accepting a transaction, we do
// not know whether it landed. Resubmitting risks doing the operation twice —
// moving a user's money twice. Refusing to resubmit leaves it unresolved.
//
// The resolution is that a timeout is not an outcome. It is the absence of
// one. The durable record carries the submission across the timeout, and the
// chain — not a timer, not an error value, not the circuit breaker — is what
// eventually supplies the outcome.
//
// The invariant this file exists to make unmissable:
//
//	A submission is retried ONLY when the chain proves the previous
//	transaction can never take effect.
//
// It is expressed as a type (ChainOutcome) rather than a boolean, so that
// "we don't know" is a value a future engineer has to handle explicitly
// instead of a false that reads like "no problem".

// SubmissionState is the durable lifecycle of one logical chain submission.
//
// PENDING IS NOT FAILURE. It means the outcome is not yet known, and it is
// the state a submission sits in across an RPC timeout, an RPC outage, and a
// process restart.
type SubmissionState string

const (
	// SubmissionPending — the intent is durable and the outcome is unknown.
	// Every submission starts here, before anything is sent.
	SubmissionPending SubmissionState = "pending"

	// SubmissionLanded — the chain included the transaction and it succeeded.
	SubmissionLanded SubmissionState = "landed"

	// SubmissionRejected — the chain included the transaction and it failed.
	// The operation did not take effect, but the sequence number was
	// consumed, so this exact envelope can never land again.
	SubmissionRejected SubmissionState = "rejected"

	// SubmissionExpired — the chain proves the transaction was never
	// included and never can be: its own signed time bounds have passed
	// according to the chain's clock.
	SubmissionExpired SubmissionState = "expired"

	// SubmissionUnresolvable — the outcome cannot be determined any more,
	// because the RPC's history no longer covers the window in which the
	// transaction could have been included. This is a terminal state
	// requiring a human, and it deliberately does NOT permit a retry:
	// "we can no longer tell" is not "it did not land".
	SubmissionUnresolvable SubmissionState = "unresolvable"
)

// Terminal reports whether the state is final — the reconciler has no further
// work to do on it.
func (s SubmissionState) Terminal() bool {
	switch s {
	case SubmissionLanded, SubmissionRejected, SubmissionExpired, SubmissionUnresolvable:
		return true
	default:
		return false
	}
}

// ChainOutcome is what the chain has told us about one specific transaction.
//
// A deliberately closed set, and deliberately not a bool: the whole failure
// mode this issue exists to prevent is code that treats "no answer" as "no,
// it didn't land". There is no value here that means that.
type ChainOutcome int

const (
	// OutcomeUnknown — the chain has given us no usable answer. A timeout, an
	// RPC outage, an open circuit breaker, and a transaction that is simply
	// still in flight all produce this. It NEVER permits a retry.
	OutcomeUnknown ChainOutcome = iota

	// OutcomeLanded — included in a ledger and successful.
	OutcomeLanded

	// OutcomeRejected — included in a ledger and failed. The operation did
	// not take effect, and the envelope's sequence number is spent, so this
	// transaction can never land.
	OutcomeRejected

	// OutcomeProvenNotLanded — never included, and never can be: the chain's
	// own clock has passed the time bounds the transaction was signed with,
	// so every validator will now refuse it.
	OutcomeProvenNotLanded

	// OutcomeUndeterminable — the RPC can no longer see far enough back to
	// answer. Distinct from OutcomeUnknown because it is permanent: waiting
	// longer will not help, and it must never be mistaken for proof.
	OutcomeUndeterminable
)

func (o ChainOutcome) String() string {
	switch o {
	case OutcomeLanded:
		return "landed"
	case OutcomeRejected:
		return "rejected"
	case OutcomeProvenNotLanded:
		return "proven_not_landed"
	case OutcomeUndeterminable:
		return "undeterminable"
	default:
		return "unknown"
	}
}

// PermitsNewAttempt reports whether the chain has proven that the submitted
// transaction can never take effect, and that a fresh attempt is therefore
// safe.
//
// This is the single gate on retrying a chain write. Read the cases:
//
//   - Rejected: it landed and failed. The operation did not happen, and the
//     sequence number is spent, so the old envelope is permanently dead.
//   - ProvenNotLanded: it never landed and its signed validity window has
//     closed, so it is permanently dead.
//
// Everything else — including both flavours of "we don't know" — returns
// false. A caller cannot reach a retry without holding one of two outcomes
// that mean "the chain has spoken and the answer is no".
func (o ChainOutcome) PermitsNewAttempt() bool {
	return o == OutcomeRejected || o == OutcomeProvenNotLanded
}

// State maps a chain outcome onto the durable state to persist.
func (o ChainOutcome) State() SubmissionState {
	switch o {
	case OutcomeLanded:
		return SubmissionLanded
	case OutcomeRejected:
		return SubmissionRejected
	case OutcomeProvenNotLanded:
		return SubmissionExpired
	case OutcomeUndeterminable:
		return SubmissionUnresolvable
	default:
		// Unknown leaves the record exactly where it was: pending.
		return SubmissionPending
	}
}

// TransactionStatus is the Soroban RPC getTransaction status field.
type TransactionStatus string

const (
	TxStatusSuccess  TransactionStatus = "SUCCESS"
	TxStatusFailed   TransactionStatus = "FAILED"
	TxStatusNotFound TransactionStatus = "NOT_FOUND"
)

// ChainView is what a getTransaction response tells us about the chain
// itself, independent of the transaction asked about.
//
// The ledger close times matter as much as the status. They are the chain's
// clock and the chain's memory, and both are needed to interpret NOT_FOUND —
// which on its own means nothing at all.
type ChainView struct {
	// LatestLedgerCloseTime is the chain's current clock, as the chain
	// reports it. Deliberately not our own wall clock: whether a transaction
	// has expired is decided by consensus time, and a skewed local clock must
	// not be able to manufacture proof.
	LatestLedgerCloseTime time.Time

	// OldestLedgerCloseTime is how far back the RPC's transaction history
	// reaches. Older than this, NOT_FOUND means "forgotten", not "absent".
	OldestLedgerCloseTime time.Time
}

// Valid reports whether the view carries usable ledger times. An RPC that
// omitted them cannot support any NOT_FOUND reasoning.
func (v ChainView) Valid() bool {
	return !v.LatestLedgerCloseTime.IsZero() && !v.OldestLedgerCloseTime.IsZero()
}

// SubmissionIntent is the durable record written BEFORE anything is sent.
//
// The ordering is the whole point: persist, then submit. A crash between the
// two directions loses at most an unsent intent, which is harmless. The
// reverse ordering can leave a transaction on-chain that nothing in the
// system knows about, which is unrecoverable.
type SubmissionIntent struct {
	ID string

	// IdempotencyReference is the caller-supplied identity of the logical
	// operation. Unique in the database, which is what makes concurrent
	// duplicate requests collapse to one submission rather than racing.
	IdempotencyReference string

	// TransactionHash is the real Stellar transaction hash, computed from
	// the signed envelope BEFORE submitting. This is the identity the
	// reconciler asks the chain about; it is not a digest of our own making.
	TransactionHash string

	// ValidUntil is the transaction's own signed maxTime. Once the chain's
	// clock passes it, the network will refuse the transaction, which is what
	// turns a NOT_FOUND into proof.
	//
	// Zero means the transaction carries no upper time bound, and therefore
	// can never be proven un-landable. Such a submission is never retried.
	ValidUntil time.Time

	SourceAccount string
	DomainAction  string
	State         SubmissionState
	Attempt       int

	CreatedAt   time.Time
	SubmittedAt *time.Time
	ResolvedAt  *time.Time

	// OutcomeDetail records how the state was reached, for audit. It never
	// carries the signed envelope or any key material.
	OutcomeDetail string
}

// ErrNoTimeBound is returned when a transaction carries no maxTime.
//
// Such a transaction can never be proven un-landable — there is no moment
// after which the network is guaranteed to refuse it — so it can never be
// safely retried. Submissions are built with time bounds precisely so this
// does not happen; the error exists so a future change that drops them fails
// loudly rather than silently creating unresolvable submissions.
var ErrNoTimeBound = errors.New("transaction has no maxTime and can never be proven un-landable")

// TransactionIdentity is the chain's identity for a signed envelope: the hash
// the network will know it by, and the validity window it enforces.
type TransactionIdentity struct {
	Hash       string
	ValidUntil time.Time
}

// IdentifyTransaction derives a signed envelope's chain identity without
// submitting it.
//
// This is what makes the whole design possible: because the hash is a
// deterministic function of the signed envelope and the network passphrase,
// we know what to ask the chain about before we take any risk. The reconciler
// never has to guess which transaction a record refers to, and never has to
// match on weak signals like amount, account, or timestamp.
func IdentifyTransaction(signedEnvelopeB64, networkPassphrase string) (TransactionIdentity, error) {
	generic, err := txnbuild.TransactionFromXDR(signedEnvelopeB64)
	if err != nil {
		return TransactionIdentity{}, fmt.Errorf("parse signed envelope: %w", err)
	}

	inner, ok := generic.Transaction()
	if !ok {
		// Fee-bump envelopes hash differently and are not produced by any
		// path here; refusing is safer than hashing the wrong thing.
		return TransactionIdentity{}, errors.New("expected a transaction, got fee-bump")
	}

	hash, err := inner.HashHex(networkPassphrase)
	if err != nil {
		return TransactionIdentity{}, fmt.Errorf("hash transaction: %w", err)
	}

	identity := TransactionIdentity{Hash: hash}

	// MaxTime is an unsigned Stellar timepoint. Converting a value above
	// MaxInt64 straight to int64 wraps it negative, which would read as a
	// transaction that expired long ago — and expiry is what this package
	// treats as proof a transaction is dead and safe to replace. Values that
	// large are not real deadlines, so they are treated as no bound at all
	// rather than as an expiry in the distant past.
	if bounds := inner.ToXDR().V1.Tx.Cond.TimeBounds; bounds != nil && bounds.MaxTime > 0 {
		if maxTime := uint64(bounds.MaxTime); maxTime <= math.MaxInt64 {
			identity.ValidUntil = time.Unix(int64(maxTime), 0).UTC()
		}
	}

	return identity, nil
}

// DetermineOutcome converts one getTransaction response into a chain outcome.
//
// Pure, so the reasoning that governs whether money can move twice is
// testable without a database, an RPC, or a clock.
//
// The NOT_FOUND branch is the delicate one, and it is where a naive
// implementation goes wrong. NOT_FOUND alone means nothing: it is returned
// for a transaction that was never submitted, for one still propagating, and
// for one the RPC has simply forgotten. Turning it into proof needs two
// further facts, both taken from the chain rather than from us:
//
//  1. The chain's clock has passed the transaction's own signed maxTime, so
//     the network is now guaranteed to refuse it.
//  2. The RPC's history still reaches back far enough to have seen the
//     transaction if it had landed. Otherwise NOT_FOUND means "outside my
//     memory", and the honest answer is that we can no longer tell.
func DetermineOutcome(status TransactionStatus, view ChainView, intent SubmissionIntent) ChainOutcome {
	switch status {
	case TxStatusSuccess:
		return OutcomeLanded
	case TxStatusFailed:
		// It landed. The operation did not take effect, but the sequence
		// number is spent, so this envelope is permanently dead and a fresh
		// attempt is safe.
		return OutcomeRejected
	case TxStatusNotFound:
		// Fall through to the reasoning below.
	default:
		// An unrecognised status is not an answer.
		return OutcomeUnknown
	}

	// Without the chain's clock and memory there is nothing to reason from.
	if !view.Valid() {
		return OutcomeUnknown
	}

	// A transaction with no upper time bound can never be proven dead: there
	// is no point after which the network must refuse it, so it might still
	// land at any time.
	if intent.ValidUntil.IsZero() {
		return OutcomeUnknown
	}

	// Still inside its validity window: it can yet be included. This is the
	// ordinary case immediately after a timeout, and the correct answer is
	// to keep waiting.
	if !view.LatestLedgerCloseTime.After(intent.ValidUntil) {
		return OutcomeUnknown
	}

	// The window has closed. Before treating NOT_FOUND as proof, check that
	// the RPC could still have seen the transaction had it landed. If its
	// history now starts after the transaction's window opened, a landed
	// transaction would also read NOT_FOUND, and the two are indistinguishable.
	//
	// This is the case a long outage produces, and it must not be allowed to
	// look like proof — that is exactly how a double-submit would happen.
	if view.OldestLedgerCloseTime.After(intent.EarliestInclusion()) {
		return OutcomeUndeterminable
	}

	return OutcomeProvenNotLanded
}

// EarliestInclusion is the earliest moment the transaction could have been
// included in a ledger. Nothing can have happened to it before it existed, so
// the RPC's history only needs to reach back this far to be conclusive.
func (i SubmissionIntent) EarliestInclusion() time.Time {
	if i.SubmittedAt != nil {
		return *i.SubmittedAt
	}
	return i.CreatedAt
}
