package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// yieldHarvestServicer is the service surface the handler depends on.
type yieldHarvestServicer interface {
	ListHarvests(ctx context.Context, input service.ListYieldHarvestsInput) (service.ListYieldHarvestsOutput, error)
}

// YieldHarvestHandler serves GET /api/v1/yields/harvests.
type YieldHarvestHandler struct {
	svc yieldHarvestServicer
}

// NewYieldHarvestHandler constructs a YieldHarvestHandler.
func NewYieldHarvestHandler(svc yieldHarvestServicer) *YieldHarvestHandler {
	return &YieldHarvestHandler{svc: svc}
}

// Register mounts the handler's routes on mux.
func (h *YieldHarvestHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/yields/harvests", h.ListHarvests)
}

// ListHarvests handles GET /api/v1/yields/harvests.
func (h *YieldHarvestHandler) ListHarvests(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid token subject"))
		return
	}

	q := r.URL.Query()

	limit := 20
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("limit must be a positive integer"))
			return
		}
		if v > 100 {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("limit must not exceed 100"))
			return
		}
		limit = v
	}

	// Optional vault scoping. The harvest modal asks only for the vault the
	// user is about to harvest, so results are not diluted by harvests made in
	// other vaults (nester#949).
	var vaultFilter *uuid.UUID
	if raw := q.Get("vault_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault_id must be a valid UUID"))
			return
		}
		vaultFilter = &parsed
	}

	out, err := h.svc.ListHarvests(r.Context(), service.ListYieldHarvestsInput{
		UserID:  userID,
		VaultID: vaultFilter,
		Cursor:  q.Get("cursor"),
		Limit:   limit,
	})
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list yield harvests"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(out))
}
