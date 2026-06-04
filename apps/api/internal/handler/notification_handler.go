package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/notifications"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type NotificationSettingsStore interface {
	UpsertDeviceToken(ctx context.Context, userID uuid.UUID, token string, platform string) (notifications.DeviceToken, error)
	Get(ctx context.Context, userID uuid.UUID) (notifications.Preferences, error)
	Set(ctx context.Context, userID uuid.UUID, prefs notifications.Preferences) (notifications.Preferences, error)
}

type NotificationHandler struct {
	store NotificationSettingsStore
}

func NewNotificationHandler(store NotificationSettingsStore) *NotificationHandler {
	return &NotificationHandler{store: store}
}

func (h *NotificationHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/users/device-tokens", h.RegisterDeviceToken)
	mux.HandleFunc("GET /api/v1/users/notification-preferences", h.GetPreferences)
	mux.HandleFunc("PATCH /api/v1/users/notification-preferences", h.UpdatePreferences)
}

type registerDeviceTokenRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

func (h *NotificationHandler) RegisterDeviceToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := notificationUserID(w, r)
	if !ok {
		return
	}

	var req registerDeviceTokenRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	token := strings.TrimSpace(req.Token)
	if token == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("token is required"))
		return
	}

	platform := strings.ToLower(strings.TrimSpace(req.Platform))
	if platform == "" {
		platform = "unknown"
	}

	device, err := h.store.UpsertDeviceToken(r.Context(), userID, token, platform)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("register device token failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusCreated, response.Created(device))
}

func (h *NotificationHandler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := notificationUserID(w, r)
	if !ok {
		return
	}

	prefs, err := h.store.Get(r.Context(), userID)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("get notification preferences failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(prefs))
}

type updateNotificationPreferencesRequest struct {
	Email     *bool `json:"email"`
	WebSocket *bool `json:"websocket"`
	Push      *bool `json:"push"`
}

func (h *NotificationHandler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID, ok := notificationUserID(w, r)
	if !ok {
		return
	}

	var req updateNotificationPreferencesRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	prefs, err := h.store.Get(r.Context(), userID)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("get notification preferences failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}
	if req.Email != nil {
		prefs.Email = *req.Email
	}
	if req.WebSocket != nil {
		prefs.WebSocket = *req.WebSocket
	}
	if req.Push != nil {
		prefs.Push = *req.Push
	}

	prefs, err = h.store.Set(r.Context(), userID, prefs)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("update notification preferences failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(prefs))
}

func notificationUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return uuid.Nil, false
	}

	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user identity"))
		return uuid.Nil, false
	}

	return userID, true
}
