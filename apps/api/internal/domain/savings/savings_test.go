package savings

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func snap(price string, at time.Time) Snapshot {
	return Snapshot{TakenAt: at, SharePrice: decimal.RequireFromString(price)}
}

func TestTrailingRate_InsufficientHistory(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	if got := TrailingRate([]Snapshot{snap("1", now)}, 7*24*time.Hour, now); got != nil {
		t.Fatalf("one snapshot: want nil, got %s", got)
	}
	two := []Snapshot{snap("1", now.Add(-time.Hour)), snap("1.001", now)}
	if got := TrailingRate(two, 7*24*time.Hour, now); got != nil {
		t.Fatalf("1h apart: want nil, got %s", got)
	}
}

func TestTrailingRate_Annualizes(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	snaps := []Snapshot{snap("1.0000", now.Add(-7*24*time.Hour)), snap("1.0010", now)}
	got := TrailingRate(snaps, 7*24*time.Hour, now)
	if got == nil {
		t.Fatal("want rate, got nil")
	}
	if !got.Sub(decimal.RequireFromString("5.35")).Abs().LessThan(decimal.RequireFromString("0.02")) {
		t.Fatalf("want ~5.35, got %s", got)
	}
}

func TestTrailingRate_IgnoresSnapshotsOutsideWindow(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	snaps := []Snapshot{snap("0.5", now.Add(-30*24*time.Hour)), snap("1.0000", now.Add(-7*24*time.Hour)), snap("1.0010", now)}
	got := TrailingRate(snaps, 7*24*time.Hour, now)
	if got == nil || got.GreaterThan(decimal.NewFromInt(6)) {
		t.Fatalf("out-of-window snapshot leaked in: %v", got)
	}
}
