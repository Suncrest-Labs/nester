package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/suncrestlabs/nester/apps/api/pkg/apperror"
)

func TestHelpers(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		resp := OK("test_data")
		assert.True(t, resp.Success)
		assert.Equal(t, "test_data", resp.Data)
		assert.Nil(t, resp.Error)
	})

	t.Run("Created", func(t *testing.T) {
		resp := Created("test_data")
		assert.True(t, resp.Success)
		assert.Equal(t, "test_data", resp.Data)
		assert.Nil(t, resp.Error)
	})

	t.Run("Err", func(t *testing.T) {
		resp := Err(500, "ERR_CODE", "Error message")
		assert.False(t, resp.Success)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, "ERR_CODE", resp.Error.Code)
		assert.Equal(t, "Error message", resp.Error.Message)
	})

	t.Run("NotFound", func(t *testing.T) {
		resp := NotFound("vault")
		assert.False(t, resp.Success)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, "NOT_FOUND", resp.Error.Code)
		assert.Equal(t, "vault not found", resp.Error.Message)
	})

	t.Run("ValidationErr", func(t *testing.T) {
		resp := ValidationErr("invalid payload")
		assert.False(t, resp.Success)
		assert.NotNil(t, resp.Error)
		assert.Equal(t, "VALIDATION_ERROR", resp.Error.Code)
		assert.Equal(t, "invalid payload", resp.Error.Message)
	})
}

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	resp := OK("written")

	WriteJSON(w, http.StatusOK, resp)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var body Response
	err := json.NewDecoder(w.Body).Decode(&body)
	assert.NoError(t, err)
	assert.True(t, body.Success)
	assert.Equal(t, "written", body.Data)
}

// TestFromAppError covers issue #1048's envelope construction: Code,
// Message, Retryable and Details must all come from the AppError, not be
// re-derived or guessed by the caller.
func TestFromAppError(t *testing.T) {
	t.Run("non-retryable kind", func(t *testing.T) {
		appErr := apperror.NewNotFound("VAULT_NOT_FOUND", "vault does not exist")
		resp := FromAppError(appErr, "req-123")

		assert.False(t, resp.Success)
		assert.Equal(t, "VAULT_NOT_FOUND", resp.Error.Code)
		assert.Equal(t, "vault does not exist", resp.Error.Message)
		assert.Equal(t, "req-123", resp.Error.RequestID)
		assert.False(t, resp.Error.Retryable)
		assert.Nil(t, resp.Error.Details)
	})

	t.Run("retryable kind", func(t *testing.T) {
		appErr := apperror.NewUpstreamUnavailable("SOROBAN_DOWN", "soroban rpc unavailable")
		resp := FromAppError(appErr, "req-456")

		assert.True(t, resp.Error.Retryable)
	})

	t.Run("validation error carries field details", func(t *testing.T) {
		details := []apperror.FieldDetail{{Field: "amount", Message: "must be positive"}}
		appErr := apperror.NewValidationWithDetails("VALIDATION_FAILED", "invalid request", details)
		resp := FromAppError(appErr, "req-789")

		assert.Equal(t, details, resp.Error.Details)
	})
}
