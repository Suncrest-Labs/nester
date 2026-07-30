package apysnapshot

import (
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func validSnapshot() APYSnapshot {
	return APYSnapshot{
		ID:           uuid.New(),
		ProtocolSlug: "aave-v3",
		APY:          decimal.RequireFromString("4.25"),
		TVL:          decimal.RequireFromString("1000000"),
		CapturedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestAPYSnapshot_Validate_Valid(t *testing.T) {
	if err := validSnapshot().Validate(); err != nil {
		t.Fatalf("Validate() unexpected error = %v", err)
	}
}

func TestAPYSnapshot_Validate_ZeroAPYAndTVLAreValid(t *testing.T) {
	snap := validSnapshot()
	snap.APY = decimal.Zero
	snap.TVL = decimal.Zero
	if err := snap.Validate(); err != nil {
		t.Fatalf("Validate() unexpected error for zero APY/TVL = %v", err)
	}
}

func TestAPYSnapshot_Validate_InvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*APYSnapshot)
	}{
		{"empty protocol slug", func(s *APYSnapshot) { s.ProtocolSlug = "" }},
		{"whitespace protocol slug", func(s *APYSnapshot) { s.ProtocolSlug = "   " }},
		{"negative apy", func(s *APYSnapshot) { s.APY = decimal.RequireFromString("-0.01") }},
		{"negative tvl", func(s *APYSnapshot) { s.TVL = decimal.RequireFromString("-1") }},
		{"zero captured_at", func(s *APYSnapshot) { s.CapturedAt = time.Time{} }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := validSnapshot()
			tt.modify(&snap)
			if err := snap.Validate(); err != ErrInvalidSnapshot {
				t.Fatalf("Validate() error = %v, want ErrInvalidSnapshot", err)
			}
		})
	}
}

func TestAPYSnapshot_APYPrecision(t *testing.T) {
	// Yield figures must not lose precision at small decimal values.
	raw := "0.000000000000000001"
	apy, err := decimal.NewFromString(raw)
	if err != nil {
		t.Fatalf("decimal.NewFromString: %v", err)
	}
	snap := validSnapshot()
	snap.APY = apy
	if snap.APY.String() != raw {
		t.Errorf("precision lost: got %q, want %q", snap.APY.String(), raw)
	}
}

func TestByCapturedAt_SortsAscending(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snaps := []APYSnapshot{
		{ProtocolSlug: "aave", CapturedAt: base.Add(48 * time.Hour)},
		{ProtocolSlug: "aave", CapturedAt: base},
		{ProtocolSlug: "aave", CapturedAt: base.Add(24 * time.Hour)},
	}

	sort.Sort(ByCapturedAt(snaps))

	for i := 1; i < len(snaps); i++ {
		if snaps[i].CapturedAt.Before(snaps[i-1].CapturedAt) {
			t.Fatalf("snapshots not in ascending order at index %d: %v before %v", i, snaps[i].CapturedAt, snaps[i-1].CapturedAt)
		}
	}
	if !snaps[0].CapturedAt.Equal(base) {
		t.Fatalf("earliest snapshot = %v, want %v", snaps[0].CapturedAt, base)
	}
	if !snaps[2].CapturedAt.Equal(base.Add(48 * time.Hour)) {
		t.Fatalf("latest snapshot = %v, want %v", snaps[2].CapturedAt, base.Add(48*time.Hour))
	}
}

func TestByCapturedAt_StableForEqualTimestamps(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snaps := []APYSnapshot{
		{ProtocolSlug: "aave", CapturedAt: base},
		{ProtocolSlug: "aave", CapturedAt: base},
	}
	sort.Sort(ByCapturedAt(snaps))
	if len(snaps) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snaps))
	}
}

func TestDuplicateTimestamps_NoneFound(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snaps := []APYSnapshot{
		{ProtocolSlug: "aave", CapturedAt: base},
		{ProtocolSlug: "aave", CapturedAt: base.Add(time.Hour)},
		{ProtocolSlug: "aave", CapturedAt: base.Add(2 * time.Hour)},
	}
	if got := DuplicateTimestamps(snaps); len(got) != 0 {
		t.Fatalf("DuplicateTimestamps() = %v, want empty", got)
	}
}

func TestDuplicateTimestamps_DetectsExactDuplicate(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snaps := []APYSnapshot{
		{ProtocolSlug: "aave", CapturedAt: base, APY: decimal.RequireFromString("4.0")},
		{ProtocolSlug: "aave", CapturedAt: base, APY: decimal.RequireFromString("4.5")},
		{ProtocolSlug: "aave", CapturedAt: base.Add(time.Hour)},
	}

	got := DuplicateTimestamps(snaps)
	if len(got) != 1 {
		t.Fatalf("DuplicateTimestamps() returned %d entries, want 1", len(got))
	}
	if !got[0].Equal(base) {
		t.Fatalf("duplicate timestamp = %v, want %v", got[0], base)
	}
}

func TestDuplicateTimestamps_ReportsEachDuplicateGroupOnce(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	snaps := []APYSnapshot{
		{ProtocolSlug: "aave", CapturedAt: base},
		{ProtocolSlug: "aave", CapturedAt: base},
		{ProtocolSlug: "aave", CapturedAt: base},
	}
	got := DuplicateTimestamps(snaps)
	if len(got) != 1 {
		t.Fatalf("DuplicateTimestamps() returned %d entries, want 1 (deduped)", len(got))
	}
}

func TestErrDuplicateSnapshot_Message(t *testing.T) {
	if ErrDuplicateSnapshot == nil {
		t.Fatal("ErrDuplicateSnapshot must not be nil")
	}
	want := "snapshot already exists for this protocol and timestamp"
	if ErrDuplicateSnapshot.Error() != want {
		t.Errorf("got %q, want %q", ErrDuplicateSnapshot.Error(), want)
	}
}

func TestErrProtocolNotFound_Message(t *testing.T) {
	if ErrProtocolNotFound == nil {
		t.Fatal("ErrProtocolNotFound must not be nil")
	}
	want := "protocol not found"
	if ErrProtocolNotFound.Error() != want {
		t.Errorf("got %q, want %q", ErrProtocolNotFound.Error(), want)
	}
}
