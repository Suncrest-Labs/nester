package scheduler

import (
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/protocoltvl"
)

func tvlSeries(values ...float64) []protocoltvl.Snapshot {
	out := make([]protocoltvl.Snapshot, len(values))
	base := time.Now().Add(-time.Duration(len(values)) * time.Hour)
	for i, v := range values {
		out[i] = protocoltvl.Snapshot{ProtocolSlug: "test", TVLUSD: v, SnapshottedAt: base.Add(time.Duration(i) * time.Hour)}
	}
	return out
}

func apySeries(values ...float64) []apysnapshot.APYSnapshot {
	out := make([]apysnapshot.APYSnapshot, len(values))
	base := time.Now().Add(-time.Duration(len(values)) * time.Hour)
	for i, v := range values {
		out[i] = apysnapshot.APYSnapshot{ProtocolSlug: "test", APY: decimal.NewFromFloat(v), CapturedAt: base.Add(time.Duration(i) * time.Hour)}
	}
	return out
}

func TestScore_DeterioratingSourcePatternScoresHigh(t *testing.T) {
	// TVL outflow + APY spike + widening reported-vs-derived gap, exactly
	// the pattern the issue describes: "TVL down 40% in 6h, APY spiked 3x,
	// derived-vs-reported APY gap widening."
	tvl := tvlSeries(1_000_000, 950_000, 850_000, 700_000, 600_000, 600_000)
	apy := apySeries(5.0, 5.1, 5.0, 4.9, 5.0, 16.0) // stable ~5%, then spikes to 16%

	ind := ComputeIndicators(tvl, apy)
	assessment := Score("deteriorating-protocol", ind)

	if assessment.Level != deterioration.LevelSevere && assessment.Level != deterioration.LevelModerate {
		t.Fatalf("expected at least moderate deterioration for this pattern, got level=%s probability=%.2f", assessment.Level, assessment.Probability)
	}
	if assessment.Probability < ThresholdModerate {
		t.Errorf("expected probability >= %.2f, got %.2f", ThresholdModerate, assessment.Probability)
	}
	if assessment.Explanation == "" {
		t.Error("expected a non-empty explanation")
	}
}

func TestScore_HealthySourceScoresLow(t *testing.T) {
	tvl := tvlSeries(1_000_000, 1_010_000, 1_005_000, 1_020_000, 1_015_000, 1_030_000)
	apy := apySeries(5.0, 5.1, 4.9, 5.0, 5.05, 4.95)

	ind := ComputeIndicators(tvl, apy)
	assessment := Score("healthy-protocol", ind)

	if assessment.Level != deterioration.LevelNone {
		t.Fatalf("expected level=none for a healthy series, got level=%s probability=%.2f explanation=%q",
			assessment.Level, assessment.Probability, assessment.Explanation)
	}
	if assessment.Probability >= ThresholdMild {
		t.Errorf("expected probability below mild threshold (%.2f), got %.2f", ThresholdMild, assessment.Probability)
	}
}

func TestScore_IsDeterministic(t *testing.T) {
	tvl := tvlSeries(1_000_000, 900_000, 800_000)
	apy := apySeries(5.0, 5.5, 6.0)
	ind := ComputeIndicators(tvl, apy)

	a1 := Score("p", ind)
	a2 := Score("p", ind)
	if a1.Probability != a2.Probability || a1.Level != a2.Level {
		t.Fatalf("expected deterministic output for identical inputs, got %+v vs %+v", a1, a2)
	}
}

func TestScore_ThinSampleCappedBelowSevere(t *testing.T) {
	// A single dramatic reading with almost no history should not alone
	// justify the severe (automatic-action) tier.
	tvl := tvlSeries(1_000_000, 100_000) // 90% "outflow" from just 2 points

	ind := ComputeIndicators(tvl, nil)
	if ind.SampleCount >= minSampleCountForConfidence {
		t.Fatalf("test setup error: expected a thin sample, got SampleCount=%d", ind.SampleCount)
	}
	assessment := Score("thin-sample-protocol", ind)
	if assessment.Level == deterioration.LevelSevere {
		t.Errorf("expected a thin sample to be capped below severe, got level=%s probability=%.2f", assessment.Level, assessment.Probability)
	}
}

