package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type APYHistoryProvider interface {
	GetHistory(ctx context.Context, slug string) (*service.APYHistoryResponse, error)
}

type APYHandler struct {
	svc APYHistoryProvider
}

func NewAPYHandler(svc APYHistoryProvider) *APYHandler {
	return &APYHandler{svc: svc}
}

func (h *APYHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/yields/{protocol_slug}/apy-history", h.getAPYHistory)
}

func (h *APYHandler) getAPYHistory(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("protocol_slug")
	if slug == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("protocol_slug is required"))
		return
	}

	history, err := h.svc.GetHistory(r.Context(), slug)
	if err != nil {
		if errors.Is(err, apysnapshot.ErrProtocolNotFound) {
			response.WriteJSON(w, http.StatusNotFound, response.NotFound("protocol"))
			return
		}
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to retrieve APY history"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(history))
}
