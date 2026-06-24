package service

import (
	"testing"
)

func TestComputeRiskScore(t *testing.T) {
	tests := []struct {
		name        string
		tvl         float64
		apy7dSwing  float64
		rewardRatio float64
		wantMin     float64 // score must be >= wantMin
		wantMax     float64 // score must be <= wantMax
		desc        string
	}{
		{
			name:        "high TVL, stable APY, base-heavy pool",
			tvl:         5_000_000,
			apy7dSwing:  2.0,
			rewardRatio: 0.1,
			wantMin:     0.0,
			wantMax:     0.0,
			desc:        "no signals triggered → score 0",
		},
		{
			name:        "low TVL only",
			tvl:         50_000,
			apy7dSwing:  5.0,
			rewardRatio: 0.3,
			wantMin:     0.4,
			wantMax:     0.4,
			desc:        "only TVL signal → 0.4",
		},
		{
			name:        "TVL < 100k and high positive swing",
			tvl:         10_000,
			apy7dSwing:  25.0,
			rewardRatio: 0.3,
			wantMin:     0.7,
			wantMax:     0.7,
			desc:        "TVL + swing → 0.7",
		},
		{
			name:        "TVL < 100k and high negative swing",
			tvl:         10_000,
			apy7dSwing:  -25.0,
			rewardRatio: 0.3,
			wantMin:     0.7,
			wantMax:     0.7,
			desc:        "absolute value of swing counts",
		},
		{
			name:        "high reward ratio only",
			tvl:         500_000,
			apy7dSwing:  5.0,
			rewardRatio: 0.9,
			wantMin:     0.2,
			wantMax:     0.2,
			desc:        "incentive-heavy pool → 0.2",
		},
		{
			name:        "all three signals",
			tvl:         1_000,
			apy7dSwing:  30.0,
			rewardRatio: 0.95,
			wantMin:     0.89,
			wantMax:     1.0,
			desc:        "all signals sum to 0.9 (floating-point ≈ 0.89...), clamped at 1.0",
		},
		{
			name:        "TVL exactly at boundary",
			tvl:         100_000,
			apy7dSwing:  0,
			rewardRatio: 0,
			wantMin:     0.0,
			wantMax:     0.0,
			desc:        "TVL == $100k does not trigger low-TVL signal",
		},
		{
			name:        "reward ratio exactly at boundary",
			tvl:         1_000_000,
			apy7dSwing:  5.0,
			rewardRatio: 0.8,
			wantMin:     0.0,
			wantMax:     0.0,
			desc:        "rewardRatio == 0.8 does not trigger incentive signal",
		},
		{
			name:        "pools with TVL < 100k always score above 0.3",
			tvl:         99_999,
			apy7dSwing:  0,
			rewardRatio: 0,
			wantMin:     0.4,
			wantMax:     0.4,
			desc:        "acceptance criteria: pools with TVL < $100k score > 0.3",
		},
		{
			name:        "pools with reward APY > 80pct score above 0.1",
			tvl:         2_000_000,
			apy7dSwing:  0,
			rewardRatio: 0.85,
			wantMin:     0.2,
			wantMax:     0.2,
			desc:        "acceptance criteria: reward-heavy pools score >= 0.2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeRiskScore(tc.tvl, tc.apy7dSwing, tc.rewardRatio)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("%s: computeRiskScore(%v, %v, %v) = %v, want [%v, %v]",
					tc.desc, tc.tvl, tc.apy7dSwing, tc.rewardRatio, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestComputeRiskScore_AlwaysClamped(t *testing.T) {
	cases := []struct{ tvl, swing, ratio float64 }{
		{0, 100, 1.0},
		{0, 0, 0},
		{1e9, 50, 0.9},
	}
	for _, c := range cases {
		score := computeRiskScore(c.tvl, c.swing, c.ratio)
		if score < 0 || score > 1.0 {
			t.Errorf("score %v out of [0,1] for tvl=%v swing=%v ratio=%v", score, c.tvl, c.swing, c.ratio)
		}
	}
}
