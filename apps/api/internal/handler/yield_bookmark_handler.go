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
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type YieldBookmarkManager interface {
	Add(ctx context.Context, userID uuid.UUID, protocolSlug string) (service.YieldBookmark, error)
	Remove(ctx context.Context, userID uuid.UUID, protocolSlug string) error
	ListWithStats(ctx context.Context, userID uuid.UUID, chain string) ([]service.YieldBookmarkWithStats, error)
}

// YieldBookmarkHandler serves /api/v1/yields/bookmarks.
type YieldBookmarkHandler struct {
	svc YieldBookmarkManager
}

func NewYieldBookmarkHandler(svc YieldBookmarkManager) *YieldBookmarkHandler {
	return &YieldBookmarkHandler{svc: svc}
}

func (h *YieldBookmarkHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/yields/bookmarks", h.add)
	mux.HandleFunc("GET /api/v1/yields/bookmarks", h.list)
	mux.HandleFunc("DELETE /api/v1/yields/bookmarks/{protocol_slug}", h.remove)
}

type addYieldBookmarkRequest struct {
	ProtocolSlug string `json:"protocol_slug"`
}

func (h *YieldBookmarkHandler) add(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	var req addYieldBookmarkRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}
	bm, err := h.svc.Add(r.Context(), userID, req.ProtocolSlug)
	if err != nil {
		if errors.Is(err, service.ErrYieldBookmarkDuplicate) {
			response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "DUPLICATE", "protocol already bookmarked"))
			return
		}
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(bm))
}

func (h *YieldBookmarkHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	chain := strings.TrimSpace(r.URL.Query().Get("chain"))
	if chain == "" {
		chain = "Stellar"
	}
	items, err := h.svc.ListWithStats(r.Context(), userID, chain)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("list yield bookmarks failed", "error", err.Error())
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UPSTREAM_ERROR", "yield data temporarily unavailable"))
		return
	}
	if items == nil {
		items = []service.YieldBookmarkWithStats{}
	}
	response.WriteJSON(w, http.StatusOK, response.OK(items))
}

func (h *YieldBookmarkHandler) remove(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.userID(w, r)
	if !ok {
		return
	}
	slug := r.PathValue("protocol_slug")
	if err := h.svc.Remove(r.Context(), userID, slug); err != nil {
		if errors.Is(err, service.ErrYieldBookmarkNotFound) {
			response.WriteJSON(w, http.StatusNotFound, response.NotFound("yield bookmark"))
			return
		}
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *YieldBookmarkHandler) userID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
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
