package stellar

import (
	"context"
	"errors"
	"time"
)

// ErrIntentNotFound is returned when no submission intent exists for a
// reference or ID.
var ErrIntentNotFound = errors.New("submission intent not found")

// ErrReferenceReused is returned when an idempotency reference is presented
// for a materially different transaction than the one it was created with.
//
// Silently reusing the original would attribute a new operation to an old
// record; silently creating a second would defeat the idempotency. Refusing
// is the only honest option, and it mirrors the 409 the HTTP idempotency
// middleware returns for a fingerprint mismatch (nester#835).
var ErrReferenceReused = errors.New("idempotency reference already used for a different transaction")

// SubmissionStore is the durable home of submission intents.
//
// It is an interface so the submit and reconcile flows can be driven
// deterministically in tests, and so the Postgres implementation's atomicity
// requirements are stated in one place rather than assumed at each call site.
type SubmissionStore interface {
	// Claim durably records the intent to submit, atomically.
	//
	// It MUST be atomic across processes — a database uniqueness constraint,
	// not a read-then-write in application code, because concurrent requests
	// with the same reference race. When the reference already exists, Claim
	// returns the existing intent with claimed=false and MUST NOT create a
	// second one; that is what collapses N concurrent duplicate requests into
	// one chain submission.
	//
	// A reference presented with a different transaction hash than the
	// stored one returns ErrReferenceReused.
	Claim(ctx context.Context, intent SubmissionIntent) (stored SubmissionIntent, claimed bool, err error)

	// MarkSubmitted records that the envelope was handed to the RPC. It is
	// called after the write is in flight and says nothing about the outcome.
	MarkSubmitted(ctx context.Context, id string, at time.Time) error

	// Resolve moves an intent to a terminal state. It MUST refuse to move an
	// already-terminal intent, so a late reconciliation cannot reopen a
	// settled submission or overwrite one outcome with another.
	Resolve(ctx context.Context, id string, state SubmissionState, detail string, at time.Time) error

	// Get returns one intent by ID.
	Get(ctx context.Context, id string) (SubmissionIntent, error)

	// ClaimPendingForReconcile returns pending intents that are due to be
	// checked against the chain, and marks them as being worked on.
	//
	// It MUST be safe for concurrent reconcilers on separate instances: two
	// workers must never take the same intent. The Postgres implementation
	// uses SELECT ... FOR UPDATE SKIP LOCKED for this.
	ClaimPendingForReconcile(ctx context.Context, limit int, now time.Time) ([]SubmissionIntent, error)
}

// ChainLookup asks the chain about one transaction by hash. Implemented by
// *ContractInvoker; declared here so the reconciler can be tested against a
// scripted chain.
type ChainLookup interface {
	LookupTransaction(ctx context.Context, hash string) (TransactionStatus, ChainView, error)
}

// referenceKey is the context key carrying a caller-supplied idempotency
// reference down to the submission chokepoint.
type referenceKey struct{}

// WithIdempotencyReference attaches a caller-supplied idempotency reference to
// ctx, to be picked up by the submission chokepoint.
//
// Context rather than a parameter threaded through eight call sites, and that
// is a deliberate trade. The enforcement lives at the chokepoint: every
// submission gets an intent record whether or not a reference was attached,
// because a caller that forgets must not thereby escape the durability
// guarantee. The reference only improves what the record is keyed by — an
// HTTP client's Idempotency-Key when there is one — rather than being the
// thing that makes the record exist.
func WithIdempotencyReference(ctx context.Context, reference string) context.Context {
	if reference == "" {
		return ctx
	}
	return context.WithValue(ctx, referenceKey{}, reference)
}

// idempotencyReferenceFrom reads a caller-supplied reference, if any.
func idempotencyReferenceFrom(ctx context.Context) string {
	reference, _ := ctx.Value(referenceKey{}).(string)
	return reference
}
