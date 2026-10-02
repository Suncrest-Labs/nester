package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/systemstate"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// systemStateRepo is the narrow persistence port MaintenanceHandler needs.
type systemStateRepo interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string) error
}

// MaintenanceHandler exposes admin endpoints to read and set the system-wide
// maintenance mode (#1328): "" (normal), "readonly" (GETs allowed, mutations
// blocked), or "halt" (everything blocked except public routes).
type MaintenanceHandler struct {
	repo systemStateRepo
}

// NewMaintenanceHandler constructs a MaintenanceHandler.
func NewMaintenanceHandler(repo systemStateRepo) *MaintenanceHandler {
	return &MaintenanceHandler{repo: repo}
}

// Register registers the maintenance routes on the given ServeMux.
func (h *MaintenanceHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/maintenance", h.getMode)
	mux.HandleFunc("PUT /api/v1/admin/maintenance", h.setMode)
}

func (h *MaintenanceHandler) getMode(w http.ResponseWriter, r *http.Request) {
	mode, err := h.repo.Get(r.Context(), systemstate.KeyMaintenanceMode)
	if err != nil {
		mode = systemstate.ModeOff
	}
	response.WriteJSON(w, http.StatusOK, response.OK(map[string]string{"mode": mode}))
}

func (h *MaintenanceHandler) setMode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}

	switch req.Mode {
	case systemstate.ModeOff, systemstate.ModeReadOnly, systemstate.ModeHalt:
	default:
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("mode must be one of: '', 'readonly', 'halt'"))
		return
	}

	if err := h.repo.Set(r.Context(), systemstate.KeyMaintenanceMode, req.Mode); err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to set maintenance mode"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(map[string]string{"mode": req.Mode}))
}
