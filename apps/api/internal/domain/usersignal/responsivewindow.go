package usersignal

import (
	"time"
)

type ResponsiveWindow struct {
	HourOfDay int // 0-23
	Timezone  string
}

// isBadHour is the shared "don't nudge in the middle of the night" rule
// used both to filter candidate hours in InferResponsiveWindow and to gate
// dispatch in IsBadTime.
func isBadHour(h int) bool {
	return h >= 22 || h < 8
}

// InferResponsiveWindow picks the user's most common activity hour, in
// their timezone, while avoiding known-bad hours (late night/early
// morning): a user who happens to be most active at 2am still gets
// scheduled into their best *non-bad* hour rather than 2am itself.
func InferResponsiveWindow(events []ActivityEvent, timezone string) ResponsiveWindow {
	if len(events) == 0 {
		return ResponsiveWindow{HourOfDay: 12, Timezone: timezone}
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}

	hourCounts := make(map[int]int)
	for _, e := range events {
		h := e.OccurredAt.In(loc).Hour()
		hourCounts[h]++
	}

	bestHour, bestCount := -1, -1
	bestBadHour, bestBadCount := -1, -1
	for h, c := range hourCounts {
		if isBadHour(h) {
			if c > bestBadCount {
				bestBadCount = c
				bestBadHour = h
			}
			continue
		}
		if c > bestCount {
			bestCount = c
			bestHour = h
		}
	}

	if bestHour >= 0 {
		return ResponsiveWindow{HourOfDay: bestHour, Timezone: timezone}
	}
	// Every observed activity hour was a bad hour — fall back to a safe
	// default rather than scheduling into the night.
	_ = bestBadHour
	return ResponsiveWindow{HourOfDay: 12, Timezone: timezone}
}

func IsWithinWindow(window ResponsiveWindow, now time.Time) bool {
	loc, err := time.LoadLocation(window.Timezone)
	if err != nil {
		loc = time.UTC
	}
	currentHour := now.In(loc).Hour()
	return currentHour == window.HourOfDay
}

func IsBadTime(now time.Time, timezone string) bool {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	return isBadHour(now.In(loc).Hour())
}
