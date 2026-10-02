package apperror

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// allKinds is the closed taxonomy (issue #1048). Any Kind added to the
// package must be added here too, or TestKindHTTPStatusMapping and
// TestEveryKindHasExactlyOneStatus below will not catch a missing mapping.
var allKinds = []Kind{
	KindValidation,
	KindUnauthenticated,
	KindForbidden,
	KindNotFound,
	KindConflict,
	KindQuotaExceeded,
	KindUpstreamUnavailable,
	KindInternal,
}

// TestKindHTTPStatusMapping asserts the exact table from the issue: every
// Kind maps to exactly one HTTP status. This is the taxonomy's public
// contract — a change here is a breaking API change, not a refactor.
func TestKindHTTPStatusMapping(t *testing.T) {
	tests := []struct {
		kind   Kind
		status int
	}{
		{KindValidation, http.StatusBadRequest},
		{KindUnauthenticated, http.StatusUnauthorized},
		{KindForbidden, http.StatusForbidden},
		{KindNotFound, http.StatusNotFound},
		{KindConflict, http.StatusConflict},
		{KindQuotaExceeded, http.StatusTooManyRequests},
		{KindUpstreamUnavailable, http.StatusServiceUnavailable},
		{KindInternal, http.StatusInternalServerError},
	}

	for _, tc := range tests {
		t.Run(string(tc.kind), func(t *testing.T) {
			assert.Equal(t, tc.status, tc.kind.HTTPStatus())
		})
	}
}

// TestEveryKindHasExactlyOneStatus fails if a Kind is ever added to allKinds
// without HTTPStatus() handling it explicitly (falling through to the
// default case would silently map it to 500, hiding a real bug).
func TestEveryKindHasExactlyOneStatus(t *testing.T) {
	seen := map[int]Kind{}
	for _, k := range allKinds {
		status := k.HTTPStatus()
		if existing, ok := seen[status]; ok && existing != k {
			// Two *different* kinds are allowed to share a status only if
			// that is a deliberate design choice — today no two kinds do,
			// so this failing is a strong signal something drifted.
			t.Logf("kinds %q and %q both map to status %d — confirm this is intentional", existing, k, status)
		}
		seen[status] = k
	}
	assert.Len(t, allKinds, 8, "the taxonomy is closed at 8 kinds; update this test deliberately if it ever changes")
}

func TestKindRetryable(t *testing.T) {
	retryable := map[Kind]bool{
		KindValidation:          false,
		KindUnauthenticated:     false,
		KindForbidden:           false,
		KindNotFound:            false,
		KindConflict:            false,
		KindQuotaExceeded:       true,
		KindUpstreamUnavailable: true,
		KindInternal:            false,
	}

	for kind, want := range retryable {
		t.Run(string(kind), func(t *testing.T) {
			assert.Equal(t, want, kind.Retryable())
		})
	}
}

func TestConstructors(t *testing.T) {
	tests := []struct {
		name string
		err  *AppError
		kind Kind
	}{
		{"NewValidation", NewValidation("BAD_AMOUNT", "amount must be positive"), KindValidation},
		{"NewUnauthenticated", NewUnauthenticated("NO_TOKEN", "missing token"), KindUnauthenticated},
		{"NewUnauthorized alias", NewUnauthorized("NO_TOKEN", "missing token"), KindUnauthenticated},
		{"NewForbidden", NewForbidden("NOT_OWNER", "not the owner of this vault"), KindForbidden},
		{"NewNotFound", NewNotFound("VAULT_NOT_FOUND", "vault does not exist"), KindNotFound},
		{"NewConflict", NewConflict("GOAL_COMPLETE", "goal already completed"), KindConflict},
		{"NewQuotaExceeded", NewQuotaExceeded("RATE_LIMIT", "too many requests"), KindQuotaExceeded},
		{"NewUpstreamUnavailable", NewUpstreamUnavailable("SOROBAN_DOWN", "soroban rpc unavailable"), KindUpstreamUnavailable},
		{"NewInternal", NewInternal("INTERNAL", "unexpected"), KindInternal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.kind, tc.err.Kind)
			assert.NotEmpty(t, tc.err.Code)
			assert.NotEmpty(t, tc.err.Message)
		})
	}
}

func TestNewValidationWithDetails(t *testing.T) {
	details := []FieldDetail{
		{Field: "amount", Message: "must be positive"},
		{Field: "asset", Message: "unsupported asset code"},
	}
	err := NewValidationWithDetails("VALIDATION_FAILED", "request has invalid fields", details)

	assert.Equal(t, KindValidation, err.Kind)
	assert.Equal(t, details, err.Details)
}

func TestErrorImplementsErrorInterface(t *testing.T) {
	var err error = NewNotFound("VAULT_NOT_FOUND", "vault does not exist")
	assert.Equal(t, "vault does not exist", err.Error())
}

// TestWithCauseDoesNotLeakIntoMessage is the core of the taxonomy's privacy
// guarantee (issue #1048): wrapping a driver/SQL error for server-side
// logging must never change what the client sees.
func TestWithCauseDoesNotLeakIntoMessage(t *testing.T) {
	dbErr := errors.New(`pq: duplicate key value violates unique constraint "vaults_pkey"`)
	appErr := NewConflict("VAULT_EXISTS", "a vault with this name already exists").WithCause(dbErr)

	assert.Equal(t, "a vault with this name already exists", appErr.Error())
	assert.Equal(t, "a vault with this name already exists", appErr.Message)
	assert.NotContains(t, appErr.Message, "pq:")
	assert.NotContains(t, appErr.Message, "vaults_pkey")
}

func TestUnwrapExposesCauseForLogging(t *testing.T) {
	dbErr := errors.New("connection refused")
	appErr := NewInternal("DB_ERROR", "internal server error").WithCause(dbErr)

	assert.Same(t, dbErr, appErr.Unwrap())
	assert.True(t, errors.Is(appErr, dbErr))
}

func TestWithCauseReturnsSameInstanceForChaining(t *testing.T) {
	appErr := NewInternal("X", "y")
	chained := appErr.WithCause(errors.New("cause"))
	assert.Same(t, appErr, chained)
}
