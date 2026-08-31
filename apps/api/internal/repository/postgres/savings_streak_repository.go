package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
)

// SavingsStreakRepository persists savings streak data in Postgres.
type SavingsStreakRepository struct {
	db *sql.DB
}

// NewSavingsStreakRepository constructs a SavingsStreakRepository.
func NewSavingsStreakRepository(db *sql.DB) *SavingsStreakRepository {
	return &SavingsStreakRepository{db: db}
}

// Get returns the streak record for the user, or (nil, nil) if none exists.
func (r *SavingsStreakRepository) Get(ctx context.Context, userID uuid.UUID) (*savingsstreak.SavingsStreak, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT user_id, current_streak, longest_streak, last_deposit_week,
		       notified_milestones, created_at, updated_at
		FROM savings_streaks
		WHERE user_id = $1
	`, userID)

	var (
		uid                  string
		current, longest     int
		lastWeek             string
		milestones           pq.Int32Array
		createdAt, updatedAt interface{}
	)
	if err := row.Scan(&uid, &current, &longest, &lastWeek, &milestones, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	parsedID, _ := uuid.Parse(uid)
	ms := make([]int, len(milestones))
	for i, m := range milestones {
		ms[i] = int(m)
	}

	s := &savingsstreak.SavingsStreak{
		UserID:             parsedID,
		CurrentStreak:      current,
		LongestStreak:      longest,
		LastDepositWeek:    lastWeek,
		NotifiedMilestones: ms,
	}
	return s, nil
}

// Upsert inserts or replaces a streak record.
func (r *SavingsStreakRepository) Upsert(ctx context.Context, streak *savingsstreak.SavingsStreak) error {
	milestones := make(pq.Int32Array, len(streak.NotifiedMilestones))
	for i, m := range streak.NotifiedMilestones {
		// Milestones are small streak-week counts (7, 30, 90...), set by the
		// application rather than user input, so the narrowing is exact
		// (nester#1035, G115).
		milestones[i] = int32(m) // #nosec G115 -- application-defined streak milestones, far below int32 range
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO savings_streaks
		    (user_id, current_streak, longest_streak, last_deposit_week, notified_milestones, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
		    current_streak      = EXCLUDED.current_streak,
		    longest_streak      = EXCLUDED.longest_streak,
		    last_deposit_week   = EXCLUDED.last_deposit_week,
		    notified_milestones = EXCLUDED.notified_milestones,
		    updated_at          = NOW()
	`, streak.UserID, streak.CurrentStreak, streak.LongestStreak, streak.LastDepositWeek, milestones)
	return err
}
