package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
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
