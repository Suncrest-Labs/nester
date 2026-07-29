package apysnapshot

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func snapshotWithAPY(apy string, capturedAt time.Time) APYSnapshot {
	return APYSnapshot{
		ProtocolSlug: "aave-v3",
		APY:          decimal.RequireFromString(apy),
		TVL:          decimal.RequireFromString("1000000"),
		CapturedAt:   capturedAt,
	}
}

func TestDetectAnomalousJump_NoPriorSnapshot(t *testing.T) {
	next := snapshotWithAPY("50", time.Now())
	anomalous, reason := DetectAnomalousJump(nil, next)
	if anomalous {
		t.Fatalf("expected no anomaly with no prior snapshot, got reason %q", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason, got %q", reason)
	}
}

func TestDetectAnomalousJump_NormalMovementNotFlagged(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	prev := snapshotWithAPY("4.5", base)
	next := snapshotWithAPY("5.1", base.Add(time.Hour)) // ~13% up, normal

	anomalous, reason := DetectAnomalousJump(&prev, next)
	if anomalous {
		t.Fatalf("expected no anomaly for normal movement, got reason %q", reason)
	}
	if reason != "" {
		t.Fatalf("expected empty reason, got %q", reason)
	}
}

func TestDetectAnomalousJump_SpikeIsFlagged(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	prev := snapshotWithAPY("5", base)
	next := snapshotWithAPY("20", base.Add(time.Hour)) // 4x jump

	anomalous, reason := DetectAnomalousJump(&prev, next)
	if !anomalous {
		t.Fatal("expected a 4x jump to be flagged as anomalous")
	}
	if !strings.Contains(reason, "jumped") {
		t.Fatalf("expected reason to describe an upward jump, got %q", reason)
	}
}

func TestDetectAnomalousJump_CrashIsFlagged(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	prev := snapshotWithAPY("30", base)
	next := snapshotWithAPY("2", base.Add(time.Hour)) // drop to 1/15th

	anomalous, reason := DetectAnomalousJump(&prev, next)
	if !anomalous {
		t.Fatal("expected a sharp drop to be flagged as anomalous")
	}
	if !strings.Contains(reason, "dropped") {
		t.Fatalf("expected reason to describe a downward drop, got %q", reason)
	}
}

func TestDetectAnomalousJump_ExactlyAtThresholdNotFlagged(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	prev := snapshotWithAPY("5", base)
	next := snapshotWithAPY("15", base.Add(time.Hour)) // exactly 3x

	anomalous, _ := DetectAnomalousJump(&prev, next)
	if anomalous {
		t.Fatal("expected exactly-at-threshold movement to not be flagged (strictly greater than required)")
	}
}

func TestDetectAnomalousJump_SkipsNearZeroBaseline(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	prev := snapshotWithAPY("0.1", base)               // below AnomalyJumpMinPriorAPY
	next := snapshotWithAPY("5", base.Add(time.Hour)) // 50x, but off a tiny base

	anomalous, reason := DetectAnomalousJump(&prev, next)
	if anomalous {
		t.Fatalf("expected near-zero baseline to skip jump detection, got reason %q", reason)
	}
}

func TestDetectAnomalousJump_ZeroOrNegativeNextAPYNotFlagged(t *testing.T) {
	base := time.Now().Add(-time.Hour)
	prev := snapshotWithAPY("5", base)
	next := snapshotWithAPY("0", base.Add(time.Hour))

	anomalous, _ := DetectAnomalousJump(&prev, next)
	if anomalous {
		t.Fatal("a drop to exactly zero should be handled by validation, not the jump detector")
	}
}
