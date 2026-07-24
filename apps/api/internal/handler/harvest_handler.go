package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/harvest"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// harvestStatusServicer is the surface the handler depends on.
type harvestStatusServicer interface {
	VaultStatusForUser(ctx context.Context, vaultID, userID uuid.UUID) (harvest.Status, error)
}

// HarvestHandler serves the user-facing harvest status endpoint (#845).
type HarvestHandler struct {
	svc harvestStatusServicer
}

// NewHarvestHandler constructs a HarvestHandler.
func NewHarvestHandler(svc harvestStatusServicer) *HarvestHandler {
	return &HarvestHandler{svc: svc}
}

// Register mounts the handler's routes on mux.
func (h *HarvestHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/vaults/{id}/harvest/status", h.Status)
}

// Status handles GET /api/v1/vaults/{id}/harvest/status, returning pending
// yield, harvest threshold, and estimated next harvest for the owner's vault.
func (h *HarvestHandler) Status(w http.ResponseWriter, r *http.Request) {
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
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid vault id"))
		return
	}

	status, err := h.svc.VaultStatusForUser(r.Context(), vaultID, userID)
	if err != nil {
		switch {
		case errors.Is(err, harvest.ErrForbidden):
			response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "vault does not belong to user"))
		default:
			response.WriteJSON(w, http.StatusNotFound, response.Err(http.StatusNotFound, "NOT_FOUND", "vault not found"))
		}
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(status))
}
