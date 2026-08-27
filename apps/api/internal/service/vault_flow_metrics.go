package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// Deposit and withdrawal SLI instrumentation for nester#1056.
//
// The classification lives beside the service rather than in the metrics
// package because it depends on the vault domain's sentinel errors, and the
// metrics package must not import a domain package: it is imported by every
// instrumented package, and a cycle would be one import away.

// classifyFlowError maps a service error to its SLI outcome.
//
// The default is OutcomeFailedInternal. That direction matters: an error kind
// nobody has classified yet is counted against the budget, so a new failure
// mode shows up as a burn rather than disappearing. Defaulting to rejected
// would let an unclassified fault silently vanish from the denominator, which
// is the failure an SLI is least able to recover from.
func classifyFlowError(err error) metrics.FlowOutcome {
	if err == nil {
		return metrics.OutcomeSucceeded
	}

	// Requests the service correctly refused before reaching the chain. These
	// are excluded from the SLI denominator: they are the validation layer
	// working, and counting them would let a client burn the error budget by
	// looping a malformed request.
	switch {
	case errors.Is(err, vault.ErrInvalidVault),
		errors.Is(err, vault.ErrInvalidAmount),
		errors.Is(err, vault.ErrInvalidPrecision),
		errors.Is(err, vault.ErrVaultNotFound),
		errors.Is(err, vault.ErrUserNotFound),
		errors.Is(err, vault.ErrVaultForbidden),
		errors.Is(err, vault.ErrVaultClosed),
		errors.Is(err, vault.ErrVaultNotActive),
		errors.Is(err, vault.ErrInsufficientBalance),
		errors.Is(err, vault.ErrWithdrawalExceedsPosition),
		errors.Is(err, vault.ErrTxHashRequired),
		errors.Is(err, vault.ErrUnverifiedChainTx),
		errors.Is(err, vault.ErrCapacityExceeded),
		errors.Is(err, vault.ErrDuplicateTransaction),
		// The contract refused a sub-minimum deposit. Classified as rejected
		// rather than as a chain failure because the request was invalid, not
		// the chain: counting it as a failure would let a client-side bug
		// that permits sub-minimum amounts burn the deposit budget.
		errors.Is(err, vault.ErrBelowMinDeposit):
		return metrics.OutcomeRejected

	// The user declined the wallet signature or abandoned the attempt.
	// Excluded from the denominator per the issue, counted separately so a
	// cancellation wave from a broken signing prompt stays visible.
	case errors.Is(err, vault.ErrUserCancelled):
		return metrics.OutcomeCancelled
	}

	// Soroban invocation failures. RecordDeposit and RecordWithdrawal wrap
	// these with a fixed prefix; matching the prefix rather than the wrapped
	// error keeps the classification correct for every RPC and contract error
	// the invoker can return without enumerating them.
	//
	// A chain failure counts against the budget. The API does not own Soroban
	// RPC, but the user cannot tell the difference between "the network was
	// down" and "we could not reach the network", and an SLI that excused
	// unowned infrastructure would report health through a total outage.
	if strings.Contains(err.Error(), "on-chain deposit failed") ||
		strings.Contains(err.Error(), "on-chain withdrawal failed") {
		return metrics.OutcomeFailedChain
	}

	// Context cancellation and deadline expiry. The attempt reached the chain
	// or the database and did not come back in time, which is a failure of
	// the flow regardless of which side gave up first.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return metrics.OutcomeFailedChain
	}

	return metrics.OutcomeFailedInternal
}

// recordFlow reports one terminal flow attempt.
//
// Intended to be deferred immediately after the attempt's start time is
// taken, reading the named error return:
//
//	defer func() { recordFlow(s.metrics, metrics.FlowDeposit, started, err) }()
//
// Deferring rather than calling on each return path is deliberate. These
// methods have a dozen early returns and more will be added; a call per branch
// makes an unrecorded branch a silent omission from the denominator, which
// inflates the reported success rate. That is the one direction of error an
// SLI must never make, so the recording is structurally impossible to skip.
func recordFlow(m *metrics.Metrics, flow metrics.Flow, startedAt time.Time, err error) {
	if m == nil {
		return
	}

	recordFlowOutcome(m, flow, startedAt, classifyFlowError(err))
}

// recordFlowOutcome reports a terminal outcome that is already classified.
// Used by callers that know the outcome without an error to inspect, such as
// a handler observing an explicit user cancellation.
func recordFlowOutcome(m *metrics.Metrics, flow metrics.Flow, startedAt time.Time, outcome metrics.FlowOutcome) {
	if m == nil {
		return
	}

	var elapsed time.Duration
	if !startedAt.IsZero() {
		elapsed = time.Since(startedAt)
	}

	m.RecordFlowAttempt(flow, outcome, elapsed)
}
