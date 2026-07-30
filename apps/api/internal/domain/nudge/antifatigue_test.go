package nudge

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
)

func dispatchAt(t time.Time) DispatchRecord {
	return DispatchRecord{ID: uuid.New(), UserID: uuid.New(), SentAt: t}
}

func TestAllow_CapsPerDayAndWeek(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	cap := Cap{MaxPerDay: 1, MaxPerWeek: 3, MinSpacingHours: 0}

	// Already sent once today: a barrage (sending again right now) must be impossible.
	history := []DispatchRecord{dispatchAt(now.Add(-1 * time.Hour))}
	if allowed, reason := Allow(history, cap, now); allowed {
		t.Fatalf("expected max_per_day to block a second send within 24h, got allowed with reason %q", reason)
	}

	// Three sends already this week (each more than a day apart, so the
	// per-day cap alone doesn't explain a rejection) must hit the weekly cap.
	weekHistory := []DispatchRecord{
		dispatchAt(now.Add(-6 * 24 * time.Hour)),
		dispatchAt(now.Add(-4 * 24 * time.Hour)),
		dispatchAt(now.Add(-2 * 24 * time.Hour)),
	}
	if allowed, reason := Allow(weekHistory, cap, now); allowed {
		t.Fatalf("expected max_per_week to block a 4th send in 7 days, got allowed with reason %q", reason)
	}
}

func TestAllow_EnforcesMinimumSpacing(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	cap := Cap{MaxPerDay: 5, MaxPerWeek: 5, MinSpacingHours: 20}

	history := []DispatchRecord{dispatchAt(now.Add(-5 * time.Hour))}
	if allowed, reason := Allow(history, cap, now); allowed {
		t.Fatalf("expected min_spacing to block a send 5h after the last one (need 20h), got allowed with reason %q", reason)
	}

	history = []DispatchRecord{dispatchAt(now.Add(-21 * time.Hour))}
	if allowed, _ := Allow(history, cap, now); !allowed {
		t.Fatalf("expected a send 21h after the last one to clear a 20h spacing requirement")
	}
}

func TestAllow_NoBarrage(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	cap := Cap{MaxPerDay: 1, MaxPerWeek: 3, MinSpacingHours: 20}

	var history []DispatchRecord
	sent := 0
	for i := 0; i < 20; i++ {
		attemptAt := now.Add(time.Duration(i) * time.Hour)
		if allowed, _ := Allow(history, cap, attemptAt); allowed {
			sent++
			history = append(history, dispatchAt(attemptAt))
		}
	}
	// 20 hourly attempts under a 20h min-spacing / 1-per-day cap must not
	// produce more than one send.
	if sent > 1 {
		t.Fatalf("expected at most 1 send out of 20 rapid-fire attempts, got %d — anti-fatigue barrage protection failed", sent)
	}
}

func TestEffectiveCap_ShrinksAsEngagementDeclines(t *testing.T) {
	base := Cap{MaxPerDay: 1, MaxPerWeek: 3, MinSpacingHours: 20}

	engaged := EffectiveCap(base, usersignal.TierHighlyEngaged)
	atRisk := EffectiveCap(base, usersignal.TierAtRisk)
	dormant := EffectiveCap(base, usersignal.TierDormant)

	if atRisk.MaxPerWeek > engaged.MaxPerWeek {
		t.Fatalf("expected at_risk weekly cap (%d) <= engaged weekly cap (%d)", atRisk.MaxPerWeek, engaged.MaxPerWeek)
	}
	if dormant.MinSpacingHours < atRisk.MinSpacingHours {
		t.Fatalf("expected dormant min spacing (%dh) >= at_risk min spacing (%dh) — disengagement must reduce frequency, not increase it",
			dormant.MinSpacingHours, atRisk.MinSpacingHours)
	}
	if dormant.MinSpacingHours < engaged.MinSpacingHours {
		t.Fatalf("expected dormant min spacing (%dh) >= engaged min spacing (%dh)", dormant.MinSpacingHours, engaged.MinSpacingHours)
	}
}
