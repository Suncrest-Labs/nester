package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

func TestErrorHandler(t *testing.T) {
	tests := []struct {
		name           string
		returnedError  error
		expectedStatus int
		expectedCode   string
		expectedMsg    string
	}{
		{
			name:           "Not Found Error",
			returnedError:  apperror.NewNotFound("NOT_FOUND", "vault not found"),
			expectedStatus: http.StatusNotFound,
			expectedCode:   "NOT_FOUND",
			expectedMsg:    "vault not found",
		},
		{
			name:           "Validation Error",
			returnedError:  apperror.NewValidation("VALIDATION_ERROR", "invalid input"),
			expectedStatus: http.StatusBadRequest,
			expectedCode:   "VALIDATION_ERROR",
			expectedMsg:    "invalid input",
		},
		{
			name:           "Conflict Error",
			returnedError:  apperror.NewConflict("CONFLICT", "resource exists"),
			expectedStatus: http.StatusConflict,
			expectedCode:   "CONFLICT",
			expectedMsg:    "resource exists",
		},
		{
			name:           "Unauthorized Error",
			returnedError:  apperror.NewUnauthorized("UNAUTHORIZED", "invalid token"),
			expectedStatus: http.StatusUnauthorized,
			expectedCode:   "UNAUTHORIZED",
			expectedMsg:    "invalid token",
		},
		{
			name:           "Forbidden Error",
			returnedError:  apperror.NewForbidden("NOT_OWNER", "not the owner of this vault"),
			expectedStatus: http.StatusForbidden,
			expectedCode:   "NOT_OWNER",
			expectedMsg:    "not the owner of this vault",
		},
		{
			name:           "Quota Exceeded Error",
			returnedError:  apperror.NewQuotaExceeded("RATE_LIMIT", "too many requests"),
			expectedStatus: http.StatusTooManyRequests,
			expectedCode:   "RATE_LIMIT",
			expectedMsg:    "too many requests",
		},
		{
			name:           "Upstream Unavailable Error",
			returnedError:  apperror.NewUpstreamUnavailable("SOROBAN_DOWN", "soroban rpc unavailable"),
			expectedStatus: http.StatusServiceUnavailable,
			expectedCode:   "SOROBAN_DOWN",
			expectedMsg:    "soroban rpc unavailable",
		},
		{
			name:           "Internal Error (typed)",
			returnedError:  apperror.NewInternal("INTERNAL", "internal server error"),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "INTERNAL",
			expectedMsg:    "internal server error",
		},
		{
			name:           "Unknown Error",
			returnedError:  errors.New("some unexpected database error"),
			expectedStatus: http.StatusInternalServerError,
			expectedCode:   "INTERNAL_SERVER_ERROR",
			expectedMsg:    "internal server error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
				return tc.returnedError
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)

			var body response.Response
			err := json.NewDecoder(w.Body).Decode(&body)
			assert.NoError(t, err)

			assert.False(t, body.Success)
			assert.NotNil(t, body.Error)
			assert.Equal(t, tc.expectedCode, body.Error.Code)
			assert.Equal(t, tc.expectedMsg, body.Error.Message)
		})
	}
}

// TestErrorHandler_RetryableFlag asserts the envelope's retryable flag
// matches the kind's documented retry semantics (issue #1048) — a client
// reading this field, rather than string-matching the code, must get the
// correct answer for whether retrying is ever useful.
func TestErrorHandler_RetryableFlag(t *testing.T) {
	tests := []struct {
		name          string
		returnedError error
		wantRetryable bool
	}{
		{"validation is not retryable", apperror.NewValidation("X", "bad input"), false},
		{"not_found is not retryable", apperror.NewNotFound("X", "missing"), false},
		{"quota_exceeded is retryable", apperror.NewQuotaExceeded("X", "rate limited"), true},
		{"upstream_unavailable is retryable", apperror.NewUpstreamUnavailable("X", "down"), true},
		{"untyped error defaults to internal, not retryable", errors.New("boom"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
				return tc.returnedError
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			var body response.Response
			require.NoError(t, json.NewDecoder(w.Body).Decode(&body))

			require.NotNil(t, body.Error)
			assert.Equal(t, tc.wantRetryable, body.Error.Retryable)
		})
	}
}

// TestErrorHandler_ValidationDetailsSurfaced asserts field-level validation
// details reach the client envelope unchanged (issue #1048's acceptance
// criteria calls for "an optional field-level detail array for validation").
func TestErrorHandler_ValidationDetailsSurfaced(t *testing.T) {
	details := []apperror.FieldDetail{
		{Field: "amount", Message: "must be positive"},
	}
	handler := ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		return apperror.NewValidationWithDetails("VALIDATION_FAILED", "invalid request", details)
	})

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var body response.Response
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))

	require.NotNil(t, body.Error)
	require.Len(t, body.Error.Details, 1)
	assert.Equal(t, "amount", body.Error.Details[0].Field)
	assert.Equal(t, "must be positive", body.Error.Details[0].Message)
}

// TestErrorHandler_NeverLeaksDriverOrSQLText is the enforcement test issue
// #1048 calls for: "A test proves no error response contains driver or SQL
// text." An error that is not an *apperror.AppError — a raw database/driver
// error escaping a handler uninstrumented — must never have its own Error()
// string surfaced to the client; the envelope must always fall back to the
// fixed, generic internal-error message instead.
func TestErrorHandler_NeverLeaksDriverOrSQLText(t *testing.T) {
	driverErrors := []error{
		errors.New(`pq: duplicate key value violates unique constraint "vaults_pkey"`),
		errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"),
		errors.New(`ERROR: syntax error at or near "SELECT" (SQLSTATE 42601)`),
	}

	for _, driverErr := range driverErrors {
		t.Run(driverErr.Error(), func(t *testing.T) {
			handler := ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
				return driverErr
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)

			body := w.Body.String()
			assert.NotContains(t, body, "pq:")
			assert.NotContains(t, body, "SQLSTATE")
			assert.NotContains(t, body, "vaults_pkey")
			assert.NotContains(t, body, "10.0.0.5")
			assert.NotContains(t, body, "connect: connection refused")

			var resp response.Response
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
			assert.Equal(t, "internal server error", resp.Error.Message)
		})
	}
}

// TestErrorHandler_AppErrorWithCauseDoesNotLeakCause ensures wrapping a
// driver error via WithCause (for server-side logging) never lets that
// cause reach the client, even when the outer AppError itself is returned.
func TestErrorHandler_AppErrorWithCauseDoesNotLeakCause(t *testing.T) {
	dbErr := errors.New(`pq: relation "vaults" does not exist`)
	handler := ErrorHandler(func(w http.ResponseWriter, r *http.Request) error {
		return apperror.NewInternal("DB_ERROR", "internal server error").WithCause(dbErr)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.NotContains(t, w.Body.String(), "pq:")
	assert.NotContains(t, w.Body.String(), "relation")
}
