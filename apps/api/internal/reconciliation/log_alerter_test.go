package reconciliation

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// levelCaptureHandler records message, level, and attrs so the test can
// assert both the severity mapping and the presence of the join-key fields.
type levelCaptureHandler struct {
	records *[]string
}

func (h levelCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h levelCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Level.String() + " " + r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	*h.records = append(*h.records, sb.String())
	return nil
}
func (h levelCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h levelCaptureHandler) WithGroup(string) slog.Handler      { return h }

func TestLogAlerterLevelsAndFields(t *testing.T) {
	finding := mismatchFinding(t, "250", "100")
	finding.Details = map[string]string{"contract_address": "CVAULT1"}
	finding.ObservedAt = time.Now()

	var logs []string
	alerter := NewLogAlerter(slog.New(levelCaptureHandler{records: &logs}))

	if err := alerter.CriticalFinding(context.Background(), finding); err != nil {
		t.Fatalf("CriticalFinding() error = %v", err)
	}
	if err := alerter.WarningFinding(context.Background(), finding); err != nil {
		t.Fatalf("WarningFinding() error = %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("expected 2 log records, got %d: %v", len(logs), logs)
	}
	if !strings.HasPrefix(logs[0], "ERROR ") {
		t.Fatalf("critical finding logged at %q, want ERROR", logs[0])
	}
	if !strings.HasPrefix(logs[1], "WARN ") {
		t.Fatalf("warning finding logged at %q, want WARN", logs[1])
	}
	for _, want := range []string{
		"type=mismatch",
		"entity_type=vault",
		"entity_id=vault-1",
		"recorded_value=250",
		"on_chain_value=100",
		"difference=150",
		"detail_contract_address=CVAULT1",
	} {
		if !strings.Contains(logs[0], want) {
			t.Fatalf("critical log missing %q: %q", want, logs[0])
		}
	}
}
