package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

func TestLoggingInjectsRequestIDAndLogsFields(t *testing.T) {
	var buffer bytes.Buffer
	baseLogger := slog.New(slog.NewJSONHandler(&buffer, nil))

	handler := Logging(baseLogger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := logpkg.RequestIDFromContext(r.Context())
		if requestID == "" {
			t.Fatal("expected request id in context")
		}

		logpkg.FromContext(r.Context()).Info("handler log")
		w.Header().Set("X-Request-ID", requestID)
		w.WriteHeader(http.StatusCreated)
	}))

	request := httptest.NewRequest(http.MethodPost, "/api/v1/vaults", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.Code)
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected request id header from handler")
	}

	output := buffer.String()
	for _, expected := range []string{
		`"request_id":"`,
		`"method":"POST"`,
		`"path":"/api/v1/vaults"`,
		`"status":201`,
		`"duration_ms":`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected output to contain %q, got %q", expected, output)
		}
	}
}

func TestLoggingRespectsSuppliedRequestID(t *testing.T) {
	var buffer bytes.Buffer
	baseLogger := slog.New(slog.NewJSONHandler(&buffer, nil))

	handler := Logging(baseLogger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := logpkg.RequestIDFromContext(r.Context())
		if requestID != "client-123" {
			t.Fatalf("expected supplied request ID to be preserved, got %q", requestID)
		}
		logpkg.FromContext(r.Context()).Info("handler log")
		w.WriteHeader(http.StatusAccepted)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/echo", nil)
	request.Header.Set("X-Request-ID", "client-123")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", response.Code)
	}
	if response.Header().Get("X-Request-ID") != "client-123" {
		t.Fatalf("expected preserved request ID header, got %q", response.Header().Get("X-Request-ID"))
	}
	if !strings.Contains(buffer.String(), `"request_id":"client-123"`) {
		t.Fatalf("expected supplied request ID in log output, got %q", buffer.String())
	}
}

func TestLoggingWritesErrorEntryForServerErrors(t *testing.T) {
	var buffer bytes.Buffer
	baseLogger := slog.New(slog.NewJSONHandler(&buffer, nil))

	handler := Logging(baseLogger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	request := httptest.NewRequest(http.MethodGet, "/boom", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	output := buffer.String()
	if !strings.Contains(output, `"level":"ERROR"`) {
		t.Fatalf("expected error log entry, got %q", output)
	}
	if !strings.Contains(output, `"stack":"`) {
		t.Fatalf("expected stack context for 5xx, got %q", output)
	}
}
