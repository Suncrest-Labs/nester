package handler

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type PortfolioHandler struct {
	service *service.PortfolioService
}

// NewPortfolioHandler creates a new handler for portfolio endpoints.
func NewPortfolioHandler(service *service.PortfolioService) *PortfolioHandler {
	return &PortfolioHandler{service: service}
}

// Register wires the portfolio routes into the mux.
func (h *PortfolioHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/portfolio/summary", h.getSummary)
}

// getSummary returns the aggregated portfolio summary for the authenticated user.
func (h *PortfolioHandler) getSummary(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user id in token"))
		return
	}

	summary, err := h.service.GetUserPortfolioSummary(r.Context(), userID)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("portfolio handler getSummary failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(summary))
}
