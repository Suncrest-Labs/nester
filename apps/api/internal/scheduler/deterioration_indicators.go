package scheduler

import (
	"math"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/protocoltvl"
)

// derivedAPYBaselineWindow is how far back the "expected/derived" baseline
// looks when computing the reported-vs-derived APY gap — a trailing average
// distinct from the single latest reading being compared against it.
const derivedAPYBaselineWindow = 3

// ComputeIndicators derives deterioration.Indicators for one protocol from
// its TVL and APY snapshot history over the window ending at "now". Pure
// and deterministic (#857: "keep the indicator computation ... as pure,
// testable units") — no I/O, callers fetch the snapshots first.
func ComputeIndicators(tvlSnapshots []protocoltvl.Snapshot, apySnapshots []apysnapshot.APYSnapshot) deterioration.Indicators {
	ind := deterioration.Indicators{
		SampleCount: len(tvlSnapshots) + len(apySnapshots),
	}
	ind.TVLOutflowVelocityPct = tvlOutflowVelocity(tvlSnapshots)
	ind.PriceInstability = tvlCoefficientOfVariation(tvlSnapshots)

	apyValues := make([]float64, 0, len(apySnapshots))
	for _, s := range apySnapshots {
		v, _ := s.APY.Float64()
		apyValues = append(apyValues, v)
	}
	ind.APYAbnormalityZScore = latestZScore(apyValues)
	ind.ReportedVsDerivedGapPct = reportedVsDerivedGapPct(apyValues)

	return ind
}

// tvlOutflowVelocity is the percentage decline from the window's first
// snapshot to its last (positive = outflow). Needs at least 2 points;
// returns 0 otherwise (no signal, not a false "healthy").
func tvlOutflowVelocity(snaps []protocoltvl.Snapshot) float64 {
	if len(snaps) < 2 {
		return 0
	}
	first := snaps[0].TVLUSD
	last := snaps[len(snaps)-1].TVLUSD
	if first <= 0 {
		return 0
	}
	return (first - last) / first * 100
}

// tvlCoefficientOfVariation is stddev/mean of the TVL series — a
// scale-independent volatility measure used as the price-instability proxy
// (see Indicators.PriceInstability's doc comment for why TVL volatility
// stands in for underlying/LP token price instability here).
func tvlCoefficientOfVariation(snaps []protocoltvl.Snapshot) float64 {
	if len(snaps) < 2 {
		return 0
	}
	values := make([]float64, len(snaps))
	for i, s := range snaps {
		values[i] = s.TVLUSD
	}
	mean, stddev := meanStdDev(values)
	if mean <= 0 {
		return 0
	}
	return stddev / mean
}

// latestZScore is how many standard deviations the last value in the
// series is from the mean of the whole series (including itself — with a
// short window, excluding it would make the "baseline" degenerate for the
// small samples this runs on in practice). Needs at least 3 points for a
// meaningful stddev; returns 0 otherwise.
func latestZScore(values []float64) float64 {
	if len(values) < 3 {
		return 0
	}
	mean, stddev := meanStdDev(values)
	if stddev == 0 {
		return 0
	}
	return (values[len(values)-1] - mean) / stddev
}

// reportedVsDerivedGapPct compares the latest reported APY against a
// trailing average of the preceding derivedAPYBaselineWindow readings (the
// "derived/expected" baseline — see Indicators.ReportedVsDerivedGapPct's
// doc comment on why this proxy is used instead of an on-chain-derived
// accrual figure). Returns the absolute gap as a percentage of the
// baseline; needs enough points for both a baseline and a latest reading.
func reportedVsDerivedGapPct(values []float64) float64 {
	if len(values) < derivedAPYBaselineWindow+1 {
		return 0
	}
	latest := values[len(values)-1]
	baselineSlice := values[len(values)-1-derivedAPYBaselineWindow : len(values)-1]
	baseline, _ := meanStdDev(baselineSlice)
	if baseline == 0 {
		return 0
	}
	return math.Abs(latest-baseline) / baseline * 100
}

func meanStdDev(values []float64) (mean, stddev float64) {
	if len(values) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean = sum / float64(len(values))

	variance := 0.0
	for _, v := range values {
		d := v - mean
		variance += d * d
	}
	variance /= float64(len(values))
	return mean, math.Sqrt(variance)
}

// windowStart is a small helper so callers building the fetch window (e.g.
// "the last 6 hours of TVL snapshots") have one place to compute it.
func windowStart(now time.Time, window time.Duration) time.Time {
	return now.Add(-window)
}
