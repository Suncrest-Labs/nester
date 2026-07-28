package protocoltvl

import (
	"fmt"
	"testing"
)

func TestNegativeTVLEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		current float64
		prior   float64
		want    float64
	}{
		{
			name:    "normal positive change",
			current: 150,
			prior:   100,
			want:    50,
		},
		{
			name:    "normal negative change",
			current: 80,
			prior:   100,
			want:    -20,
		},
		{
			name:    "no change",
			current: 100,
			prior:   100,
			want:    0,
		},
		{
			name:    "prior zero returns zero",
			current: 100,
			prior:   0,
			want:    0,
		},
		// Negative current is clamped to 0: (0 - 100) / 100 * 100 = -100
		{
			name:    "negative current treated as zero for computation",
			current: -50,
			prior:   100,
			want:    -100,
		},
		// Negative prior is undefined — return 0.
		{
			name:    "negative prior - unusual but handled",
			current: 100,
			prior:   -50,
			want:    0,
		},
		// Both negative: prior <= 0 short-circuits to 0.
		{
			name:    "both negative",
			current: -100,
			prior:   -50,
			want:    0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computePctChange(tc.current, tc.prior)
			if got != tc.want {
				t.Errorf(
					"computePctChange(%v, %v) = %v, want %v",
					tc.current, tc.prior, got, tc.want,
				)
			}
		})
	}
}


func TestComputePctChangeFormat(t *testing.T) {
	cases := []struct {
		current float64
		prior   float64
		want    string
	}{
		{120, 100, "+20.00"},
		{80, 100, "-20.00"},
		{100, 100, "0.00"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			pct := computePctChange(tc.current, tc.prior)
			var got string
			switch {
			case pct > 0:
				got = fmt.Sprintf("+%.2f", pct)
			case pct < 0:
				got = fmt.Sprintf("%.2f", pct)
			default:
				got = "0.00"
			}
			if got != tc.want {
				t.Errorf("formatted = %q, want %q", got, tc.want)
			}
		})
	}
}
