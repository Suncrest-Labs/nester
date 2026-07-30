package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/goalnotification"
)

// GoalNotificationRepository persists per-goal notification preferences and
// queued digest items backing the mute/digest-frequency settings.
type GoalNotificationRepository struct {
	db *sql.DB
}

func NewGoalNotificationRepository(db *sql.DB) *GoalNotificationRepository {
	return &GoalNotificationRepository{db: db}
}

func (r *GoalNotificationRepository) Get(ctx context.Context, goalID uuid.UUID) (*goalnotification.Preference, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT goal_id, user_id, muted, digest_frequency, last_digest_sent_at, updated_at
		FROM savings_goal_notification_preferences
		WHERE goal_id = $1
	`, goalID)

	pref, err := scanGoalNotificationPreference(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &pref, nil
}

func (r *GoalNotificationRepository) Upsert(ctx context.Context, pref goalnotification.Preference) (goalnotification.Preference, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO savings_goal_notification_preferences (goal_id, user_id, muted, digest_frequency, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (goal_id)
		DO UPDATE SET muted = EXCLUDED.muted, digest_frequency = EXCLUDED.digest_frequency, updated_at = NOW()
		RETURNING goal_id, user_id, muted, digest_frequency, last_digest_sent_at, updated_at
	`, pref.GoalID, pref.UserID, pref.Muted, string(pref.DigestFrequency))

	out, err := scanGoalNotificationPreference(row)
	if err != nil {
		return goalnotification.Preference{}, err
	}
	return out, nil
}

func (r *GoalNotificationRepository) EnqueueDigestItem(ctx context.Context, item goalnotification.DigestItem) error {
	payload, err := json.Marshal(item.Payload)
	if err != nil {
		return err
	}
	id := item.ID
	if id == uuid.Nil {
		id = uuid.New()
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO goal_notification_digest_queue (id, goal_id, user_id, title, body, payload)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
	`, id, item.GoalID, item.UserID, item.Title, item.Body, payload)
	return err
}

// ListDue returns preferences with a non-immediate frequency that have at
// least one queued digest item and whose cadence interval has elapsed since
// their last flush (or have never been flushed).
func (r *GoalNotificationRepository) ListDue(ctx context.Context, now time.Time) ([]goalnotification.Preference, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT p.goal_id, p.user_id, p.muted, p.digest_frequency, p.last_digest_sent_at, p.updated_at
		FROM savings_goal_notification_preferences p
		WHERE p.digest_frequency IN ('daily', 'weekly')
		  AND NOT p.muted
		  AND EXISTS (SELECT 1 FROM goal_notification_digest_queue q WHERE q.goal_id = p.goal_id)
		  AND (
		    p.last_digest_sent_at IS NULL
		    OR (p.digest_frequency = 'daily' AND p.last_digest_sent_at <= $1::timestamptz - INTERVAL '1 day')
		    OR (p.digest_frequency = 'weekly' AND p.last_digest_sent_at <= $1::timestamptz - INTERVAL '7 days')
		  )
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []goalnotification.Preference
	for rows.Next() {
		pref, err := scanGoalNotificationPreference(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pref)
	}
	return out, rows.Err()
}

func (r *GoalNotificationRepository) ListQueuedItems(ctx context.Context, goalID uuid.UUID) ([]goalnotification.DigestItem, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, goal_id, user_id, title, body, payload, created_at
		FROM goal_notification_digest_queue
		WHERE goal_id = $1
		ORDER BY created_at ASC
	`, goalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []goalnotification.DigestItem
	for rows.Next() {
		var (
			item        goalnotification.DigestItem
			payloadJSON []byte
		)
		if err := rows.Scan(&item.ID, &item.GoalID, &item.UserID, &item.Title, &item.Body, &payloadJSON, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(payloadJSON) > 0 {
			_ = json.Unmarshal(payloadJSON, &item.Payload)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *GoalNotificationRepository) ClearQueuedItems(ctx context.Context, goalID uuid.UUID, itemIDs []uuid.UUID) error {
	if len(itemIDs) == 0 {
		return nil
	}
	ids := make([]string, len(itemIDs))
	for i, id := range itemIDs {
		ids[i] = id.String()
	}
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM goal_notification_digest_queue WHERE goal_id = $1 AND id = ANY($2::uuid[])
	`, goalID, pq.Array(ids))
	return err
}

func (r *GoalNotificationRepository) MarkDigestSent(ctx context.Context, goalID uuid.UUID, sentAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE savings_goal_notification_preferences SET last_digest_sent_at = $2, updated_at = NOW() WHERE goal_id = $1
	`, goalID, sentAt)
	return err
}

type goalNotificationScanner interface {
	Scan(dest ...any) error
}

func scanGoalNotificationPreference(row goalNotificationScanner) (goalnotification.Preference, error) {
	var (
		pref             goalnotification.Preference
		frequency        string
		lastDigestSentAt sql.NullTime
	)
	if err := row.Scan(&pref.GoalID, &pref.UserID, &pref.Muted, &frequency, &lastDigestSentAt, &pref.UpdatedAt); err != nil {
		return goalnotification.Preference{}, err
	}
	pref.DigestFrequency = goalnotification.Frequency(frequency)
	if lastDigestSentAt.Valid {
		t := lastDigestSentAt.Time
		pref.LastDigestSentAt = &t
	}
	return pref, nil
}
