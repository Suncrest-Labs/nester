package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
)

type ActivityEventRepository struct {
	db *sql.DB
}

func NewActivityEventRepository(db *sql.DB) *ActivityEventRepository {
	return &ActivityEventRepository{db: db}
}

func (r *ActivityEventRepository) RecordEvent(ctx context.Context, userID uuid.UUID, eventType usersignal.EventType, occurredAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO activity_events (user_id, event_type, occurred_at)
		VALUES ($1, $2, $3)
	`, userID, eventType, occurredAt)
	return err
}

// DeleteOlderThan permanently removes activity_events rows whose occurred_at
// is before cutoff (nester#1226 retention policy). Returns the number of
// rows removed so the caller can audit-log the deletion with a real count.
func (r *ActivityEventRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM activity_events WHERE occurred_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *ActivityEventRepository) ListRecentEvents(ctx context.Context, userID uuid.UUID, since time.Time) ([]usersignal.ActivityEvent, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, event_type, occurred_at
		FROM activity_events
		WHERE user_id = $1 AND occurred_at >= $2
		ORDER BY occurred_at DESC
	`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []usersignal.ActivityEvent
	for rows.Next() {
		var e usersignal.ActivityEvent
		if err := rows.Scan(&e.ID, &e.UserID, &e.EventType, &e.OccurredAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}
