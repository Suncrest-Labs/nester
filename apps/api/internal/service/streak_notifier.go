package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

// StreakMilestoneNotifier delivers a notification when a user reaches a streak badge milestone.
type StreakMilestoneNotifier interface {
	SendStreakMilestone(ctx context.Context, userID uuid.UUID, weeks int)
}

type noopStreakMilestoneNotifier struct{}

func (noopStreakMilestoneNotifier) SendStreakMilestone(context.Context, uuid.UUID, int) {}

// DispatcherStreakMilestoneNotifier sends streak milestone notifications via the dispatcher.
type DispatcherStreakMilestoneNotifier struct {
	Dispatcher *notifications.Dispatcher
}

func (n DispatcherStreakMilestoneNotifier) SendStreakMilestone(ctx context.Context, userID uuid.UUID, weeks int) {
	if n.Dispatcher == nil {
		return
	}
	title, body := streakMilestoneContent(weeks)
	_ = n.Dispatcher.Send(ctx, userID, notifications.EventSavingsStreak, title, body, map[string]any{
		"streak_weeks": weeks,
	})
}

func streakMilestoneContent(weeks int) (string, string) {
	switch weeks {
	case 4:
		return "4-week streak!", "You've saved every week for a month. Keep the momentum going!"
	case 8:
		return "8-week streak!", "Two months of consistent saving — you're building a great habit!"
	case 12:
		return "12-week streak!", fmt.Sprintf("A full quarter of weekly deposits. %d weeks strong!", weeks)
	case 26:
		return "Half-year streak! 🏆", "26 weeks of saving — you've hit the half-year mark!"
	case 52:
		return "One-year streak! 🎉", "An entire year of weekly deposits. Outstanding commitment!"
	default:
		return fmt.Sprintf("%d-week streak!", weeks), fmt.Sprintf("You've saved every week for %d consecutive weeks!", weeks)
	}
}
