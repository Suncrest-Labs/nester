package stellar

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// SubmissionReconciler resolves pending chain submissions against the chain
// (nester#1085).
//
// It is the only thing in the system permitted to decide that a submission
// ended, and it decides by asking the chain about a specific transaction
// hash. It never resubmits, never infers an outcome from elapsed time, and
// never treats an unreachable RPC as an answer.
//
// It follows the repository's existing background-worker shape: a Run loop on
// a ticker, gated by the same LeaderChecker the rebalancer and protocol-health
// jobs use, so exactly one instance reconciles at a time.
type SubmissionReconciler struct {
	store  SubmissionStore
	chain  ChainLookup
	logger *slog.Logger

	interval time.Duration
	batch    int

	// leader gates the sweep. Nil means "always leader", matching the
	// convention in internal/scheduler so single-instance deployments and
	// tests are unaffected.
	leader LeaderChecker

	now func() time.Time
}

// LeaderChecker reports whether this instance currently holds leadership.
// Structurally identical to scheduler.LeaderChecker; declared here so the
// stellar package does not depend on the scheduler package.
type LeaderChecker interface {
	IsLeader() bool
}

// Defaults for the reconciliation sweep.
//
// The interval is short relative to the five-minute transaction time bounds,
// so a submission that will resolve does so promptly; the batch bounds how
// much work one sweep does, so a backlog drains steadily rather than in one
// burst against a recovering RPC.
const (
	DefaultReconcileInterval = 15 * time.Second
	DefaultReconcileBatch    = 50
)

// NewSubmissionReconciler builds a reconciler. A nil logger, zero interval, or
// zero batch take sensible defaults.
func NewSubmissionReconciler(store SubmissionStore, chain ChainLookup, logger *slog.Logger) *SubmissionReconciler {
	return &SubmissionReconciler{
		store:    store,
		chain:    chain,
		logger:   logger,
		interval: DefaultReconcileInterval,
		batch:    DefaultReconcileBatch,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// SetLeaderChecker wires leader election so only one instance sweeps.
func (r *SubmissionReconciler) SetLeaderChecker(l LeaderChecker) { r.leader = l }

// SetInterval overrides the sweep cadence.
func (r *SubmissionReconciler) SetInterval(d time.Duration) {
	if d > 0 {
		r.interval = d
	}
}

// SetClock injects a clock, so tests drive the sweep without sleeping.
func (r *SubmissionReconciler) SetClock(now func() time.Time) {
	if now != nil {
		r.now = now
	}
}

func (r *SubmissionReconciler) isLeader() bool {
	return r.leader == nil || r.leader.IsLeader()
}

// Run sweeps until ctx is cancelled.
//
// Recovery after a restart needs no special path: the intents are in the
// database, so the first sweep of a freshly started process picks up
// everything the previous one left pending. No submission state lives in
// memory.
func (r *SubmissionReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Tick(ctx)
		}
	}
}

// Tick performs one sweep. Exported so tests drive it directly rather than
// waiting on a ticker.
func (r *SubmissionReconciler) Tick(ctx context.Context) {
	if !r.isLeader() {
		return
	}

	// SKIP LOCKED in the store means a concurrent reconciler takes a
	// different set, so two instances never work the same submission — and
	// since neither ever resubmits, the worst case of an overlap is a
	// duplicate read of the chain.
	intents, err := r.store.ClaimPendingForReconcile(ctx, r.batch, r.now())
	if err != nil {
		r.log().Error("submission reconciler: failed to claim pending submissions", "error", err.Error())
		return
	}

	for _, intent := range intents {
		if ctx.Err() != nil {
			return
		}
		r.reconcile(ctx, intent)
	}
}

// reconcile resolves one submission, or leaves it pending.
//
// Every branch that does not produce a chain-derived outcome leaves the record
// exactly as it found it. That is the whole job.
func (r *SubmissionReconciler) reconcile(ctx context.Context, intent SubmissionIntent) {
	status, view, err := r.chain.LookupTransaction(ctx, intent.TransactionHash)
	if err != nil {
		// The RPC is unreachable, timing out, or the circuit breaker is open.
		// None of those is information about the transaction. Debug, not
		// error: during an outage this runs every sweep for every pending
		// submission, and logging it loudly would bury the incident in its
		// own noise.
		r.log().Debug("submission reconciler: chain unreachable, leaving submission pending",
			"submission_id", intent.ID,
			"transaction_hash", intent.TransactionHash,
			"error", err.Error(),
		)
		return
	}

	outcome := DetermineOutcome(status, view, intent)
	if outcome == OutcomeUnknown {
		// The chain answered, but not conclusively — typically the
		// transaction is still inside its validity window and may yet be
		// included. Waiting is the correct action.
		r.log().Debug("submission reconciler: outcome not yet determined",
			"submission_id", intent.ID,
			"transaction_hash", intent.TransactionHash,
			"chain_status", string(status),
		)
		return
	}

	state := outcome.State()
	detail := "chain reported " + string(status) + "; outcome " + outcome.String()

	if err := r.store.Resolve(ctx, intent.ID, state, detail, r.now()); err != nil {
		if errors.Is(err, ErrIntentNotFound) {
			// Already resolved by another sweep, or purged. Nothing to do.
			return
		}
		r.log().Error("submission reconciler: failed to persist outcome",
			"submission_id", intent.ID,
			"state", string(state),
			"error", err.Error(),
		)
		return
	}

	r.logResolved(intent, status, outcome, state)
}

func (r *SubmissionReconciler) logResolved(
	intent SubmissionIntent,
	status TransactionStatus,
	outcome ChainOutcome,
	state SubmissionState,
) {
	attrs := []any{
		"submission_id", intent.ID,
		"transaction_hash", intent.TransactionHash,
		"chain_status", string(status),
		"outcome", outcome.String(),
		"state", string(state),
		// Surfaced explicitly because it is the safety-critical fact: whether
		// this submission is now eligible for a fresh attempt, and why.
		"permits_new_attempt", outcome.PermitsNewAttempt(),
	}

	switch state {
	case SubmissionLanded:
		r.log().Info("submission reconciler: transaction landed", attrs...)
	case SubmissionUnresolvable:
		// The one case a human must look at: the chain can no longer tell us
		// what happened, so neither can we.
		r.log().Error("submission reconciler: submission can no longer be resolved against the chain", attrs...)
	default:
		r.log().Warn("submission reconciler: transaction did not take effect", attrs...)
	}
}

func (r *SubmissionReconciler) log() *slog.Logger {
	if r.logger == nil {
		return slog.Default()
	}
	return r.logger
}
