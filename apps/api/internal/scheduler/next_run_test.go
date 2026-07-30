package scheduler

import (
	"testing"
	"time"
)

func TestNextRunAt_Weekly(t *testing.T) {
	from := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	got := NextRunAt(from, "weekly")
	want := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextRunAt weekly = %v, want %v", got, want)
	}
}

func TestNextRunAt_Monthly_EndOfMonth(t *testing.T) {
	from := time.Date(2026, 1, 31, 12, 30, 0, 0, time.UTC)
	got := NextRunAt(from, "monthly")
	want := time.Date(2026, 2, 28, 12, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextRunAt monthly (Jan 31) = %v, want %v", got, want)
	}
}

func TestNextRunAt_Monthly_LeapYear(t *testing.T) {
	from := time.Date(2024, 1, 31, 8, 0, 0, 0, time.UTC)
	got := NextRunAt(from, "monthly")
	want := time.Date(2024, 2, 29, 8, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextRunAt monthly (leap year) = %v, want %v", got, want)
	}
}

func TestNextRunAt_Monthly_RegularDay(t *testing.T) {
	from := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
	got := NextRunAt(from, "monthly")
	want := time.Date(2026, 7, 15, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("NextRunAt monthly = %v, want %v", got, want)
	}
}
