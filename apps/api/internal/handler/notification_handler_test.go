package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
)

type memoryNotificationSettings struct {
	tokenUserID uuid.UUID
	token       string
	platform    string
	prefs       map[uuid.UUID]notifications.Preferences
	err         error
}

func newMemoryNotificationSettings() *memoryNotificationSettings {
	return &memoryNotificationSettings{prefs: map[uuid.UUID]notifications.Preferences{}}
}

func (m *memoryNotificationSettings) UpsertDeviceToken(_ context.Context, userID uuid.UUID, token, platform string) (notifications.DeviceToken, error) {
	if m.err != nil {
		return notifications.DeviceToken{}, m.err
	}
	m.tokenUserID = userID
	m.token = token
	m.platform = platform
	return notifications.DeviceToken{
		ID:       uuid.New(),
		UserID:   userID,
		Token:    token,
		Platform: platform,
		Enabled:  true,
	}, nil
}

func (m *memoryNotificationSettings) Get(_ context.Context, userID uuid.UUID) (notifications.Preferences, error) {
	if m.err != nil {
		return notifications.Preferences{}, m.err
	}
	if p, ok := m.prefs[userID]; ok {
		return p, nil
	}
	return notifications.DefaultPreferences(), nil
}

func (m *memoryNotificationSettings) Set(_ context.Context, userID uuid.UUID, prefs notifications.Preferences) (notifications.Preferences, error) {
	if m.err != nil {
		return notifications.Preferences{}, m.err
	}
	m.prefs[userID] = prefs
	return prefs, nil
}

func TestNotificationHandler_RegisterDeviceTokenUsesAuthenticatedUser(t *testing.T) {
	store := newMemoryNotificationSettings()
	handler := NewNotificationHandler(store)
	userID := uuid.New()

	mux := http.NewServeMux()
	handler.Register(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/users/device-tokens",
		bytes.NewBufferString(`{"token":"  ExponentPushToken[abc]  ","platform":"expo"}`),
	)
	req = req.WithContext(auth.NewContext(req.Context(), auth.User{ID: userID.String()}))
	res := httptest.NewRecorder()

	mux.ServeHTTP(res, req)

	if res.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusCreated, res.Body.String())
	}
	if store.tokenUserID != userID {
		t.Fatalf("stored user ID = %s, want %s", store.tokenUserID, userID)
	}
	if store.token != "ExponentPushToken[abc]" {
		t.Fatalf("stored token = %q", store.token)
	}
	if store.platform != "expo" {
		t.Fatalf("stored platform = %q", store.platform)
	}
}

func TestNotificationHandler_RegisterDeviceTokenRejectsBlankToken(t *testing.T) {
	handler := NewNotificationHandler(newMemoryNotificationSettings())
	userID := uuid.New()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/users/device-tokens", bytes.NewBufferString(`{"token":"   ","platform":"ios"}`))
	req = req.WithContext(auth.NewContext(req.Context(), auth.User{ID: userID.String()}))
	res := httptest.NewRecorder()

	handler.RegisterDeviceToken(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusBadRequest, res.Body.String())
	}
}

func TestNotificationHandler_UpdatePreferences(t *testing.T) {
	store := newMemoryNotificationSettings()
	handler := NewNotificationHandler(store)
	userID := uuid.New()

	req := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/users/notification-preferences",
		bytes.NewBufferString(`{"push":false,"email":true,"websocket":false}`),
	)
	req = req.WithContext(auth.NewContext(req.Context(), auth.User{ID: userID.String()}))
	res := httptest.NewRecorder()

	handler.UpdatePreferences(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	if got := store.prefs[userID]; got.Push || !got.Email || got.WebSocket {
		t.Fatalf("stored prefs = %+v", got)
	}

	var body struct {
		Data notifications.Preferences `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Push || !body.Data.Email || body.Data.WebSocket {
		t.Fatalf("response prefs = %+v", body.Data)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw["data"], &data); err != nil {
		t.Fatalf("decode raw data: %v", err)
	}
	for _, key := range []string{"email", "websocket", "push"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("response data missing lower-case key %q: %s", key, res.Body.String())
		}
	}
}

func TestNotificationHandler_StoreErrorReturnsInternalError(t *testing.T) {
	store := newMemoryNotificationSettings()
	store.err = errors.New("database down")
	handler := NewNotificationHandler(store)
	userID := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/notification-preferences", nil)
	req = req.WithContext(auth.NewContext(req.Context(), auth.User{ID: userID.String()}))
	res := httptest.NewRecorder()

	handler.GetPreferences(res, req)

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", res.Code, http.StatusInternalServerError, res.Body.String())
	}
}
