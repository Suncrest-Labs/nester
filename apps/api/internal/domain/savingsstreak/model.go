package savingsstreak

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// StreakMilestones are the consecutive-week counts at which the user earns a badge notification.
var StreakMilestones = []int{4, 8, 12, 26, 52}

// SavingsStreak tracks consecutive weekly deposit activity for a user.
type SavingsStreak struct {
	UserID             uuid.UUID `json:"user_id"`
	CurrentStreak      int       `json:"current_streak"`
	LongestStreak      int       `json:"longest_streak"`
	LastDepositWeek    string    `json:"-"`
	NotifiedMilestones []int     `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// IsNewMilestone returns true if streakWeeks is one of the badge milestones and has
// not already been notified.
func (s *SavingsStreak) IsNewMilestone(streakWeeks int) bool {
	for _, m := range StreakMilestones {
		if m != streakWeeks {
			continue
		}
		for _, notified := range s.NotifiedMilestones {
			if notified == streakWeeks {
				return false
			}
		}
		return true
	}
	return false
}

// Repository is the persistence contract for savings streaks.
type Repository interface {
	// Get returns the streak for the given user, or (nil, nil) if none exists yet.
	Get(ctx context.Context, userID uuid.UUID) (*SavingsStreak, error)
	// Upsert inserts or fully replaces the streak record for the user.
	Upsert(ctx context.Context, streak *SavingsStreak) error
}
