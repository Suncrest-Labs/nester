package nudge

import (
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
)

type Cap struct {
	MaxPerDay       int
	MaxPerWeek      int
	MinSpacingHours int
}

func EffectiveCap(base Cap, tier usersignal.EngagementTier) Cap {
	switch tier {
	case usersignal.TierHighlyEngaged, usersignal.TierEngaged:
		return base
	case usersignal.TierAtRisk:
		return Cap{
			MaxPerDay:       1,
			MaxPerWeek:      1,
			MinSpacingHours: 72,
		}
	case usersignal.TierDormant:
		return Cap{
			MaxPerDay:       1,
			MaxPerWeek:      1, // the issue said "1 per 14 days" for dormant, so we might need a MaxPer14Days but let's just hack MaxPerWeek=0 maybe?
			MinSpacingHours: 14 * 24, // 14 days minimum spacing effectively limits to 1 per 14 days
		}
	default:
		return base
	}
}

func Allow(history []DispatchRecord, cap Cap, now time.Time) (bool, string) {
	if len(history) == 0 {
		return true, ""
	}

	// Count last 24h, last 7d, check minimum spacing
	last24h := 0
	last7d := 0
	mostRecent := time.Time{}

	for _, record := range history {
		if record.SentAt.After(mostRecent) {
			mostRecent = record.SentAt
		}
		age := now.Sub(record.SentAt)
		if age <= 24*time.Hour {
			last24h++
		}
		if age <= 7*24*time.Hour {
			last7d++
		}
	}

	if cap.MinSpacingHours > 0 {
		spacing := time.Duration(cap.MinSpacingHours) * time.Hour
		if now.Sub(mostRecent) < spacing {
			return false, "min_spacing"
		}
	}

	if last24h >= cap.MaxPerDay {
		return false, "max_per_day"
	}
	if last7d >= cap.MaxPerWeek && cap.MaxPerWeek > 0 { // For dormant maxPerWeek might be bypassed by MinSpacing=14days, but if we check it here we shouldn't fail if we want 1 per 14 days. Wait, if MaxPerWeek=1 it means 1 in 7 days, which is less strict than 14 days. It's fine.
		return false, "max_per_week"
	}

	return true, ""
}
