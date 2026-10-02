package vault

import (
	"errors"
	"fmt"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
)

// TestSentinelErrorsAreTypedAppErrors covers nester#1341: every sentinel
// error this package exports must be an *apperror.AppError with a non-empty
// stable Code, reachable via errors.As - not merely an opaque errors.New
// string a caller can only compare by identity.
func TestSentinelErrorsAreTypedAppErrors(t *testing.T) {
	sentinels := map[string]struct {
		err      error
		wantKind apperror.Kind
	}{
		"ErrVaultNotFound":                {ErrVaultNotFound, apperror.KindNotFound},
		"ErrUserNotFound":                 {ErrUserNotFound, apperror.KindNotFound},
		"ErrInvalidVault":                 {ErrInvalidVault, apperror.KindValidation},
		"ErrInvalidAmount":                {ErrInvalidAmount, apperror.KindValidation},
		"ErrInvalidAllocation":            {ErrInvalidAllocation, apperror.KindValidation},
		"ErrInvalidPrecision":             {ErrInvalidPrecision, apperror.KindValidation},
		"ErrInvalidTransition":            {ErrInvalidTransition, apperror.KindValidation},
		"ErrVaultClosed":                  {ErrVaultClosed, apperror.KindValidation},
		"ErrVaultNotActive":               {ErrVaultNotActive, apperror.KindValidation},
		"ErrInsufficientBalance":          {ErrInsufficientBalance, apperror.KindValidation},
		"ErrWithdrawalExceedsPosition":    {ErrWithdrawalExceedsPosition, apperror.KindValidation},
		"ErrTxHashRequired":               {ErrTxHashRequired, apperror.KindValidation},
		"ErrUnverifiedChainTx":            {ErrUnverifiedChainTx, apperror.KindValidation},
		"ErrChainVerificationUnavailable": {ErrChainVerificationUnavailable, apperror.KindForbidden},
		"ErrChainEventCallerMismatch":     {ErrChainEventCallerMismatch, apperror.KindForbidden},
		"ErrVaultForbidden":               {ErrVaultForbidden, apperror.KindForbidden},
		"ErrAllocationNotFound":           {ErrAllocationNotFound, apperror.KindNotFound},
		"ErrAllocationHasBalance":         {ErrAllocationHasBalance, apperror.KindConflict},
		"ErrDuplicateProtocol":            {ErrDuplicateProtocol, apperror.KindConflict},
		"ErrBelowMinDeposit":              {ErrBelowMinDeposit, apperror.KindValidation},
		"ErrInvalidHarvestFrequency":      {ErrInvalidHarvestFrequency, apperror.KindValidation},
		"ErrDuplicateTransaction":         {ErrDuplicateTransaction, apperror.KindConflict},
		"ErrContractAddressRegistered":    {ErrContractAddressRegistered, apperror.KindConflict},
		"ErrInvalidSharePrice":            {ErrInvalidSharePrice, apperror.KindInternal},
		"ErrOperatorFundedDepositRefused": {ErrOperatorFundedDepositRefused, apperror.KindForbidden},
		"ErrCapacityExceeded":             {ErrCapacityExceeded, apperror.KindValidation},
		"ErrUserCancelled":                {ErrUserCancelled, apperror.KindValidation},
		"ErrYieldSourceNotFound":          {ErrYieldSourceNotFound, apperror.KindNotFound},
		"ErrYieldSourceDeactivated":       {ErrYieldSourceDeactivated, apperror.KindValidation},
		"ErrMigrationTargetRequired":      {ErrMigrationTargetRequired, apperror.KindValidation},
		"ErrMigrationTargetInactive":      {ErrMigrationTargetInactive, apperror.KindValidation},
		"ErrMigrationSourceActive":        {ErrMigrationSourceActive, apperror.KindValidation},
		"ErrMigrationSourceEqualsTarget":  {ErrMigrationSourceEqualsTarget, apperror.KindValidation},
		"ErrMigrationAmountInvalid":       {ErrMigrationAmountInvalid, apperror.KindValidation},
		"ErrMigrationAmountExceeded":      {ErrMigrationAmountExceeded, apperror.KindValidation},
	}

	seenCodes := make(map[string]string)
	for name, tc := range sentinels {
		t.Run(name, func(t *testing.T) {
			var appErr *apperror.AppError
			if !errors.As(tc.err, &appErr) {
				t.Fatalf("%s is not an *apperror.AppError (errors.As failed)", name)
			}
			if appErr.Kind != tc.wantKind {
				t.Errorf("%s Kind = %q, want %q", name, appErr.Kind, tc.wantKind)
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
// matches a sentinel against itself, and against a value a function
// "returns" it as (the exact pattern every existing call site like
// errors.Is(err, vault.ErrVaultNotFound) across the codebase relies on).
// This is what makes the apperror retrofit a non-breaking change: it would
// be silently broken if sentinels were redefined as new equal-looking
// values instead of being converted in place.
func TestSentinelErrorsRemainComparableByIdentity(t *testing.T) {
	wrapped := fmt.Errorf("lookup failed: %w", ErrVaultNotFound)
	if !errors.Is(wrapped, ErrVaultNotFound) {
		t.Error("expected errors.Is to match a wrapped ErrVaultNotFound by identity")
	}
	if errors.Is(wrapped, ErrUserNotFound) {
		t.Error("expected errors.Is to NOT match a different sentinel")
	}
}
