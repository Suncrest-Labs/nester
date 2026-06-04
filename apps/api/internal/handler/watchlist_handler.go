package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// WatchlistManager is the service surface the handler depends on.
type WatchlistManager interface {
	Add(ctx context.Context, userID uuid.UUID, req service.AddWatchlistItemRequest) (service.WatchlistItem, error)
	List(ctx context.Context, userID uuid.UUID) ([]service.WatchlistItem, error)
	Delete(ctx context.Context, userID, itemID uuid.UUID) error
}

// WatchlistHandler serves the /api/v1/users/watchlist endpoints.
type WatchlistHandler struct {
	svc WatchlistManager
}

func NewWatchlistHandler(svc WatchlistManager) *WatchlistHandler {
	return &WatchlistHandler{svc: svc}
}

func (h *WatchlistHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users/watchlist", h.list)
	mux.HandleFunc("POST /api/v1/users/watchlist", h.add)
	mux.HandleFunc("DELETE /api/v1/users/watchlist/{id}", h.delete)
}

func (h *WatchlistHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}

	items, err := h.svc.List(r.Context(), userID)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("watchlist list failed", "user_id", userID, "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(items))
}

func (h *WatchlistHandler) add(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("failed to read request body"))
		return
	}

	var req service.AddWatchlistItemRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}

	item, err := h.svc.Add(r.Context(), userID, req)
	if err != nil {
		if errors.Is(err, service.ErrWatchlistDuplicate) {
			response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "DUPLICATE", "pool already in watchlist"))
			return
		}
		logpkg.FromContext(r.Context()).Error("watchlist add failed", "user_id", userID, "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusCreated, response.Created(item))
}

func (h *WatchlistHandler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}

	itemID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("watchlist item id must be a valid UUID"))
		return
	}

	if err := h.svc.Delete(r.Context(), userID, itemID); err != nil {
		if errors.Is(err, service.ErrWatchlistItemNotFound) {
			response.WriteJSON(w, http.StatusNotFound, response.NotFound("watchlist item"))
			return
		}
		logpkg.FromContext(r.Context()).Error("watchlist delete failed", "user_id", userID, "item_id", itemID, "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *WatchlistHandler) authenticatedUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user identity"))
		return uuid.Nil, false
	}
	return id, true
}
