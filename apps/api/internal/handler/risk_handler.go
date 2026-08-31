package handler

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/services"
	"github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type RiskHandler struct {
	riskService *services.RiskService
}

func NewRiskHandler(riskService *services.RiskService) *RiskHandler {
	return &RiskHandler{
		riskService: riskService,
	}
}

func (h *RiskHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/vaults/{id}/risk", h.getVaultRisk)
	mux.HandleFunc("GET /api/v1/vaults/{id}/risk/history", h.getRiskHistory)
	mux.HandleFunc("POST /api/v1/vaults/{id}/risk/refresh", h.refreshRiskScore)
}

func (h *RiskHandler) getVaultRisk(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	vaultID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid vault ID"))
		return
	}

	riskScore, err := h.riskService.Score(r.Context(), vaultID)
	if err != nil {
		if err == services.ErrEmptyVault {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
		logger.FromContext(r.Context()).Error("failed to get vault risk", "error", err.Error(), "vault_id", vaultID)
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(riskScore))
}

func (h *RiskHandler) getRiskHistory(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	vaultID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid vault ID"))
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil {
			limit = parsed
		}
	}

	history, err := h.riskService.GetHistory(r.Context(), vaultID, limit)
	if err != nil {
		logger.FromContext(r.Context()).Error("failed to get risk history", "error", err.Error(), "vault_id", vaultID)
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(history))
}

func (h *RiskHandler) refreshRiskScore(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	vaultID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid vault ID"))
		return
	}

	riskScore, err := h.riskService.ScoreOnDemand(r.Context(), vaultID)
	if err != nil {
		if err == services.ErrEmptyVault {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
		logger.FromContext(r.Context()).Error("failed to refresh risk score", "error", err.Error(), "vault_id", vaultID)
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(riskScore))
}