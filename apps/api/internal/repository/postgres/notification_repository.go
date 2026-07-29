package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

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
		SELECT email_enabled, websocket_enabled, push_enabled, digest_cadence
		FROM notification_preferences
		WHERE user_id = $1
	`

	var prefs notifications.Preferences
	if err := r.db.QueryRowContext(ctx, query, userID.String()).Scan(
		&prefs.Email, &prefs.WebSocket, &prefs.Push, &prefs.DigestCadence,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notifications.DefaultPreferences(), nil
		}
		return notifications.Preferences{}, err
	}
	return prefs, nil
}

func (r *NotificationRepository) Set(ctx context.Context, userID uuid.UUID, prefs notifications.Preferences) (notifications.Preferences, error) {
	if prefs.DigestCadence == "" {
		prefs.DigestCadence = notifications.DigestCadenceMonthly
	}
	if !notifications.ValidDigestCadence(prefs.DigestCadence) {
		return notifications.Preferences{}, fmt.Errorf("invalid digest_cadence %q", prefs.DigestCadence)
	}

	const query = `
		INSERT INTO notification_preferences (
			user_id, email_enabled, websocket_enabled, push_enabled, digest_cadence, updated_at
		) VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id)
		DO UPDATE SET
			email_enabled = EXCLUDED.email_enabled,
			websocket_enabled = EXCLUDED.websocket_enabled,
			push_enabled = EXCLUDED.push_enabled,
			digest_cadence = EXCLUDED.digest_cadence,
			updated_at = NOW()
		RETURNING email_enabled, websocket_enabled, push_enabled, digest_cadence
	`

	var out notifications.Preferences
	if err := r.db.QueryRowContext(
		ctx, query, userID.String(), prefs.Email, prefs.WebSocket, prefs.Push, prefs.DigestCadence,
	).Scan(&out.Email, &out.WebSocket, &out.Push, &out.DigestCadence); err != nil {
		return notifications.Preferences{}, err
	}
	return out, nil
}

// GetForCategory implements notifications.CategoryPreferenceStore (#829):
// it reads the per-category override stored in the category_overrides JSONB
// column (see migration 069) and falls back to
// notifications.DefaultPreferencesForCategory when the user has no row, or
// no override for this specific category — the Go-side default is the
// single source of truth, not a SQL default, so there is only one place
// that logic lives.
func (r *NotificationRepository) GetForCategory(ctx context.Context, userID uuid.UUID, category notifications.Category) (notifications.Preferences, error) {
	const query = `
		SELECT category_overrides -> $2
		FROM notification_preferences
		WHERE user_id = $1
	`

	var raw sql.NullString
	if err := r.db.QueryRowContext(ctx, query, userID.String(), string(category)).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return notifications.DefaultPreferencesForCategory(category), nil
		}
		return notifications.Preferences{}, err
	}
	if !raw.Valid || raw.String == "" || raw.String == "null" {
		return notifications.DefaultPreferencesForCategory(category), nil
	}

	var prefs notifications.Preferences
	if err := json.Unmarshal([]byte(raw.String), &prefs); err != nil {
		return notifications.Preferences{}, fmt.Errorf("notification_repository: unmarshal category override for %s/%s: %w", userID, category, err)
	}
	return prefs, nil
}

// SetCategoryOverride implements the write side of #829's per-category
// preferences: it merges category's entry into the existing
// category_overrides JSONB (creating the user's row if it doesn't exist
// yet) without disturbing any other category's override.
func (r *NotificationRepository) SetCategoryOverride(ctx context.Context, userID uuid.UUID, category notifications.Category, prefs notifications.Preferences) error {
	raw, err := json.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("notification_repository: marshal category override: %w", err)
	}

	const query = `
		INSERT INTO notification_preferences (user_id, category_overrides, updated_at)
		VALUES ($1, jsonb_build_object($2::text, $3::jsonb), NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			category_overrides = notification_preferences.category_overrides || jsonb_build_object($2::text, $3::jsonb),
			updated_at = NOW()
	`
	_, err = r.db.ExecContext(ctx, query, userID.String(), string(category), string(raw))
	return err
}

// ListUserIDsForDigestCadence returns every user whose effective digest
// cadence equals the given value, including users with no preferences row
// yet (they get the DefaultPreferences() cadence). Used by the digest
// scheduler (#859) to find who is due this tick.
func (r *NotificationRepository) ListUserIDsForDigestCadence(ctx context.Context, cadence string) ([]uuid.UUID, error) {
	const query = `
		SELECT u.id
		FROM users u
		LEFT JOIN notification_preferences np ON np.user_id = u.id
		WHERE COALESCE(np.digest_cadence, $2) = $1
	`

	rows, err := r.db.QueryContext(ctx, query, cadence, notifications.DigestCadenceMonthly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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
