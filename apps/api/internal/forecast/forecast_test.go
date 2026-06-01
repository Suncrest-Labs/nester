package forecast

import (
	"errors"
	"math"
	"testing"
)

func TestPredict_InsufficientHistory(t *testing.T) {
	_, err := Predict(Signals{HistoricalAPY: []float64{8, 8}})
	if !errors.Is(err, ErrInsufficientHistory()) {
		t.Fatalf("expected insufficient-history error, got %v", err)
	}
}

func TestPredict_RejectsNonFinite(t *testing.T) {
	_, err := Predict(Signals{HistoricalAPY: []float64{8, math.Inf(1), 8}})
	if err == nil {
		t.Fatal("expected error for non-finite input")
	}
}

func TestPredict_StableHistoryTracksLevel(t *testing.T) {
	f, err := Predict(Signals{HistoricalAPY: []float64{8, 8, 8, 8, 8}})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(f.PredictedAPY-8) > 0.01 {
		t.Fatalf("expected ~8%%, got %v", f.PredictedAPY)
	}
	if !(f.LowerBound <= f.PredictedAPY && f.PredictedAPY <= f.UpperBound) {
		t.Fatalf("predicted must lie within band: %+v", f)
	}
	if f.LowerBound < 0 {
		t.Fatalf("lower bound must not be negative: %v", f.LowerBound)
	}
}

func TestPredict_RisingTVLCompressesYield(t *testing.T) {
	hist := []float64{10, 10, 10, 10}
	flat, _ := Predict(Signals{HistoricalAPY: hist})
	grown, _ := Predict(Signals{HistoricalAPY: hist, TVLTrend: 0.5})
	if !(grown.PredictedAPY < flat.PredictedAPY) {
		t.Fatalf("rising TVL should lower predicted APY: flat=%v grown=%v", flat.PredictedAPY, grown.PredictedAPY)
	}
}

func TestPredict_TighteningLiftsYieldVsEasing(t *testing.T) {
	hist := []float64{10, 10, 10, 10}
	tightening, _ := Predict(Signals{HistoricalAPY: hist, RateDir: RateTightening})
	easing, _ := Predict(Signals{HistoricalAPY: hist, RateDir: RateEasing})
	if !(tightening.PredictedAPY > easing.PredictedAPY) {
		t.Fatalf("tightening should predict higher APY than easing: %v vs %v", tightening.PredictedAPY, easing.PredictedAPY)
	}
}

func TestPredict_VolatileHistoryWidensBandAndLowersConfidence(t *testing.T) {
	calm, _ := Predict(Signals{HistoricalAPY: []float64{9, 10, 9, 10, 9, 10}})
	wild, _ := Predict(Signals{HistoricalAPY: []float64{2, 18, 3, 17, 1, 19}})
	calmWidth := calm.UpperBound - calm.LowerBound
	wildWidth := wild.UpperBound - wild.LowerBound
	if !(wildWidth > calmWidth) {
		t.Fatalf("volatile history should widen the band: calm=%v wild=%v", calmWidth, wildWidth)
	}
	if !(wild.Confidence < calm.Confidence) {
		t.Fatalf("volatile history should lower confidence: calm=%v wild=%v", calm.Confidence, wild.Confidence)
	}
}

func TestPredict_RecencyWeighting(t *testing.T) {
	// An upward-trending series should predict above its simple mean (6).
	f, err := Predict(Signals{HistoricalAPY: []float64{2, 4, 6, 8, 10}})
	if err != nil {
		t.Fatal(err)
	}
	if !(f.PredictedAPY > 6) {
		t.Fatalf("recency weighting should pull an uptrend above the simple mean: %v", f.PredictedAPY)
	}
}
