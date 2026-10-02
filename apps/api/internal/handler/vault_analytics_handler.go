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

// DriftStateQuerier is the APYDriftDetector surface the handler needs to
// attach live drift state to the analytics response (#613).
type DriftStateQuerier interface {
	GetDriftState(ctx context.Context, vaultID uuid.UUID) (service.DriftState, error)
}

// VaultAnalyticsHandler serves GET /api/v1/vaults/{id}/analytics.
type VaultAnalyticsHandler struct {
	svc   VaultAnalyticsQuerier
	drift DriftStateQuerier
}

// NewVaultAnalyticsHandler builds a VaultAnalyticsHandler. drift may be nil
// (e.g. in tests, or if the drift detector isn't wired up), in which case
// the response's "drift" field is simply omitted.
func NewVaultAnalyticsHandler(svc VaultAnalyticsQuerier, drift DriftStateQuerier) *VaultAnalyticsHandler {
	return &VaultAnalyticsHandler{svc: svc, drift: drift}
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

	// Attach live drift state on a best-effort basis (#613): a failure here
	// (or no detector wired up at all) must not fail the whole analytics
	// response, since the historical metrics above are still valid.
	if h.drift != nil {
		if driftState, err := h.drift.GetDriftState(r.Context(), vaultID); err != nil {
			logpkg.FromContext(r.Context()).Warn("vault drift state unavailable", "vault_id", vaultID, "error", err.Error())
		} else {
			analytics.Drift = &driftState
		}
	}

	response.WriteJSON(w, http.StatusOK, response.OK(analytics))
}
