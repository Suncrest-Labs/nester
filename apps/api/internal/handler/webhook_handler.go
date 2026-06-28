package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/webhook"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type WebhookManager interface {
	Register(ctx context.Context, userID uuid.UUID, in service.RegisterWebhookInput) (webhook.Webhook, error)
	List(ctx context.Context, userID uuid.UUID) ([]webhook.Webhook, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

type WebhookHandler struct {
	svc WebhookManager
}

func NewWebhookHandler(svc WebhookManager) *WebhookHandler {
	return &WebhookHandler{svc: svc}
}

func (h *WebhookHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/webhooks", h.create)
	mux.HandleFunc("GET /api/v1/webhooks", h.list)
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", h.delete)
}

type registerWebhookRequest struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

func (h *WebhookHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := webhookAuthenticatedUserID(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("could not read request body"))
		return
	}
	var req registerWebhookRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("url is required"))
		return
	}
	wh, err := h.svc.Register(r.Context(), userID, service.RegisterWebhookInput{
		URL:    req.URL,
		Secret: req.Secret,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(wh))
}

func (h *WebhookHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := webhookAuthenticatedUserID(w, r)
	if !ok {
		return
	}
	hooks, err := h.svc.List(r.Context(), userID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if hooks == nil {
		hooks = []webhook.Webhook{}
	}
	response.WriteJSON(w, http.StatusOK, response.OK(hooks))
}

func (h *WebhookHandler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := webhookAuthenticatedUserID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("webhook id must be a valid UUID"))
		return
	}
	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WebhookHandler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, webhook.ErrWebhookNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("webhook"))
	case errors.Is(err, webhook.ErrInvalidWebhook):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}

func webhookAuthenticatedUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return uuid.UUID{}, false
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "invalid user id"))
		return uuid.UUID{}, false
	}
	return userID, true
}
