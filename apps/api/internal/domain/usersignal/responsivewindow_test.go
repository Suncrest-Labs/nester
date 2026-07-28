package usersignal

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func eventAt(hour int) ActivityEvent {
	return ActivityEvent{
		ID:         uuid.New(),
		OccurredAt: time.Date(2026, 7, 20, hour, 0, 0, 0, time.UTC),
	}
}

func TestInferResponsiveWindow_FromActivityEvents(t *testing.T) {
	events := []ActivityEvent{
		eventAt(14), eventAt(14), eventAt(14),
		eventAt(9),
	}
	window := InferResponsiveWindow(events, "UTC")
	if window.HourOfDay != 14 {
		t.Fatalf("expected the most frequent hour (14) to be chosen, got %d", window.HourOfDay)
	}
}

func TestInferResponsiveWindow_AvoidsBadHours(t *testing.T) {
	events := []ActivityEvent{
		eventAt(2), eventAt(2), eventAt(2), eventAt(2), // most frequent, but a bad hour
		eventAt(9), // less frequent, but a good hour
	}
	window := InferResponsiveWindow(events, "UTC")
	if window.HourOfDay == 2 {
		t.Fatalf("expected a known-bad hour (2am) to never be chosen even when it's the most frequent, got %d", window.HourOfDay)
	}
	if window.HourOfDay != 9 {
		t.Fatalf("expected the best non-bad hour (9) to be chosen, got %d", window.HourOfDay)
	}
}

func TestInferResponsiveWindow_AllBadHoursFallsBackSafely(t *testing.T) {
	events := []ActivityEvent{eventAt(1), eventAt(23)}
	window := InferResponsiveWindow(events, "UTC")
	if IsBadTime(time.Date(2026, 7, 20, window.HourOfDay, 0, 0, 0, time.UTC), "UTC") {
		t.Fatalf("expected a safe fallback hour when every observed hour is bad, got %d", window.HourOfDay)
	}
}

func TestIsWithinWindow_RespectsTimezone(t *testing.T) {
	window := ResponsiveWindow{HourOfDay: 9, Timezone: "America/New_York"}
	// 14:00 UTC is 09:00/10:00 in New York depending on DST; pick a date
	// firmly in EDT (UTC-4) so 13:00 UTC == 09:00 local.
	within := time.Date(2026, 7, 20, 13, 0, 0, 0, time.UTC)
	notWithin := time.Date(2026, 7, 20, 20, 0, 0, 0, time.UTC)

	if !IsWithinWindow(window, within) {
		t.Fatalf("expected 13:00 UTC (09:00 EDT) to be within a 9am America/New_York window")
	}
	if IsWithinWindow(window, notWithin) {
		t.Fatalf("expected 20:00 UTC to be outside a 9am America/New_York window")
	}
}

func TestIsBadTime_RejectsLateNight(t *testing.T) {
	late := time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC)
	midday := time.Date(2026, 7, 20, 14, 0, 0, 0, time.UTC)

	if !IsBadTime(late, "UTC") {
		t.Fatalf("expected 2am to be flagged as a bad time")
	}
	if IsBadTime(midday, "UTC") {
		t.Fatalf("expected 2pm to not be flagged as a bad time")
	}
}
