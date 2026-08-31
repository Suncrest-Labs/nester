package scheduler

import (
	"fmt"
	"math"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/deterioration"
)

// Score weights and normalization caps. A deliberately simple, fully
// transparent weighted-logistic model rather than an opaque one (#857:
// "validate calibration and start with a well-understood model over an
// opaque one") — every term here is directly inspectable and each
// indicator's contribution can be read straight off the weights below,
// which is also what makes the explanation text possible: the model's
// output IS the sum of named, interpretable terms.
const (
	// weightTVLOutflow etc. are applied to each indicator after it's
	// normalized to roughly a [0,1] contribution (see normalize* below),
	// then summed and passed through a logistic squash to produce a
	// probability. Weights reflect the issue's framing of TVL outflow as
	// the single strongest signal ("smart money leaving early"), with APY
	// abnormality and the reported/derived gap as strong corroborating
	// signals, and price instability as a weaker, noisier one.
	weightTVLOutflow     = 2.2
	weightAPYAbnormality = 1.6
	weightAPYGap         = 1.3
	weightInstability    = 0.8

	// logisticBias shifts the squash function so that all-zero indicators
	// (a perfectly quiet protocol) score near-zero probability rather than
	// 50% — a sigmoid centered at 0 input maps 0 to 0.5, which would call
	// an untroubled protocol "50% likely to deteriorate." The bias offsets
	// that back down.
	logisticBias = -4.0

	// Calibration thresholds. Chosen so that a single strong indicator
	// alone (e.g. TVL outflow velocity ~40%, per the issue's own worked
	// example) lands in "moderate," and multiple corroborating strong
	// indicators together are required to reach "severe" — reflecting
	// that severe triggers automatic capital movement and must not fire
	// on a single noisy signal.
	ThresholdMild     = 0.30
	ThresholdModerate = 0.55
	ThresholdSevere   = 0.75

	// minSampleCountForConfidence: below this, indicators are computed
	// from too little history to trust — the score is still produced (so
	// the pipeline never silently skips a protocol) but capped below the
	// severe threshold, since automatic capital movement on a thin sample
	// is exactly the "cries wolf" failure mode calibration must avoid.
	minSampleCountForConfidence = 4
)

// normalizeOutflow maps a TVL-outflow-velocity percentage to roughly [0,1]:
// 0% outflow -> 0, 50%+ outflow -> saturates at 1. Inflow (negative values)
// clamps to 0, not negative — inflow isn't itself a deterioration signal in
// this model.
func normalizeOutflow(pct float64) float64 {
	return clamp01(pct / 50.0)
}

// normalizeAbsZScore maps an absolute z-score to roughly [0,1]: a z-score
// of 3 (a genuinely rare deviation for a roughly-normal series) saturates
// the term.
func normalizeAbsZScore(z float64) float64 {
	return clamp01(math.Abs(z) / 3.0)
}

// normalizeGapPct maps a reported-vs-derived gap percentage to [0,1]: 40%+
// divergence saturates.
func normalizeGapPct(pct float64) float64 {
	return clamp01(pct / 40.0)
}

// normalizeInstability maps a coefficient-of-variation to [0,1]: a CoV of
// 0.5+ (TVL swinging by half its mean) saturates.
func normalizeInstability(cov float64) float64 {
	return clamp01(cov / 0.5)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// Score computes a deterioration.Assessment from Indicators. Deterministic:
// identical inputs always produce an identical probability and level.
func Score(protocolSlug string, ind deterioration.Indicators) deterioration.Assessment {
	nOutflow := normalizeOutflow(ind.TVLOutflowVelocityPct)
	nZ := normalizeAbsZScore(ind.APYAbnormalityZScore)
	nGap := normalizeGapPct(ind.ReportedVsDerivedGapPct)
	nInstability := normalizeInstability(ind.PriceInstability)

	linear := logisticBias +
		weightTVLOutflow*nOutflow +
		weightAPYAbnormality*nZ +
		weightAPYGap*nGap +
		weightInstability*nInstability

	probability := 1.0 / (1.0 + math.Exp(-linear))

	if ind.SampleCount < minSampleCountForConfidence && probability >= ThresholdSevere {
		// A thin sample must never alone justify the severe tier — see
		// minSampleCountForConfidence's doc comment.
		probability = ThresholdSevere - 0.01
	}

	level := levelFor(probability)
	return deterioration.Assessment{
		ProtocolSlug: protocolSlug,
		Indicators:   ind,
		Probability:  probability,
		Level:        level,
		Explanation:  explain(protocolSlug, ind, probability, level),
	}
}

func levelFor(probability float64) deterioration.Level {
	switch {
	case probability >= ThresholdSevere:
		return deterioration.LevelSevere
	case probability >= ThresholdModerate:
		return deterioration.LevelModerate
	case probability >= ThresholdMild:
		return deterioration.LevelMild
	default:
		return deterioration.LevelNone
	}
}

// explain renders the specific indicators driving the assessment so an
// operator can act in seconds (#857) — never just the bare probability.
func explain(slug string, ind deterioration.Indicators, probability float64, level deterioration.Level) string {
	var drivers []string
	if ind.TVLOutflowVelocityPct > 5 {
		drivers = append(drivers, fmt.Sprintf("TVL down %.0f%% in the window", ind.TVLOutflowVelocityPct))
	}
	if math.Abs(ind.APYAbnormalityZScore) > 1 {
		direction := "spiked"
		if ind.APYAbnormalityZScore < 0 {
			direction = "collapsed"
		}
		drivers = append(drivers, fmt.Sprintf("APY %s (z-score %.1f)", direction, ind.APYAbnormalityZScore))
	}
	if ind.ReportedVsDerivedGapPct > 5 {
		drivers = append(drivers, fmt.Sprintf("reported-vs-derived APY gap %.0f%%", ind.ReportedVsDerivedGapPct))
	}
	if ind.PriceInstability > 0.1 {
		drivers = append(drivers, fmt.Sprintf("TVL instability (CoV %.2f)", ind.PriceInstability))
	}
	if len(drivers) == 0 {
		return fmt.Sprintf("%s: no significant deterioration signals (%.0f%% probability)", slug, probability*100)
	}
	return fmt.Sprintf("%s: %s — %.0f%% deterioration probability (%s)",
		slug, strings.Join(drivers, ", "), probability*100, level)
}
