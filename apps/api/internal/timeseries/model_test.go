package timeseries

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestSeriesRefKeySortsDimensions(t *testing.T) {
	key, err := (SeriesRef{
		Metric:     MetricAPY,
		EntityType: "vault",
		EntityID:   "v1",
		Dimensions: map[string]string{"period": "7d", "source": "blend"},
	}).Key()
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if key != "apy:vault:v1:period=7d:source=blend" {
		t.Fatalf("Key() = %q", key)
	}
}

func TestAggregateComputesOHLCAndAverage(t *testing.T) {
	series := SeriesRef{Metric: MetricTVL, EntityType: "vault", EntityID: "v1"}
	base := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	points := []Point{
		{Series: series, ObservedAt: base.Add(40 * time.Second), Value: decimal.RequireFromString("12")},
		{Series: series, ObservedAt: base.Add(10 * time.Second), Value: decimal.RequireFromString("10")},
		{Series: series, ObservedAt: base.Add(50 * time.Second), Value: decimal.RequireFromString("8")},
	}

	rollups, err := Aggregate(points, ResolutionMinute)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}
	if len(rollups) != 1 {
		t.Fatalf("len(rollups) = %d, want 1", len(rollups))
	}
	got := rollups[0]
	assertDecimal(t, "open", got.Open, "10")
	assertDecimal(t, "high", got.High, "12")
	assertDecimal(t, "low", got.Low, "8")
	assertDecimal(t, "close", got.Close, "8")
	assertDecimal(t, "average", got.Average, "10")
	assertDecimal(t, "last", got.Last, "8")
	if got.Count != 3 {
		t.Fatalf("Count = %d, want 3", got.Count)
	}
}

func TestSelectTier(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		req  QueryRequest
		want Resolution
	}{
		{
			name: "recent narrow range uses raw",
			req:  QueryRequest{From: now.Add(-2 * time.Hour), To: now, MaxPoints: 180},
			want: ResolutionRaw,
		},
		{
			name: "multi day range uses hour rollup",
			req:  QueryRequest{From: now.Add(-48 * time.Hour), To: now, MaxPoints: 100},
			want: ResolutionHour,
		},
		{
			name: "wide range uses day rollup",
			req:  QueryRequest{From: now.Add(-180 * 24 * time.Hour), To: now, MaxPoints: 100},
			want: ResolutionDay,
		},
		{
			name: "preferred resolution wins",
			req:  QueryRequest{From: now.Add(-180 * 24 * time.Hour), To: now, PreferredResolution: ResolutionMinute},
			want: ResolutionMinute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SelectTier(tt.req, now); got != tt.want {
				t.Fatalf("SelectTier() = %q, want %q", got, tt.want)
			}
		})
	}
}

func assertDecimal(t *testing.T, name string, got decimal.Decimal, want string) {
	t.Helper()
	if !got.Equal(decimal.RequireFromString(want)) {
		t.Fatalf("%s = %s, want %s", name, got.String(), want)
	}
}
