// Package api provides shared HTTP response helpers used by all handlers.
//
// Every API response follows a standard envelope:
//
//	Success: {"success":true,"data":{...}}
//	Error:   {"success":false,"error":{"code":400,"message":"..."}}
//
// Using these helpers ensures consistency across all endpoints and makes
// client-side parsing predictable.
package api

import (
	"encoding/json"
	"net/http"
)

// envelope is the top-level wrapper for every API response.
// Data intentionally has no omitempty: a nil payload must still produce
// "data":null rather than omitting the field entirely.
type envelope struct {
	Success bool      `json:"success"`
	Data    any       `json:"data"`
	Error   *apiError `json:"error,omitempty"`
}

// apiError carries the HTTP status code and a human-readable message.
type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON writes a success envelope with the given status code and data payload.
// If data is nil, the response body will be {"success":true,"data":null}.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := envelope{Success: true, Data: data}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Encoding already started — we cannot change the status code.
		// Write a minimal fallback so the body is not empty.
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":500,"message":"encoding error"}}`))
	}
}

// Error writes an error envelope with the given status code and message.
// The message should be safe to expose to clients (no stack traces or
// internal details).
func Error(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := envelope{
		Success: false,
		Error:   &apiError{Code: status, Message: message},
	}
	// If encoding fails here we cannot do much — headers are already sent.
	_ = json.NewEncoder(w).Encode(resp)
}

// FieldError represents a single field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// validationEnvelope wraps field-level validation errors.
type validationEnvelope struct {
	Success bool             `json:"success"`
	Error   *validationError `json:"error"`
}

type validationError struct {
	Code    int          `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields"`
}

// ValidationError writes a 422 error envelope including field-level error details.
func ValidationError(w http.ResponseWriter, fields []FieldError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)

	resp := validationEnvelope{
		Success: false,
		Error: &validationError{
			Code:    http.StatusUnprocessableEntity,
			Message: "validation failed",
			Fields:  fields,
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
