package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// newHandler
// ---------------------------------------------------------------------------

func TestNewHandler_JSONProducesValidJSON(t *testing.T) {
	var buf bytes.Buffer
	handler, err := newHandler("json", &buf, &slog.HandlerOptions{})
	if err != nil {
		t.Fatalf("newHandler(json) error = %v", err)
	}

	slog.New(handler).Info("hello", "key", "value")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON output, got %q: %v", buf.String(), err)
	}
	if entry["msg"] != "hello" {
		t.Errorf("expected msg=hello, got %v", entry["msg"])
	}
	if entry["key"] != "value" {
		t.Errorf("expected key=value, got %v", entry["key"])
	}
}

func TestNewHandler_TextProducesNonJSONOutput(t *testing.T) {
	var buf bytes.Buffer
	handler, err := newHandler("text", &buf, &slog.HandlerOptions{})
	if err != nil {
		t.Fatalf("newHandler(text) error = %v", err)
	}

	slog.New(handler).Info("hello", "foo", "bar")

	output := buf.String()
	// Text format should not be parseable as a JSON object.
	var entry map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(output)), &entry) == nil {
		t.Errorf("expected non-JSON text output, but got valid JSON: %q", output)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain the log message, got %q", output)
	}
}

func TestNewHandler_CaseInsensitiveFormat(t *testing.T) {
	var buf bytes.Buffer
	for _, format := range []string{"JSON", "Json", "TEXT", "Text"} {
		_, err := newHandler(format, &buf, &slog.HandlerOptions{})
		if err != nil {
			t.Errorf("newHandler(%q) expected no error, got %v", format, err)
		}
	}
}

func TestNewHandler_UnsupportedFormatReturnsError(t *testing.T) {
	var buf bytes.Buffer
	_, err := newHandler("logfmt", &buf, &slog.HandlerOptions{})
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
	if !strings.Contains(err.Error(), "logfmt") {
		t.Errorf("expected error message to mention the bad format, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// parseLevel
// ---------------------------------------------------------------------------

func TestParseLevel_AllValidLevels(t *testing.T) {
	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
	}

	for _, tc := range cases {
		got, err := parseLevel(tc.input)
		if err != nil {
			t.Errorf("parseLevel(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseLevel_UnsupportedLevelReturnsError(t *testing.T) {
	_, err := parseLevel("verbose")
	if err == nil {
		t.Fatal("expected error for unsupported level, got nil")
	}
	if !strings.Contains(err.Error(), "verbose") {
		t.Errorf("expected error to mention the bad value, got %q", err.Error())
	}
}

func TestParseLevel_DefaultsToInfoOnError(t *testing.T) {
	got, err := parseLevel("bad")
	if err == nil {
		t.Fatal("expected error for bad level, got nil")
	}
	// parseLevel documents that it returns slog.LevelInfo on an unrecognised value.
	if got != slog.LevelInfo {
		t.Errorf("parseLevel(bad) default level = %v, want %v", got, slog.LevelInfo)
	}
}

// ---------------------------------------------------------------------------
// Log level filtering
// ---------------------------------------------------------------------------

func TestLogLevel_DebugLevelEmitsDebugMessages(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler, _ := newHandler("json", &buf, opts)
	slog.New(handler).Debug("debug message")

	if !strings.Contains(buf.String(), "debug message") {
		t.Errorf("expected DEBUG message to appear in output, got %q", buf.String())
	}
}

func TestLogLevel_InfoLevelSuppressesDebugMessages(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	handler, _ := newHandler("json", &buf, opts)
	slog.New(handler).Debug("should not appear")

	if strings.Contains(buf.String(), "should not appear") {
		t.Errorf("expected DEBUG message to be suppressed at INFO level, got %q", buf.String())
	}
}

func TestLogLevel_ErrorLevelSuppressesInfoAndWarn(t *testing.T) {
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelError}
	handler, _ := newHandler("json", &buf, opts)
	log := slog.New(handler)
	log.Info("info message")
	log.Warn("warn message")
	log.Error("error message")

	output := buf.String()
	if strings.Contains(output, "info message") {
		t.Errorf("INFO should be suppressed at ERROR level, got %q", output)
	}
	if strings.Contains(output, "warn message") {
		t.Errorf("WARN should be suppressed at ERROR level, got %q", output)
	}
	if !strings.Contains(output, "error message") {
		t.Errorf("ERROR message should appear at ERROR level, got %q", output)
	}
}

// ---------------------------------------------------------------------------
// WithLogger / FromContext
// ---------------------------------------------------------------------------

func TestWithLoggerAndFromContext_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	handler, _ := newHandler("json", &buf, nil)
	original := slog.New(handler)

	ctx := WithLogger(context.Background(), original)
	retrieved := FromContext(ctx)

	retrieved.Info("from context")
	if !strings.Contains(buf.String(), "from context") {
		t.Errorf("expected log from retrieved logger to appear in buffer, got %q", buf.String())
	}
}

func TestFromContext_ReturnsSlogDefaultWhenNoLoggerInContext(t *testing.T) {
	retrieved := FromContext(context.Background())
	if retrieved == nil {
		t.Fatal("FromContext(empty context) returned nil, want non-nil default logger")
	}
	// The default logger should still be callable without panicking.
	retrieved.Info("safe default call")
}

func TestFromContext_NilContextValueDoesNotPanic(t *testing.T) {
	// Store a nil pointer via a typed nil.
	key := loggerContextKey
	ctx := context.WithValue(context.Background(), key, (*slog.Logger)(nil))

	// FromContext must fall back to slog.Default() gracefully.
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext with nil logger in context returned nil, want slog.Default()")
	}
}

// ---------------------------------------------------------------------------
// WithRequestID / RequestIDFromContext
// ---------------------------------------------------------------------------

func TestWithRequestIDAndRequestIDFromContext_RoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-abc-123")
	got := RequestIDFromContext(ctx)
	if got != "req-abc-123" {
		t.Errorf("RequestIDFromContext = %q, want %q", got, "req-abc-123")
	}
}

func TestRequestIDFromContext_EmptyStringWhenMissing(t *testing.T) {
	got := RequestIDFromContext(context.Background())
	if got != "" {
		t.Errorf("RequestIDFromContext(empty context) = %q, want empty string", got)
	}
}

func TestWithRequestID_OverwritesPreviousID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "first")
	ctx = WithRequestID(ctx, "second")
	got := RequestIDFromContext(ctx)
	if got != "second" {
		t.Errorf("expected overwritten request ID %q, got %q", "second", got)
	}
}

