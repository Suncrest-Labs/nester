package service

import "testing"

func TestComputeRiskScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		tvlUsd       float64
		apy7dSwing   float64
		rewardRatio  float64
		wantMin      float64 // score must be >= wantMin
		wantMax      float64 // score must be <= wantMax
		wantExact    *float64 // when non-nil, score must equal this exactly
	}{
		{
			name:        "zero inputs returns 0.0",
			tvlUsd:      500_000,
			apy7dSwing:  0,
			rewardRatio: 0,
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			name:        "tvl below 100k adds 0.4",
			tvlUsd:      50_000,
			apy7dSwing:  0,
			rewardRatio: 0,
			wantMin:     0.4,
			wantMax:     0.4,
		},
		{
			name:        "high positive apy swing adds 0.3",
			tvlUsd:      500_000,
			apy7dSwing:  25.0,
			rewardRatio: 0,
			wantMin:     0.3,
			wantMax:     0.3,
		},
		{
			name:        "high negative apy swing also adds 0.3",
			tvlUsd:      500_000,
			apy7dSwing:  -21.0,
			rewardRatio: 0,
			wantMin:     0.3,
			wantMax:     0.3,
		},
		{
			name:        "reward ratio above 0.8 adds 0.2",
			tvlUsd:      500_000,
			apy7dSwing:  0,
			rewardRatio: 0.9,
			wantMin:     0.2,
			wantMax:     0.2,
		},
		{
			name:        "all signals fire, score is 0.9 (0.4+0.3+0.2, below clamp)",
			tvlUsd:      10_000,
			apy7dSwing:  50.0,
			rewardRatio: 0.95,
			wantMin:     0.9,
			wantMax:     0.9,
		},
		{
			name:        "score never exceeds 1.0 with extreme inputs",
			tvlUsd:      0,
			apy7dSwing:  1000.0,
			rewardRatio: 1.0,
			wantMin:     0.0,
			wantMax:     1.0,
		},
		{
			name:        "exactly at tvl boundary (100_000) does not trigger penalty",
			tvlUsd:      100_000,
			apy7dSwing:  0,
			rewardRatio: 0,
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			name:        "exactly at apy swing boundary (20) does not trigger penalty",
			tvlUsd:      500_000,
			apy7dSwing:  20.0,
			rewardRatio: 0,
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			name:        "exactly at reward ratio boundary (0.8) does not trigger penalty",
			tvlUsd:      500_000,
			apy7dSwing:  0,
			rewardRatio: 0.8,
			wantMin:     0.0,
			wantMax:     0.0,
		},
		{
			name:        "tvl and apy swing both fire",
			tvlUsd:      99_999,
			apy7dSwing:  21.0,
			rewardRatio: 0,
			wantMin:     0.7,
			wantMax:     0.7,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ComputeRiskScore(tc.tvlUsd, tc.apy7dSwing, tc.rewardRatio)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("ComputeRiskScore(%v, %v, %v) = %v; want in [%v, %v]",
					tc.tvlUsd, tc.apy7dSwing, tc.rewardRatio, got, tc.wantMin, tc.wantMax)
			}
			if got < 0.0 || got > 1.0 {
				t.Errorf("ComputeRiskScore result %v outside [0, 1]", got)
			}
		})
	}
}
