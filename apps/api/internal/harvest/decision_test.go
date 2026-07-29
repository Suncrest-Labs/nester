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

func TestFrequencyDuration(t *testing.T) {
	tests := []struct {
		frequency string
		want      time.Duration
	}{
		{"daily", 24 * time.Hour},
		{"weekly", 7 * 24 * time.Hour},
		{"", 24 * time.Hour},
		{"monthly", 24 * time.Hour},
	}
	for _, tt := range tests {
		if got := FrequencyDuration(tt.frequency); got != tt.want {
			t.Errorf("FrequencyDuration(%q) = %v, want %v", tt.frequency, got, tt.want)
		}
	}
}

func TestDueForHarvest(t *testing.T) {
	now := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	if !DueForHarvest(now, "daily", nil) {
		t.Error("never-harvested vault should always be due")
	}

	almostADayAgo := now.Add(-23 * time.Hour)
	if DueForHarvest(now, "daily", &almostADayAgo) {
		t.Error("daily vault harvested 23h ago should not be due")
	}

	overADayAgo := now.Add(-25 * time.Hour)
	if !DueForHarvest(now, "daily", &overADayAgo) {
		t.Error("daily vault harvested 25h ago should be due")
	}

	sixDaysAgo := now.Add(-6 * 24 * time.Hour)
	if DueForHarvest(now, "weekly", &sixDaysAgo) {
		t.Error("weekly vault harvested 6 days ago should not be due")
	}

	eightDaysAgo := now.Add(-8 * 24 * time.Hour)
	if !DueForHarvest(now, "weekly", &eightDaysAgo) {
		t.Error("weekly vault harvested 8 days ago should be due")
	}
}
