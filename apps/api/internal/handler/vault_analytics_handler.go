package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// VaultAnalyticsQuerier is the service surface the handler depends on.
type VaultAnalyticsQuerier interface {
	Compute(ctx context.Context, vaultID uuid.UUID, period string) (service.VaultAnalytics, error)
}

// VaultAnalyticsHandler serves GET /api/v1/vaults/{id}/analytics.
type VaultAnalyticsHandler struct {
	svc VaultAnalyticsQuerier
}

func NewVaultAnalyticsHandler(svc VaultAnalyticsQuerier) *VaultAnalyticsHandler {
	return &VaultAnalyticsHandler{svc: svc}
}

func (h *VaultAnalyticsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/vaults/{id}/analytics", h.analytics)
}

func (h *VaultAnalyticsHandler) analytics(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
	if period == "" {
		period = "30d"
	}

	// Accept 30d, 90d, 1y.
	switch period {
	case "30d", "90d":
	case "1y":
		period = "365d"
	default:
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("period must be 30d, 90d, or 1y"))
		return
	}

	analytics, err := h.svc.Compute(r.Context(), vaultID, period)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("vault analytics compute failed", "vault_id", vaultID, "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "analytics computation failed"))
		return
	}

	// Normalise period label in response back to the canonical form.
	if period == "365d" {
		analytics.Period = "1y"
	}

	response.WriteJSON(w, http.StatusOK, response.OK(analytics))
}
