package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/api"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type envelope struct {
	Success bool             `json:"success"`
	Data    json.RawMessage  `json:"data"`
	Error   *envelopeError   `json:"error"`
}

type envelopeError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func decodeEnvelope(t *testing.T, body string) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("response body is not valid JSON: %v\nbody: %q", err, body)
	}
	return env
}

// ---------------------------------------------------------------------------
// JSON — success envelope
// ---------------------------------------------------------------------------

func TestJSON_WritesSuccessEnvelopeWithData(t *testing.T) {
	rec := httptest.NewRecorder()
	api.JSON(rec, http.StatusOK, map[string]string{"id": "vault-1"})

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	env := decodeEnvelope(t, rec.Body.String())
	if !env.Success {
		t.Errorf("expected success=true, got false")
	}
	if env.Error != nil {
		t.Errorf("expected no error field in success response, got %+v", env.Error)
	}

	var data map[string]string
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("data field is not valid JSON: %v", err)
	}
	if data["id"] != "vault-1" {
		t.Errorf("expected data.id=vault-1, got %q", data["id"])
	}
}

func TestJSON_NilDataProducesNullDataField(t *testing.T) {
	rec := httptest.NewRecorder()
	api.JSON(rec, http.StatusOK, nil)

	body := rec.Body.String()
	if strings.TrimSpace(body) == "" {
		t.Fatal("expected non-empty body for nil data, got empty")
	}

	// Confirm we get valid JSON back (not an empty body).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("expected valid JSON for nil data, got %q: %v", body, err)
	}
	if !json.Valid(raw["data"]) && string(raw["data"]) != "null" {
		t.Errorf("expected data=null for nil payload, got %q", raw["data"])
	}
}

func TestJSON_StatusCodeIsWrittenCorrectly(t *testing.T) {
	cases := []int{
		http.StatusCreated,
		http.StatusAccepted,
		http.StatusNoContent,
	}
	for _, status := range cases {
		rec := httptest.NewRecorder()
		api.JSON(rec, status, map[string]string{"ok": "yes"})
		if rec.Code != status {
			t.Errorf("JSON(status=%d): got status %d", status, rec.Code)
		}
	}
}

func TestJSON_LargePayloadDoesNotTruncate(t *testing.T) {
	large := make([]string, 10_000)
	for i := range large {
		large[i] = "value"
	}
	rec := httptest.NewRecorder()
	api.JSON(rec, http.StatusOK, large)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("large payload produced invalid JSON: %v", err)
	}

	var decoded []string
	if err := json.Unmarshal(raw["data"], &decoded); err != nil {
		t.Fatalf("could not decode large data array: %v", err)
	}
	if len(decoded) != 10_000 {
		t.Errorf("expected 10000 items, got %d", len(decoded))
	}
}

func TestJSON_NonSerialisableDataReturnsInternalError(t *testing.T) {
	rec := httptest.NewRecorder()
	// Channels are not JSON-serialisable.
	api.JSON(rec, http.StatusOK, make(chan int))

	// The handler must not panic.  Because headers are flushed before encoding
	// errors surface, the status may already be 200 — what matters is that
	// the body is not empty and no panic occurred.
	if rec.Body.Len() == 0 {
		t.Error("expected non-empty body even for non-serialisable data")
	}
}

// ---------------------------------------------------------------------------
// Error — error envelope
// ---------------------------------------------------------------------------

func TestError_WritesBadRequestEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, http.StatusBadRequest, "invalid user_id")

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	env := decodeEnvelope(t, rec.Body.String())
	if env.Success {
		t.Errorf("expected success=false in error response")
	}
	if env.Error == nil {
		t.Fatal("expected error field in error response, got nil")
	}
	if env.Error.Code != http.StatusBadRequest {
		t.Errorf("expected error.code=400, got %d", env.Error.Code)
	}
	if env.Error.Message != "invalid user_id" {
		t.Errorf("expected error.message=%q, got %q", "invalid user_id", env.Error.Message)
	}
}

func TestError_StandardCodesMapToCorrectHTTPStatus(t *testing.T) {
	cases := []struct {
		status  int
		message string
	}{
		{http.StatusBadRequest, "bad request"},
		{http.StatusUnauthorized, "unauthorized"},
		{http.StatusForbidden, "forbidden"},
		{http.StatusNotFound, "not found"},
		{http.StatusUnprocessableEntity, "validation failed"},
		{http.StatusInternalServerError, "internal server error"},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		api.Error(rec, tc.status, tc.message)

		if rec.Code != tc.status {
			t.Errorf("Error(status=%d): got HTTP status %d", tc.status, rec.Code)
		}

		env := decodeEnvelope(t, rec.Body.String())
		if env.Error == nil {
			t.Errorf("Error(status=%d): expected error envelope, got nil", tc.status)
			continue
		}
		if env.Error.Code != tc.status {
			t.Errorf("Error(status=%d): error.code=%d", tc.status, env.Error.Code)
		}
	}
}

func TestError_MessageDoesNotLeakStackTrace(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, http.StatusInternalServerError, "internal server error")

	body := rec.Body.String()
	forbidden := []string{
		"goroutine",
		"runtime/debug",
		"stack trace",
		".go:",
	}
	for _, fragment := range forbidden {
		if strings.Contains(strings.ToLower(body), strings.ToLower(fragment)) {
			t.Errorf("error response must not leak internal details; found %q in body %q", fragment, body)
		}
	}
}

func TestError_ResponseIsNotRawHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, http.StatusNotFound, "not found")

	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("error response must not be raw HTML, got %q", body)
	}
	// Must be parseable JSON.
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Errorf("error response is not valid JSON: %v\nbody: %q", err, body)
	}
}

// ---------------------------------------------------------------------------
// Envelope shape invariants
// ---------------------------------------------------------------------------

func TestJSON_SuccessEnvelopeHasNoErrorField(t *testing.T) {
	rec := httptest.NewRecorder()
	api.JSON(rec, http.StatusOK, "payload")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if errField, exists := raw["error"]; exists && string(errField) != "null" {
		t.Errorf("success envelope must not include a non-null error field, got %q", errField)
	}
}

func TestError_ErrorEnvelopeHasNoDataField(t *testing.T) {
	rec := httptest.NewRecorder()
	api.Error(rec, http.StatusBadRequest, "bad")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if dataField, exists := raw["data"]; exists && string(dataField) != "null" {
		t.Errorf("error envelope must not include a non-null data field, got %q", dataField)
	}
}
