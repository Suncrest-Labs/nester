package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// valuationServicer is the surface the handler depends on.
type valuationServicer interface {
	GetValuation(ctx context.Context, userID uuid.UUID) (portfolio.Valuation, error)
}

// ValuationHandler serves the real-time portfolio valuation endpoint (#832).
type ValuationHandler struct {
	svc valuationServicer
}

// NewValuationHandler constructs a ValuationHandler.
func NewValuationHandler(svc valuationServicer) *ValuationHandler {
	return &ValuationHandler{svc: svc}
}

// Register mounts the handler's routes on mux.
func (h *ValuationHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/portfolio/valuation", h.getValuation)
}

// getValuation handles GET /api/v1/portfolio/valuation. Retrieval is strictly
// scoped to the authenticated user — the valuation is computed only for their
// own vaults, goals, and rewards.
func (h *ValuationHandler) getValuation(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}
	userID, err := uuid.Parse(u.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid token subject"))
		return
	}

	val, err := h.svc.GetValuation(r.Context(), userID)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "VALUATION_FAILED", "could not compute valuation"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(val))
}
