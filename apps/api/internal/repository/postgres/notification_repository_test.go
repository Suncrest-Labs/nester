package postgres

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

func TestNotificationRepository_UpsertDeviceToken(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewNotificationRepository(db)
	userID := uuid.New()
	tokenID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs(sqlmock.AnyArg(), userID.String(), "ExponentPushToken[abc]", "expo").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "token", "platform", "enabled", "created_at", "updated_at", "last_seen_at",
		}).AddRow(tokenID.String(), userID.String(), "ExponentPushToken[abc]", "expo", true, now, now, now))

	device, err := repo.UpsertDeviceToken(context.Background(), userID, "ExponentPushToken[abc]", "expo")
	if err != nil {
		t.Fatalf("UpsertDeviceToken: %v", err)
	}
	if device.ID != tokenID || device.UserID != userID || device.Token != "ExponentPushToken[abc]" || !device.Enabled {
		t.Fatalf("device = %+v", device)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestNotificationRepository_ListDeviceTokens(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewNotificationRepository(db)
	userID := uuid.New()
	tokenID := uuid.New()
	now := time.Now().UTC()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT id, user_id, token, platform, enabled, created_at, updated_at, last_seen_at
		FROM device_tokens
		WHERE user_id = $1 AND enabled = TRUE
		ORDER BY updated_at DESC
	`)).
		WithArgs(userID.String()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "token", "platform", "enabled", "created_at", "updated_at", "last_seen_at",
		}).AddRow(tokenID.String(), userID.String(), "fcm-token", "ios", true, now, now, now))

	devices, err := repo.ListDeviceTokens(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListDeviceTokens: %v", err)
	}
	if len(devices) != 1 || devices[0].Token != "fcm-token" {
		t.Fatalf("devices = %+v", devices)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestNotificationRepository_GetPreferencesDefaultsWhenMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewNotificationRepository(db)
	userID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT email_enabled, websocket_enabled, push_enabled, digest_cadence
		FROM notification_preferences
		WHERE user_id = $1
	`)).
		WithArgs(userID.String()).
		WillReturnError(sql.ErrNoRows)

	prefs, err := repo.Get(context.Background(), userID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if prefs != notifications.DefaultPreferences() {
		t.Fatalf("prefs = %+v, want %+v", prefs, notifications.DefaultPreferences())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestNotificationRepository_SetPreferences(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewNotificationRepository(db)
	userID := uuid.New()
	want := notifications.Preferences{Email: true, WebSocket: false, Push: false, DigestCadence: "weekly"}

	mock.ExpectQuery(regexp.QuoteMeta(`
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
	`)).
		WithArgs(userID.String(), true, false, false, "weekly").
		WillReturnRows(sqlmock.NewRows([]string{"email_enabled", "websocket_enabled", "push_enabled", "digest_cadence"}).
			AddRow(true, false, false, "weekly"))

	prefs, err := repo.Set(context.Background(), userID, want)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if prefs != want {
		t.Fatalf("prefs = %+v, want %+v", prefs, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}

func TestNotificationRepository_SetPreferencesRejectsInvalidCadence(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewNotificationRepository(db)
	_, err = repo.Set(context.Background(), uuid.New(), notifications.Preferences{DigestCadence: "daily"})
	if err == nil {
		t.Fatal("expected error for invalid digest_cadence")
	}
}

func TestNotificationRepository_ListUserIDsForDigestCadence(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	repo := NewNotificationRepository(db)
	userID := uuid.New()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT u.id
		FROM users u
		LEFT JOIN notification_preferences np ON np.user_id = u.id
		WHERE COALESCE(np.digest_cadence, $2) = $1
	`)).
		WithArgs("monthly", "monthly").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID.String()))

	ids, err := repo.ListUserIDsForDigestCadence(context.Background(), "monthly")
	if err != nil {
		t.Fatalf("ListUserIDsForDigestCadence: %v", err)
	}
	if len(ids) != 1 || ids[0] != userID {
		t.Fatalf("ids = %+v", ids)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectations: %v", err)
	}
}
