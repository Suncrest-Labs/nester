package handler

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/services"
	"github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// RiskHandler handles risk-related HTTP requests
type RiskHandler struct {
	riskService *services.RiskService
}

// NewRiskHandler creates a new RiskHandler
func NewRiskHandler(riskService *services.RiskService) *RiskHandler {
	return &RiskHandler{
		riskService: riskService,
	}
}

// Register registers the risk routes on the given ServeMux
func (h *RiskHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/vaults/{id}/risk", h.getVaultRisk)
}

// getVaultRisk handles GET /api/v1/vaults/{id}/risk
func (h *RiskHandler) getVaultRisk(w http.ResponseWriter, r *http.Request) {
	// Extract vault ID from path
	idStr := r.PathValue("id")
	vaultID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid vault ID"))
		return
	}

	// Get risk score from service
	riskScore, err := h.riskService.Score(r.Context(), vaultID)
	if err != nil {
		if err.Error() == "empty vault: no allocations" {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
		logger.FromContext(r.Context()).Error("failed to get vault risk", "error", err.Error(), "vault_id", vaultID)
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	// Convert to response format
	responseData := map[string]interface{}{
		"overall":         riskScore.Overall,
		"tier":            riskScore.Tier,
		"concentration_risk": riskScore.ConcentrationRisk,
		"protocol_risk":   riskScore.ProtocolRisk,
		"yield_volatility": riskScore.YieldVolatility,
		"liquidity_risk":  riskScore.LiquidityRisk,
		"computed_at":     riskScore.ComputedAt.Format(time.RFC3339),
	}

	response.WriteJSON(w, http.StatusOK, response.OK(responseData))
}