// ---------------------------------------------------------------------------
// Sensitive field filtering
// ---------------------------------------------------------------------------

func TestLogger_AuthorizationHeaderIsNotLogged(t *testing.T) {
	var buf bytes.Buffer
	handler, _ := newHandler("json", &buf, nil)
	log := slog.New(handler)

	// Simulate what a handler might log — only safe fields should appear.
	// The logger itself does not strip fields; callers must not log sensitive data.
	// This test documents and enforces the contract: nothing in the logger package
	// should ever log Authorization or password fields automatically.
	log.Info("request received", "method", "POST", "path", "/api/v1/vaults")

	output := buf.String()
	if strings.Contains(strings.ToLower(output), "authorization") {
		t.Errorf("logger must not emit Authorization fields automatically, got %q", output)
	}
	if strings.Contains(strings.ToLower(output), "password") {
		t.Errorf("logger must not emit password fields automatically, got %q", output)
	}
}

// ---------------------------------------------------------------------------
// logger.With — attaches key-value pairs to subsequent entries
// ---------------------------------------------------------------------------

func TestLoggerWith_AttachesFieldsToSubsequentEntries(t *testing.T) {
	var buf bytes.Buffer
	handler, _ := newHandler("json", &buf, nil)
	log := slog.New(handler).With("service", "nester-api", "env", "test")

	log.Info("boot")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON, got %q", buf.String())
	}
	if entry["service"] != "nester-api" {
		t.Errorf("expected service=nester-api, got %v", entry["service"])
	}
	if entry["env"] != "test" {
		t.Errorf("expected env=test, got %v", entry["env"])
	}
}

func TestLoggerWith_DoesNotMutateParentLogger(t *testing.T) {
	var parentBuf, childBuf bytes.Buffer

	parentHandler, _ := newHandler("json", &parentBuf, nil)
	parent := slog.New(parentHandler)

	childHandler, _ := newHandler("json", &childBuf, nil)
	child := slog.New(childHandler).With("child_key", "child_value")

	parent.Info("parent message")
	child.Info("child message")

	parentOutput := parentBuf.String()
	if strings.Contains(parentOutput, "child_key") {
		t.Errorf("parent logger should not contain child_key, got %q", parentOutput)
	}
}

// ---------------------------------------------------------------------------
// JSON structured fields
// ---------------------------------------------------------------------------

func TestJSONOutput_ContainsExpectedLevelAndTimestampFields(t *testing.T) {
	var buf bytes.Buffer
	handler, _ := newHandler("json", &buf, nil)
	slog.New(handler).Warn("check fields")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("expected valid JSON, got %q", buf.String())
	}
	if _, ok := entry["time"]; !ok {
		t.Errorf("expected 'time' field in JSON output, got %v", entry)
	}
	if entry["level"] != "WARN" {
		t.Errorf("expected level=WARN, got %v", entry["level"])
	}
	if entry["msg"] != "check fields" {
		t.Errorf("expected msg='check fields', got %v", entry["msg"])
	}
}
