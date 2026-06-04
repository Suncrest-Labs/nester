package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

type NotificationRepository struct {
	db *sql.DB
}

func NewNotificationRepository(db *sql.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) UpsertDeviceToken(ctx context.Context, userID uuid.UUID, token string, platform string) (notifications.DeviceToken, error) {
	const query = `
		INSERT INTO device_tokens (
			id, user_id, token, platform, enabled, created_at, updated_at, last_seen_at
		) VALUES ($1, $2, $3, $4, TRUE, NOW(), NOW(), NOW())
		ON CONFLICT (user_id, token)
		DO UPDATE SET
			platform = EXCLUDED.platform,
			enabled = TRUE,
			updated_at = NOW(),
			last_seen_at = NOW()
		RETURNING id, user_id, token, platform, enabled, created_at, updated_at, last_seen_at
	`

	return scanDeviceToken(r.db.QueryRowContext(ctx, query, uuid.New().String(), userID.String(), token, platform))
}

func (r *NotificationRepository) ListDeviceTokens(ctx context.Context, userID uuid.UUID) ([]notifications.DeviceToken, error) {
	const query = `
		SELECT id, user_id, token, platform, enabled, created_at, updated_at, last_seen_at
		FROM device_tokens
		WHERE user_id = $1 AND enabled = TRUE
		ORDER BY updated_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := make([]notifications.DeviceToken, 0)
	for rows.Next() {
		device, err := scanDeviceToken(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return devices, nil
}

func (r *NotificationRepository) Get(ctx context.Context, userID uuid.UUID) (notifications.Preferences, error) {
	const query = `
		SELECT email_enabled, websocket_enabled, push_enabled
		FROM notification_preferences
		WHERE user_id = $1
	`

	var prefs notifications.Preferences
	if err := r.db.QueryRowContext(ctx, query, userID.String()).Scan(&prefs.Email, &prefs.WebSocket, &prefs.Push); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notifications.DefaultPreferences(), nil
		}
		return notifications.Preferences{}, err
	}
	return prefs, nil
}

func (r *NotificationRepository) Set(ctx context.Context, userID uuid.UUID, prefs notifications.Preferences) (notifications.Preferences, error) {
	const query = `
		INSERT INTO notification_preferences (
			user_id, email_enabled, websocket_enabled, push_enabled, updated_at
		) VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (user_id)
		DO UPDATE SET
			email_enabled = EXCLUDED.email_enabled,
			websocket_enabled = EXCLUDED.websocket_enabled,
			push_enabled = EXCLUDED.push_enabled,
			updated_at = NOW()
		RETURNING email_enabled, websocket_enabled, push_enabled
	`

	var out notifications.Preferences
	if err := r.db.QueryRowContext(ctx, query, userID.String(), prefs.Email, prefs.WebSocket, prefs.Push).Scan(&out.Email, &out.WebSocket, &out.Push); err != nil {
		return notifications.Preferences{}, err
	}
	return out, nil
}

type deviceTokenScanner interface {
	Scan(dest ...any) error
}

func scanDeviceToken(row deviceTokenScanner) (notifications.DeviceToken, error) {
	var device notifications.DeviceToken
	if err := row.Scan(
		&device.ID,
		&device.UserID,
		&device.Token,
		&device.Platform,
		&device.Enabled,
		&device.CreatedAt,
		&device.UpdatedAt,
		&device.LastSeenAt,
	); err != nil {
		return notifications.DeviceToken{}, err
	}
	return device, nil
}
