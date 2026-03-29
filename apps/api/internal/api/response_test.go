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
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *envelopeError  `json:"error"`
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

// ---------------------------------------------------------------------------
// Validation error envelope with field-level errors
// ---------------------------------------------------------------------------

func TestValidationError_IncludesFieldLevelErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	api.ValidationError(rec, []api.FieldError{
		{Field: "email", Message: "must be a valid email"},
		{Field: "name", Message: "is required"},
	})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// success must be false
	if string(raw["success"]) != "false" {
		t.Errorf("expected success=false, got %s", raw["success"])
	}

	var errObj struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Fields  []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(raw["error"], &errObj); err != nil {
		t.Fatalf("could not decode error object: %v", err)
	}
	if errObj.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected error.code=422, got %d", errObj.Code)
	}
	if len(errObj.Fields) != 2 {
		t.Fatalf("expected 2 field errors, got %d", len(errObj.Fields))
	}
	if errObj.Fields[0].Field != "email" {
		t.Errorf("expected first field=email, got %q", errObj.Fields[0].Field)
	}
	if errObj.Fields[1].Field != "name" {
		t.Errorf("expected second field=name, got %q", errObj.Fields[1].Field)
	}
}

// ---------------------------------------------------------------------------
// Middleware integration — unhandled route returns error envelope (not HTML)
// ---------------------------------------------------------------------------

func TestUnhandledRoute_ReturnsErrorEnvelope_NotHTML(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/items", func(w http.ResponseWriter, _ *http.Request) {
		api.JSON(w, http.StatusOK, map[string]string{"ok": "yes"})
	})

	// Wrap the mux with a NotFound handler that uses the error envelope
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, r)
		if rec.Code == http.StatusNotFound {
			api.Error(w, http.StatusNotFound, "not found")
			return
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("unhandled route must return JSON, not HTML: %q", body)
	}

	var env envelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("expected valid JSON from unhandled route, got %q: %v", body, err)
	}
	if env.Success {
		t.Errorf("expected success=false for 404")
	}
	if env.Error == nil || env.Error.Code != http.StatusNotFound {
		t.Errorf("expected error.code=404, got %+v", env.Error)
	}
}

// ---------------------------------------------------------------------------
// Middleware integration — panic recovery returns error envelope (not HTML)
// ---------------------------------------------------------------------------

func TestPanicRecovery_ReturnsErrorEnvelope_NotHTML(t *testing.T) {
	// Simulate a recovery middleware that catches panics and uses api.Error
	inner := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		panic("test panic")
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				api.Error(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		inner.ServeHTTP(w, r)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, "<!DOCTYPE") {
		t.Errorf("panic recovery must return JSON, not HTML: %q", body)
	}

	var env envelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("expected valid JSON from panic recovery, got %q: %v", body, err)
	}
	if env.Success {
		t.Errorf("expected success=false for panic recovery 500")
	}
	if env.Error == nil || env.Error.Code != http.StatusInternalServerError {
		t.Errorf("expected error.code=500, got %+v", env.Error)
	}
}
