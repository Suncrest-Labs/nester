package service

import (
	"testing"
	"time"
)

// TestLocalCalendarDate_CrossesUTCBoundary is a regression test for #923: the
// deadline reminder dedup key must be derived from the user's local calendar
// date, not the server's UTC date. A user in a timezone far ahead of (or
// behind) UTC can have their local day fall on a different calendar date than
// UTC at the same instant. If the dedup key used UTC's date, two scheduler
// ticks that occur on the same local calendar day but straddle the UTC day
// boundary would produce two different dedup keys and double-send the
// reminder.
func TestLocalCalendarDate_CrossesUTCBoundary(t *testing.T) {
	// 23:30 UTC on 2026-07-28 is already 2026-07-29 in Kiritimati (UTC+14).
	utcMoment := time.Date(2026, 7, 28, 23, 30, 0, 0, time.UTC)

	utcDate := utcMoment.UTC().Format("2006-01-02")
	localDate := localCalendarDate(utcMoment, "Pacific/Kiritimati")

	if utcDate == localDate {
		t.Fatalf("expected UTC date (%s) and local Kiritimati date (%s) to differ across the boundary, test setup is invalid", utcDate, localDate)
	}
	if localDate != "2026-07-29" {
		t.Fatalf("expected local date 2026-07-29 for UTC+14 user, got %s", localDate)
	}

	// A second tick an hour later is still 2026-07-29 UTC, but is now also
	// 2026-07-29 in Kiritimati -- the two ticks fall on the same local
	// calendar day and must dedup to the same key.
	secondTick := utcMoment.Add(90 * time.Minute)
	if got := localCalendarDate(secondTick, "Pacific/Kiritimati"); got != localDate {
		t.Fatalf("expected both ticks within the same local calendar day to produce the same dedup date, got %s and %s", localDate, got)
	}
}

// TestLocalCalendarDate_NegativeOffsetCrossesBoundary covers the opposite
// extreme: a user far behind UTC (e.g. UTC-11) whose local day starts later
// than UTC's, so a tick just after UTC midnight is still "yesterday" locally.
func TestLocalCalendarDate_NegativeOffsetCrossesBoundary(t *testing.T) {
	// 00:30 UTC on 2026-07-29 is still 2026-07-28 in Pago Pago (UTC-11).
	utcMoment := time.Date(2026, 7, 29, 0, 30, 0, 0, time.UTC)

	utcDate := utcMoment.UTC().Format("2006-01-02")
	localDate := localCalendarDate(utcMoment, "Pacific/Pago_Pago")

	if utcDate == localDate {
		t.Fatalf("expected UTC date (%s) and local Pago Pago date (%s) to differ across the boundary, test setup is invalid", utcDate, localDate)
	}
	if localDate != "2026-07-28" {
		t.Fatalf("expected local date 2026-07-28 for UTC-11 user, got %s", localDate)
	}
}

// TestLocalCalendarDate_UnknownTimezoneFallsBackToUTC ensures a bad/unknown
// IANA zone name degrades to UTC rather than panicking or erroring the whole
// dispatch path.
func TestLocalCalendarDate_UnknownTimezoneFallsBackToUTC(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	got := localCalendarDate(now, "Not/A_Real_Zone")
	want := now.UTC().Format("2006-01-02")
	if got != want {
		t.Fatalf("expected fallback to UTC date %s, got %s", want, got)
	}
}
