package stellar

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// A chain submission is the far end of a request that started at the HTTP
// edge, so its log lines have to carry the same correlation id (#1111).
// Without this the one thing an operator cannot do is the thing the id exists
// for: join an API log line to the chain submission it caused.
func TestLogSubmissionCarriesTheRequestCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	requestLogger := slog.New(slog.NewJSONHandler(&buf, nil)).With("request_id", "req-abc-123")

	ctx := logpkg.WithRequestID(context.Background(), "req-abc-123")
	ctx = logpkg.WithLogger(ctx, requestLogger)

	// The injected logger writes somewhere else entirely, so a correlation id
	// in the output can only have come from the context.
	var fallback bytes.Buffer
	c := &ContractInvoker{logger: slog.New(slog.NewJSONHandler(&fallback, nil))}

	c.logSubmission(ctx, "submission response lost", SubmissionIntent{
		ID:              "sub-1",
		TransactionHash: "tx-1",
		State:           SubmissionPending,
	})

	if fallback.Len() != 0 {
		t.Errorf("fell back to the injected logger; request-scoped logger was ignored: %s", fallback.String())
	}

	var entry map[string]any
	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("no log line emitted for the submission")
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, line)
	}
	if got := entry["request_id"]; got != "req-abc-123" {
		t.Errorf("request_id = %v, want req-abc-123 — the chain submission is not correlated to its request", got)
	}
	if got := entry["submission_id"]; got != "sub-1" {
		t.Errorf("submission_id = %v, want sub-1", got)
	}
}

// Submissions that originate outside a request — the reconciler, for one —
// still have to be logged, so an absent correlation id must not silence them.
func TestLogSubmissionFallsBackWithoutARequestContext(t *testing.T) {
	var buf bytes.Buffer
	c := &ContractInvoker{logger: slog.New(slog.NewJSONHandler(&buf, nil))}

	c.logSubmission(context.Background(), "submission response lost", SubmissionIntent{
		ID:              "sub-2",
		TransactionHash: "tx-2",
		State:           SubmissionPending,
	})

	if buf.Len() == 0 {
		t.Fatal("no log line emitted without a request context; reconciler submissions would go unlogged")
	}
	if !strings.Contains(buf.String(), "sub-2") {
		t.Errorf("log line missing the submission id: %s", buf.String())
	}
}
