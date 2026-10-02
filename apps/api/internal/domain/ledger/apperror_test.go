package ledger

import (
	"errors"
	"fmt"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
)

// TestSentinelErrorsAreTypedAppErrors covers nester#1341: every sentinel
// error this package exports must be an *apperror.AppError with a
// non-empty, unique stable Code reachable via errors.As.
func TestSentinelErrorsAreTypedAppErrors(t *testing.T) {
	sentinels := map[string]error{
		"ErrInvalidAccountType": ErrInvalidAccountType,
		"ErrUnbalanced":         ErrUnbalanced,
		"ErrTooFewEntries":      ErrTooFewEntries,
		"ErrEmptyTransactionID": ErrEmptyTransactionID,
		"ErrZeroAmount":         ErrZeroAmount,
	}

	seenCodes := make(map[string]string)
	for name, err := range sentinels {
		t.Run(name, func(t *testing.T) {
			var appErr *apperror.AppError
			if !errors.As(err, &appErr) {
				t.Fatalf("%s is not an *apperror.AppError (errors.As failed)", name)
			}
			if appErr.Kind != apperror.KindValidation {
				t.Errorf("%s Kind = %q, want %q (every ledger sentinel describes a malformed entry set)", name, appErr.Kind, apperror.KindValidation)
			}
			if appErr.Code == "" {
				t.Errorf("%s has an empty Code", name)
			}
			if other, dup := seenCodes[appErr.Code]; dup {
				t.Errorf("%s and %s share the same Code %q; codes must be unique", name, other, appErr.Code)
			}
			seenCodes[appErr.Code] = name
		})
	}
}

// TestSentinelErrorsRemainComparableByIdentity confirms errors.Is still
// matches a sentinel wrapped by fmt.Errorf("%w", ...), the pattern every
// existing errors.Is(err, ledger.ErrX) call site relies on.
func TestSentinelErrorsRemainComparableByIdentity(t *testing.T) {
	wrapped := fmt.Errorf("validate failed: %w", ErrUnbalanced)
	if !errors.Is(wrapped, ErrUnbalanced) {
		t.Error("expected errors.Is to match a wrapped ErrUnbalanced by identity")
	}
	if errors.Is(wrapped, ErrTooFewEntries) {
		t.Error("expected errors.Is to NOT match a different sentinel")
	}
}
