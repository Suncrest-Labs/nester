package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/nudge"
)

type NudgeHistoryRepository struct {
	db *sql.DB
}

func NewNudgeHistoryRepository(db *sql.DB) *NudgeHistoryRepository {
	return &NudgeHistoryRepository{db: db}
}

func (r *NudgeHistoryRepository) GetRecentDispatches(ctx context.Context, userID uuid.UUID, since time.Time) ([]nudge.DispatchRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, nudge_type, dedup_key, channel, title, body, copy_source, segment, sent_at
		FROM nudge_dispatch_log
		WHERE user_id = $1 AND sent_at >= $2
		ORDER BY sent_at DESC
	`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []nudge.DispatchRecord
	for rows.Next() {
		var l nudge.DispatchRecord
		if err := rows.Scan(&l.ID, &l.UserID, &l.NudgeType, &l.DedupKey, &l.Channel, &l.Title, &l.Body, &l.CopySource, &l.Segment, &l.SentAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func (r *NudgeHistoryRepository) RecordDispatch(ctx context.Context, record nudge.DispatchRecord) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO nudge_dispatch_log (id, user_id, nudge_type, dedup_key, channel, title, body, copy_source, segment, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (user_id, dedup_key) DO NOTHING
	`, record.ID, record.UserID, record.NudgeType, record.DedupKey, record.Channel, record.Title, record.Body, record.CopySource, record.Segment, record.SentAt)
	return err
}

func (r *NudgeHistoryRepository) GetEffectivenessStats(ctx context.Context, nudgeType string, segment string) (nudge.EffectivenessStats, error) {
	// Simple heuristic: 1.0 by default if small sample size
	// We could do a complex query here joining nudge_dispatch_log and nudge_outcomes.
	// For the sake of the MVP test, let's just return a stub.
	var sent, converted int
	err := r.db.QueryRowContext(ctx, `
		SELECT count(*), count(o.id)
		FROM nudge_dispatch_log d
		LEFT JOIN nudge_outcomes o ON o.dispatch_id = d.id
		WHERE d.nudge_type = $1 AND d.segment = $2
	`, nudgeType, segment).Scan(&sent, &converted)
	
	if err != nil || sent == 0 {
		return nudge.EffectivenessStats{ConversionRate: 1.0}, nil // Cold start safe
	}
	
	return nudge.EffectivenessStats{ConversionRate: float64(converted) / float64(sent)}, nil
}

func (r *NudgeHistoryRepository) GetLatestDispatchByType(ctx context.Context, userID uuid.UUID, nudgeType string) (*nudge.DispatchRecord, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, nudge_type, dedup_key, channel, title, body, copy_source, segment, sent_at
		FROM nudge_dispatch_log
		WHERE user_id = $1 AND nudge_type = $2
		ORDER BY sent_at DESC LIMIT 1
	`, userID, nudgeType)

	var l nudge.DispatchRecord
	if err := row.Scan(&l.ID, &l.UserID, &l.NudgeType, &l.DedupKey, &l.Channel, &l.Title, &l.Body, &l.CopySource, &l.Segment, &l.SentAt); err != nil {
		return nil, err
	}
	return &l, nil
}

// NudgesEnabled reads the user's nudges_enabled preference. Absence of a
// notification_preferences row means the user has never touched their
// settings, so it defaults to enabled (matches DefaultPreferences).
func (r *NudgeHistoryRepository) NudgesEnabled(ctx context.Context, userID uuid.UUID) (bool, error) {
	var enabled bool
	err := r.db.QueryRowContext(ctx, `
		SELECT nudges_enabled FROM notification_preferences WHERE user_id = $1
	`, userID).Scan(&enabled)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return enabled, nil
}

func (r *NudgeHistoryRepository) RecordOutcome(ctx context.Context, userID uuid.UUID, outcomeType string, occurredAt time.Time) error {
	// Finds the most recent nudge_dispatch_log row for that user within a 72h attribution window with no existing outcome row.
	row := r.db.QueryRowContext(ctx, `
		SELECT d.id, d.sent_at
		FROM nudge_dispatch_log d
		LEFT JOIN nudge_outcomes o ON o.dispatch_id = d.id AND o.outcome_type = $2
		WHERE d.user_id = $1 AND o.id IS NULL AND d.sent_at >= $3
		ORDER BY d.sent_at DESC LIMIT 1
	`, userID, outcomeType, occurredAt.Add(-72*time.Hour))
	
	var dispatchID uuid.UUID
	var sentAt time.Time
	if err := row.Scan(&dispatchID, &sentAt); err != nil {
		if err == sql.ErrNoRows {
			return nil // No attribution
		}
		return err
	}

	hoursAfter := occurredAt.Sub(sentAt).Hours()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO nudge_outcomes (id, dispatch_id, outcome_type, occurred_at, hours_after_dispatch)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New(), dispatchID, outcomeType, occurredAt, hoursAfter)
	return err
}
