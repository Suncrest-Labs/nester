package harvest

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestEvaluate_Gating(t *testing.T) {
	tests := []struct {
		name       string
		accrued    string
		gas        string
		margin     string
		wantHarv   bool
		wantReason Reason
	}{
		{"clears threshold", "10", "2", "1", true, ReasonHarvest},
		{"exactly at threshold does not harvest", "3", "2", "1", false, ReasonBelowThreshold},
		{"just below threshold", "2.99", "2", "1", false, ReasonBelowThreshold},
		{"just above threshold", "3.01", "2", "1", true, ReasonHarvest},
		{"zero yield", "0", "2", "1", false, ReasonNoYield},
		{"negative yield", "-5", "2", "1", false, ReasonNoYield},
		{"negative gas floored to zero", "0.5", "-4", "0", true, ReasonHarvest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(GatingInput{AccruedYield: d(tt.accrued), GasFee: d(tt.gas), Margin: d(tt.margin)})
			if got.Harvest != tt.wantHarv {
				t.Fatalf("harvest = %v, want %v (net %s)", got.Harvest, tt.wantHarv, got.NetGain)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestEstimateNextHarvest(t *testing.T) {
	// Need 10 more units at 5/hour => 2 hours.
	got, ok := EstimateNextHarvest(d("0"), d("10"), d("5"))
	if !ok {
		t.Fatal("expected a defined estimate")
	}
	if got != 2*time.Hour {
		t.Fatalf("estimate = %v, want 2h", got)
	}

	// Already at/over threshold => zero, defined.
	if got, ok := EstimateNextHarvest(d("12"), d("10"), d("5")); !ok || got != 0 {
		t.Fatalf("already-met estimate = %v ok=%v, want 0/true", got, ok)
	}

	// Non-positive rate => undefined.
	if _, ok := EstimateNextHarvest(d("0"), d("10"), d("0")); ok {
		t.Fatal("expected undefined estimate for zero rate")
	}
}
