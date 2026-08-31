package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
)

type SavingsGamificationRepository struct {
	db *sql.DB
}

func NewSavingsGamificationRepository(db *sql.DB) *SavingsGamificationRepository {
	return &SavingsGamificationRepository{db: db}
}

func (r *SavingsGamificationRepository) GetState(ctx context.Context, userID uuid.UUID) (savingsstreak.GamificationState, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(u.timezone, 'UTC'),
			COALESCE(s.current_streak_days, 0),
			COALESCE(s.longest_streak_days, 0),
			COALESCE(s.last_qualified_day, ''),
			COALESCE(s.grace_used_for_day, ''),
			COALESCE(s.total_saved::text, '0'),
			COALESCE(s.goals_completed, 0),
			COALESCE(s.current_level, 1),
			COALESCE(s.durable_score::text, '0'),
			COALESCE(s.awarded_achievements, ARRAY[]::TEXT[])
		FROM users u
		LEFT JOIN savings_gamification_state s ON s.user_id = u.id
		WHERE u.id = $1
	`, userID)

	var (
		state       savingsstreak.GamificationState
		totalSaved  string
		score       string
		achievements pq.StringArray
	)
	state.UserID = userID
	if err := row.Scan(
		&state.Timezone,
		&state.CurrentStreakDays,
		&state.LongestStreakDays,
		&state.LastQualifiedDay,
		&state.GraceUsedForDay,
		&totalSaved,
		&state.GoalsCompleted,
		&state.CurrentLevel,
		&score,
		&achievements,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			state.Timezone = "UTC"
			state.CurrentLevel = 1
			state.TotalSaved = decimal.Zero
			state.DurableScore = decimal.Zero
			return state, nil
		}
		return savingsstreak.GamificationState{}, err
	}
	var err error
	state.TotalSaved, err = decimal.NewFromString(totalSaved)
	if err != nil {
		return savingsstreak.GamificationState{}, err
	}
	state.DurableScore, err = decimal.NewFromString(score)
	if err != nil {
		return savingsstreak.GamificationState{}, err
	}
	state.AwardedAchievements = append([]string(nil), achievements...)
	return state, nil
}

func (r *SavingsGamificationRepository) RecordEvent(ctx context.Context, event savingsstreak.SavingEvent, transition savingsstreak.Transition) (bool, error) {
	transitionJSON, err := json.Marshal(transition)
	if err != nil {
		return false, err
	}
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO savings_gamification_events (
			event_id, user_id, event_type, amount, net_amount, occurred_at, qualified, reason, transition
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (event_id) DO NOTHING
	`, event.EventID, event.UserID, event.Type, event.Amount.String(), event.NetAmount.String(), event.OccurredAt.UTC(), transition.Qualified, transition.Reason, transitionJSON)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return true, nil
	}
	return rows > 0, nil
}

func (r *SavingsGamificationRepository) UpsertState(ctx context.Context, state savingsstreak.GamificationState) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO savings_gamification_state (
			user_id, current_streak_days, longest_streak_days, last_qualified_day,
			grace_used_for_day, total_saved, goals_completed, current_level,
			durable_score, awarded_achievements, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			current_streak_days = EXCLUDED.current_streak_days,
			longest_streak_days = EXCLUDED.longest_streak_days,
			last_qualified_day = EXCLUDED.last_qualified_day,
			grace_used_for_day = EXCLUDED.grace_used_for_day,
			total_saved = EXCLUDED.total_saved,
			goals_completed = EXCLUDED.goals_completed,
			current_level = EXCLUDED.current_level,
			durable_score = EXCLUDED.durable_score,
			awarded_achievements = EXCLUDED.awarded_achievements,
			updated_at = NOW()
	`, state.UserID, state.CurrentStreakDays, state.LongestStreakDays, state.LastQualifiedDay, state.GraceUsedForDay, state.TotalSaved.String(), state.GoalsCompleted, state.CurrentLevel, state.DurableScore.String(), pq.StringArray(state.AwardedAchievements))
	return err
}

func (r *SavingsGamificationRepository) AwardAchievement(ctx context.Context, userID uuid.UUID, code string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO savings_achievements (user_id, code)
		VALUES ($1, $2)
		ON CONFLICT (user_id, code) DO NOTHING
	`, userID, code)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return true, nil
	}
	return rows > 0, nil
}
