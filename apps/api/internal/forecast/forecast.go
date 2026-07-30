// Package forecast provides yield (APY) forecasting for vault protocols from
// historical trends and macro signals (issue #118).
//
// The model is an explainable heuristic rather than an opaque ML black box: it
// blends a recency-weighted average of historical APY with adjustments for TVL
// trend (rising TVL compresses yield), the stablecoin market rate (yields track
// market rates), and the policy-rate direction (tightening lifts yields). It
// returns a point estimate, a confidence band derived from historical
// volatility, a confidence score, and a human-readable rationale.
package forecast

import (
	"errors"
	"fmt"
	"math"
)

// minHistory is the smallest number of historical APY points we will forecast
// from; fewer than this is too noisy to be meaningful.
const minHistory = 3

// RateDirection encodes the direction of the benchmark policy rate.
type RateDirection int

const (
	RateEasing     RateDirection = -1
	RateNeutral    RateDirection = 0
	RateTightening RateDirection = 1
)

// Signals are the inputs to a forecast. APY values are percentages (e.g. 8.5).
type Signals struct {
	// HistoricalAPY in chronological order (oldest first), as percentages.
	HistoricalAPY []float64
	// TVLTrend is the recent fractional change in TVL (e.g. +0.2 = +20%).
	TVLTrend float64
	// StablecoinRate is a market lending-rate proxy (percentage); 0 = unknown.
	StablecoinRate float64
	// RateDir is the policy-rate direction.
	RateDir RateDirection
}

// Forecast is the predicted APY range for the next period.
type Forecast struct {
	PredictedAPY float64 `json:"predicted_apy"`
	LowerBound   float64 `json:"lower_bound"`
	UpperBound   float64 `json:"upper_bound"`
	Confidence   float64 `json:"confidence"` // 0..1
	Rationale    string  `json:"rationale"`
}

var errInsufficientHistory = fmt.Errorf("forecast: need at least %d historical APY points", minHistory)

// ErrInsufficientHistory is returned when too few data points are supplied.
func ErrInsufficientHistory() error { return errInsufficientHistory }

// Predict produces an APY forecast from the supplied signals.
func Predict(s Signals) (Forecast, error) {
	if len(s.HistoricalAPY) < minHistory {
		return Forecast{}, errInsufficientHistory
	}
	for _, v := range s.HistoricalAPY {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return Forecast{}, errors.New("forecast: historical APY contains a non-finite value")
		}
	}

	base := recencyWeightedMean(s.HistoricalAPY)
	vol := stdDev(s.HistoricalAPY)

	// TVL trend: rising TVL compresses yield. Bounded so an extreme TVL swing
	// can't dominate the estimate.
	tvlAdj := -clamp(s.TVLTrend, -2, 2) * 0.10 * base
	// Policy rate: tightening tends to lift on-chain stablecoin yields.
	rateAdj := float64(s.RateDir) * 0.05 * base

	predicted := base + tvlAdj + rateAdj
	// Yields track the broader market rate; nudge gently toward it when known.
	if s.StablecoinRate > 0 {
		predicted = 0.85*predicted + 0.15*s.StablecoinRate
	}
	if predicted < 0 {
		predicted = 0
	}

	// Band: ~1 std dev, but never narrower than 10% of the estimate.
	half := math.Max(vol, 0.10*predicted)
	lower := math.Max(0, predicted-half)
	upper := predicted + half

	return Forecast{
		PredictedAPY: round2(predicted),
		LowerBound:   round2(lower),
		UpperBound:   round2(upper),
		Confidence:   round2(confidence(len(s.HistoricalAPY), vol, base)),
		Rationale:    rationale(base, tvlAdj, rateAdj, s),
	}, nil
}

// recencyWeightedMean weights more recent observations more heavily (linear).
func recencyWeightedMean(xs []float64) float64 {
	var sum, wsum float64
	for i, x := range xs {
		w := float64(i + 1)
		sum += w * x
		wsum += w
	}
	return sum / wsum
}

func mean(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func stdDev(xs []float64) float64 {
	m := mean(xs)
	var ss float64
	for _, x := range xs {
		d := x - m
		ss += d * d
	}
	return math.Sqrt(ss / float64(len(xs)))
}

// confidence rises with sample size and falls with relative volatility.
func confidence(n int, vol, base float64) float64 {
	sample := math.Min(1, float64(n)/12.0)
	relVol := 0.0
	if base > 0 {
		relVol = math.Min(1, vol/base)
	}
	c := 0.3 + 0.6*sample - 0.4*relVol
	return clamp(c, 0.05, 0.95)
}

func rationale(base, tvlAdj, rateAdj float64, s Signals) string {
	dir := "neutral"
	switch s.RateDir {
	case RateTightening:
		dir = "tightening"
	case RateEasing:
		dir = "easing"
	}
	return fmt.Sprintf(
		"recency-weighted base %.2f%%; TVL trend %.0f%% -> %+.2f; policy %s -> %+.2f",
		base, s.TVLTrend*100, tvlAdj, dir, rateAdj,
	)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
