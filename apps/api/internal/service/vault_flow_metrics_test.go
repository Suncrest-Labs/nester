package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/metrics"
)

// The classifier decides what burns the deposit and withdrawal error budget,
// so each case below is really an assertion about the SLI's denominator rather
// than about a switch statement.

func TestClassifyRejectedErrors(t *testing.T) {
	// Requests the service correctly refused before reaching the chain. These
	// are excluded from the SLI denominator entirely: they are the validation
	// layer working as designed, and counting them would let a client burn the
	// error budget by looping a malformed request.
	rejected := []struct {
		name string
		err  error
	}{
		{"invalid vault", vault.ErrInvalidVault},
		{"invalid amount", vault.ErrInvalidAmount},
		{"invalid precision", vault.ErrInvalidPrecision},
		{"vault not found", vault.ErrVaultNotFound},
		{"user not found", vault.ErrUserNotFound},
		{"forbidden", vault.ErrVaultForbidden},
		{"vault closed", vault.ErrVaultClosed},
		{"vault not active", vault.ErrVaultNotActive},
		{"insufficient balance", vault.ErrInsufficientBalance},
		{"withdrawal exceeds position", vault.ErrWithdrawalExceedsPosition},
		{"tx hash required", vault.ErrTxHashRequired},
		{"unverified chain tx", vault.ErrUnverifiedChainTx},
		{"capacity exceeded", vault.ErrCapacityExceeded},
		{"duplicate transaction", vault.ErrDuplicateTransaction},
		// The contract refused a sub-minimum deposit. Classified as rejected
		// rather than as a chain failure: the request was invalid, not the
		// chain, and counting it as a failure would let a client-side bug that
		// permits sub-minimum amounts burn the deposit budget.
		{"below minimum deposit", vault.ErrBelowMinDeposit},
	}

	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFlowError(tc.err); got != metrics.OutcomeRejected {
				t.Fatalf("classify(%v) = %q, want %q", tc.err, got, metrics.OutcomeRejected)
			}
		})
	}
}

// A user declining the wallet signature is excluded from the denominator on
// the issue's instruction: it is not a service failure. It is still counted
// separately, so a cancellation wave caused by a broken signing prompt stays
// visible rather than being silently dropped.
func TestClassifyUserCancellation(t *testing.T) {
	if got := classifyFlowError(vault.ErrUserCancelled); got != metrics.OutcomeCancelled {
		t.Fatalf("classify(ErrUserCancelled) = %q, want %q", got, metrics.OutcomeCancelled)
	}
}

// Chain failures count against the budget even though Soroban RPC is not ours.
// A user whose deposit did not land cannot tell "the network was down" from
// "we could not reach the network", and an SLI that excused unowned
// infrastructure would report health through a total chain outage.
func TestClassifyChainFailures(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			"wrapped deposit failure",
			fmt.Errorf("on-chain deposit failed: %w", errors.New("rpc timeout")),
		},
		{
			"wrapped withdrawal failure",
			fmt.Errorf("on-chain withdrawal failed: %w", errors.New("contract error #12")),
		},
		{"context canceled", context.Canceled},
		{"deadline exceeded", context.DeadlineExceeded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFlowError(tc.err); got != metrics.OutcomeFailedChain {
				t.Fatalf("classify(%v) = %q, want %q", tc.err, got, metrics.OutcomeFailedChain)
			}
		})
	}
}

// An unclassified error must default to a failure, never to an exclusion.
//
// This is the most important case in the file. Defaulting to rejected would
// let a new, unrecognised failure mode disappear from the denominator
// entirely, inflating the reported success rate — the one direction of error
// an SLI must never make. Defaulting to a failure means a new fault shows up
// as a burn and gets investigated.
func TestUnclassifiedErrorsDefaultToFailure(t *testing.T) {
	cases := []error{
		errors.New("some unexpected database error"),
		errors.New("connection reset by peer"),
		fmt.Errorf("wrapped: %w", errors.New("unknown")),
	}

	for _, err := range cases {
		if got := classifyFlowError(err); got != metrics.OutcomeFailedInternal {
			t.Fatalf("classify(%v) = %q, want %q", err, got, metrics.OutcomeFailedInternal)
		}
	}
}

func TestClassifyNilIsSuccess(t *testing.T) {
	if got := classifyFlowError(nil); got != metrics.OutcomeSucceeded {
		t.Fatalf("classify(nil) = %q, want %q", got, metrics.OutcomeSucceeded)
	}
}

// Sentinel errors must classify correctly through wrapping, because the
// service layer wraps them on the way out. A classifier that only matched
// unwrapped sentinels would silently reclassify most real errors as internal
// failures.
func TestSentinelsClassifyThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("record deposit: %w", vault.ErrInvalidAmount)

	if got := classifyFlowError(wrapped); got != metrics.OutcomeRejected {
		t.Fatalf("classify(wrapped ErrInvalidAmount) = %q, want %q", got, metrics.OutcomeRejected)
	}
}

// recordFlow must tolerate a nil recorder: the vault service holds an optional
// metrics pointer, and a service constructed without one must behave exactly
// as before rather than panicking in the middle of a deposit.
func TestRecordFlowWithNilMetricsIsSafe(t *testing.T) {
	recordFlow(nil, metrics.FlowDeposit, time.Now(), errors.New("boom"))
	recordFlowOutcome(nil, metrics.FlowWithdrawal, time.Time{}, metrics.OutcomeCancelled)
}
