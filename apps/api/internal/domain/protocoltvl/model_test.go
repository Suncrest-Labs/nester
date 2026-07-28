package protocoltvl

import (
	"testing"
	"time"
)

func TestTVLDropThreshold(t *testing.T) {
	if TVLDropThreshold != 20.0 {
		t.Fatalf("expected TVLDropThreshold = 20.0, got %f", TVLDropThreshold)
	}
}

func TestAlertCooldown(t *testing.T) {
	expected := 12 * time.Hour
	if AlertCooldown != expected {
		t.Fatalf("expected AlertCooldown = %v, got %v", expected, AlertCooldown)
	}
}

func TestComputeTVLDelta(t *testing.T) {
	tests := []struct {
		name         string
		current      float64
		prior        float64
		wantPct      float64
		wantDrop     bool
		wantSignif   bool
	}{
		{
			name:       "25% drop triggers alert",
			current:    75.0,
			prior:      100.0,
			wantPct:    -25.0,
			wantDrop:   true,
			wantSignif: true,
		},
		{
			name:       "exactly 20% drop triggers alert",
			current:    80.0,
			prior:      100.0,
			wantPct:    -20.0,
			wantDrop:   true,
			wantSignif: true,
		},
		{
			name:       "19% drop does not trigger",
			current:    81.0,
			prior:      100.0,
			wantPct:    -19.0,
			wantDrop:   true,
			wantSignif: false,
		},
		{
			name:       "10% increase no alert",
			current:    110.0,
			prior:      100.0,
			wantPct:    10.0,
			wantDrop:   false,
			wantSignif: false,
		},
		{
			name:       "no change",
			current:    100.0,
			prior:      100.0,
			wantPct:    0.0,
			wantDrop:   false,
			wantSignif: false,
		},
		{
			name:       "small positive change",
			current:    101.0,
			prior:      100.0,
			wantPct:    1.0,
			wantDrop:   false,
			wantSignif: false,
		},
		{
			name:       "small negative change",
			current:    99.0,
			prior:      100.0,
			wantPct:    -1.0,
			wantDrop:   true,
			wantSignif: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct := computePctChange(tt.current, tt.prior)
			if !floatNear(pct, tt.wantPct, 0.01) {
				t.Errorf("computePctChange(%v, %v) = %v, want %v", tt.current, tt.prior, pct, tt.wantPct)
			}

			isDrop := pct < 0
			if isDrop != tt.wantDrop {
				t.Errorf("isDrop = %v, want %v", isDrop, tt.wantDrop)
			}

			isSignificant := pct <= -TVLDropThreshold
			if isSignificant != tt.wantSignif {
				t.Errorf("isSignificant = %v, want %v (threshold=%v)", isSignificant, tt.wantSignif, TVLDropThreshold)
			}
		})
	}
}

func TestZeroTVLEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		current float64
		prior   float64
		wantPct float64
	}{
		{
			name:    "prior is zero - avoid division by zero",
			current: 100.0,
			prior:   0.0,
			wantPct: 0.0,
		},
		{
			name:    "current is zero from positive prior",
			current: 0.0,
			prior:   100.0,
			wantPct: -100.0,
		},
		{
			name:    "both zero",
			current: 0.0,
			prior:   0.0,
			wantPct: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct := computePctChange(tt.current, tt.prior)
			if !floatNear(pct, tt.wantPct, 0.01) {
				t.Errorf("computePctChange(%v, %v) = %v, want %v", tt.current, tt.prior, pct, tt.wantPct)
			}
		})
	}
}

func TestNegativeTVLEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		current float64
		prior   float64
		wantPct float64
	}{
		{
			name:    "negative current treated as zero for computation",
			current: -50.0,
			prior:   100.0,
			wantPct: -100.0,
		},
		{
			name:    "negative prior - unusual but handled",
			current: 100.0,
			prior:   -50.0,
			wantPct: 0.0,
		},
		{
			name:    "both negative",
			current: -100.0,
			prior:   -50.0,
			wantPct: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pct := computePctChange(tt.current, tt.prior)
			if !floatNear(pct, tt.wantPct, 0.01) {
				t.Errorf("computePctChange(%v, %v) = %v, want %v", tt.current, tt.prior, pct, tt.wantPct)
			}
		})
	}
}

// computePctChange calculates percentage change from prior to current.
// Returns 0 if prior is zero or negative to avoid division by zero.
// Negative current values are treated as zero.
func computePctChange(current, prior float64) float64 {
	if prior <= 0 {
		return 0.0
	}
	if current < 0 {
		current = 0
	}
	return ((current - prior) / prior) * 100.0
}

// floatNear checks if two floats are within tolerance of each other.
func floatNear(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

