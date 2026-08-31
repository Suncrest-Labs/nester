package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsstreak"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type SavingsGamificationProgressReader interface {
	Progress(ctx context.Context, userID uuid.UUID) (savingsstreak.Progress, error)
}

type SavingsGamificationHandler struct {
	svc SavingsGamificationProgressReader
}

func NewSavingsGamificationHandler(svc SavingsGamificationProgressReader) *SavingsGamificationHandler {
	return &SavingsGamificationHandler{svc: svc}
}

func (h *SavingsGamificationHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users/savings-gamification/progress", h.progress)
}

func (h *SavingsGamificationHandler) progress(w http.ResponseWriter, r *http.Request) {
	userID, ok := gamificationUserID(w, r)
	if !ok {
		return
	}

	progress, err := h.svc.Progress(r.Context(), userID)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("get savings gamification progress failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(progress))
}

func gamificationUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
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
