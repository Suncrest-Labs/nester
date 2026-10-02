package protocoltvl

import (
	"testing"
	"time"
)

func TestDetectAnomaly(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	recent := &Snapshot{TVLUSD: 100, SnapshottedAt: now.Add(-30 * time.Minute)}

	tests := []struct {
		name      string
		prior     *Snapshot
		current   float64
		threshold float64
		want      bool
		wantDrop  float64
	}{
		{"drop above threshold", recent, 80, 10, true, 20},
		{"drop exactly at threshold", recent, 90, 10, true, 10},
		{"drop below threshold", recent, 95, 10, false, 0},
		{"increase", recent, 120, 10, false, 0},
		{"zero threshold uses default", recent, 85, 0, true, 15},
		{"zero threshold default not met", recent, 95, 0, false, 0},
		{"full collapse", recent, 0, 10, true, 100},
		{"negative current treated as zero", recent, -5, 10, true, 100},
		{"no prior snapshot", nil, 50, 10, false, 0},
		{"zero prior TVL", &Snapshot{TVLUSD: 0, SnapshottedAt: now}, 0, 10, false, 0},
		{"stale prior snapshot", &Snapshot{TVLUSD: 100, SnapshottedAt: now.Add(-3 * time.Hour)}, 10, 10, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectAnomaly("aave", tt.prior, tt.current, tt.threshold, now)
			if (got != nil) != tt.want {
				t.Fatalf("DetectAnomaly anomaly = %v, want anomaly = %v", got, tt.want)
			}
			if got == nil {
				return
			}
			if !floatNear(got.DropPct, tt.wantDrop, 0.01) {
				t.Errorf("DropPct = %v, want %v", got.DropPct, tt.wantDrop)
			}
			if got.ProtocolSlug != "aave" || !got.DetectedAt.Equal(now) {
				t.Errorf("unexpected anomaly metadata: %+v", got)
			}
			if got.Explanation() == "" {
				t.Error("Explanation must not be empty")
			}
		})
	}
}