func TestScore_GraduatedLevelsMapMonotonically(t *testing.T) {
	// Increasingly severe outflow should never produce a LOWER level.
	prevRank := -1
	rank := map[deterioration.Level]int{
		deterioration.LevelNone: 0, deterioration.LevelMild: 1, deterioration.LevelModerate: 2, deterioration.LevelSevere: 3,
	}
	for _, outflowPct := range []float64{0, 10, 20, 30, 40, 50, 60, 80} {
		ind := deterioration.Indicators{TVLOutflowVelocityPct: outflowPct, SampleCount: 10}
		assessment := Score("p", ind)
		r := rank[assessment.Level]
		if r < prevRank {
			t.Fatalf("level regressed as outflow increased: outflow=%.0f%% level=%s (rank %d < previous %d)", outflowPct, assessment.Level, r, prevRank)
		}
		prevRank = r
	}
}

func TestComputeIndicators_TVLOutflowVelocity(t *testing.T) {
	tvl := tvlSeries(1_000_000, 600_000) // 40% decline
	ind := ComputeIndicators(tvl, nil)
	if math.Abs(ind.TVLOutflowVelocityPct-40) > 0.01 {
		t.Errorf("expected ~40%% outflow velocity, got %.2f", ind.TVLOutflowVelocityPct)
	}
}

func TestComputeIndicators_TVLInflowClampsToZeroNotNegative(t *testing.T) {
	tvl := tvlSeries(1_000_000, 1_500_000) // 50% inflow
	ind := ComputeIndicators(tvl, nil)
	if ind.TVLOutflowVelocityPct >= 0 {
		t.Errorf("expected a negative raw outflow value for inflow (normalizeOutflow clamps it, not the raw indicator), got %.2f", ind.TVLOutflowVelocityPct)
	}
	nOutflow := normalizeOutflow(ind.TVLOutflowVelocityPct)
	if nOutflow != 0 {
		t.Errorf("expected normalizeOutflow to clamp inflow to 0, got %.2f", nOutflow)
	}
}

func TestComputeIndicators_InsufficientDataReturnsZeroNotError(t *testing.T) {
	ind := ComputeIndicators(nil, nil)
	if ind.TVLOutflowVelocityPct != 0 || ind.APYAbnormalityZScore != 0 || ind.ReportedVsDerivedGapPct != 0 || ind.PriceInstability != 0 {
		t.Errorf("expected all-zero indicators for no data, got %+v", ind)
	}
}

func TestComputeIndicators_APYAbnormalityZScore_DetectsSpike(t *testing.T) {
	apy := apySeries(5.0, 5.1, 4.9, 5.0, 5.05, 20.0) // stable then a sharp spike
	ind := ComputeIndicators(nil, apy)
	if ind.APYAbnormalityZScore < 1.5 {
		t.Errorf("expected a high z-score for a sharp APY spike, got %.2f", ind.APYAbnormalityZScore)
	}
}

func TestComputeIndicators_ReportedVsDerivedGap_WideningDetected(t *testing.T) {
	apy := apySeries(5.0, 5.0, 5.0, 5.0, 5.0, 12.0) // stable baseline, then a widening gap
	ind := ComputeIndicators(nil, apy)
	if ind.ReportedVsDerivedGapPct < 50 {
		t.Errorf("expected a large reported-vs-derived gap for a 5%%->12%% jump, got %.2f%%", ind.ReportedVsDerivedGapPct)
	}
}

func TestExplain_MentionsOnlySignificantDrivers(t *testing.T) {
	ind := deterioration.Indicators{TVLOutflowVelocityPct: 45, APYAbnormalityZScore: 0.2, ReportedVsDerivedGapPct: 2, PriceInstability: 0.02, SampleCount: 10}
	assessment := Score("x", ind)
	if !containsSubstring(assessment.Explanation, "TVL down") {
		t.Errorf("expected explanation to mention TVL outflow, got %q", assessment.Explanation)
	}
	if containsSubstring(assessment.Explanation, "z-score") {
		t.Errorf("expected explanation to NOT mention APY z-score (below significance threshold), got %q", assessment.Explanation)
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
