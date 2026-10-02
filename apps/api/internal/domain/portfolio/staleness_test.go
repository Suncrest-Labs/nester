package portfolio

import "testing"

// TestClassifyStaleness covers issue #1109's three required cases: fresh,
// stale and unknown.
func TestClassifyStaleness(t *testing.T) {
	tests := []struct {
		name    string
		sampled bool
		stale   bool
		want    Staleness
	}{
		{"never sampled is unknown regardless of stale flag", false, false, StalenessUnknown},
		{"never sampled is unknown even if stale flag happens to be true", false, true, StalenessUnknown},
		{"sampled and within budget is fresh", true, false, StalenessFresh},
		{"sampled and beyond budget is stale", true, true, StalenessStale},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyStaleness(tc.sampled, tc.stale)
			if got != tc.want {
				t.Fatalf("ClassifyStaleness(%v, %v) = %q, want %q", tc.sampled, tc.stale, got, tc.want)
			}
		})
	}
}
