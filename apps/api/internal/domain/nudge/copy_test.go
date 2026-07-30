package nudge

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestTemplateCopyGenerator_RendersExactFacts(t *testing.T) {
	facts := Facts{
		GoalName:      "Rent Buffer",
		TargetAmount:  decimal.NewFromInt(5000),
		CurrentAmount: decimal.NewFromInt(4200),
		Currency:      "NGN",
		Deadline:      time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC),
	}

	gen := TemplateCopyGenerator{}
	_, body, err := gen.Generate(NudgeTypeDeadlineReminder, facts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(body, "5000") {
		t.Fatalf("expected body to quote the exact target amount 5000, got %q", body)
	}
	if !strings.Contains(body, "NGN") {
		t.Fatalf("expected body to quote the exact currency NGN, got %q", body)
	}
	if !strings.Contains(body, "Rent Buffer") {
		t.Fatalf("expected body to quote the exact goal name, got %q", body)
	}
	// Must never invent a figure that isn't one of the facts.
	if strings.Contains(body, "9999") {
		t.Fatalf("body contains a fabricated figure not present in facts: %q", body)
	}
}

func TestTemplateCopyGenerator_UnknownTypeFallsBackSafely(t *testing.T) {
	gen := TemplateCopyGenerator{}
	title, body, err := gen.Generate(NudgeType("not_a_real_type"), Facts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title == "" || body == "" {
		t.Fatalf("expected a non-empty fallback title/body for an unrecognized nudge type")
	}
}
