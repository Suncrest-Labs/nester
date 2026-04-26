package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type IntelligenceHandler struct {
	prometheus *service.PrometheusClient
}

func NewIntelligenceHandler(prometheus *service.PrometheusClient) *IntelligenceHandler {
	return &IntelligenceHandler{
		prometheus: prometheus,
	}
}

func (h *IntelligenceHandler) GetVaultRecommendations(w http.ResponseWriter, r *http.Request) {
	vaultID := chi.URLParam(r, "id")
	if vaultID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	recs, err := h.prometheus.GetVaultRecommendations(r.Context(), vaultID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
}

func (h *IntelligenceHandler) GetMarketSentiment(w http.ResponseWriter, r *http.Request) {
	report, err := h.prometheus.GetMarketSentiment(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = report
}

func (h *IntelligenceHandler) GetPortfolioInsights(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userId")
	if userID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	insights, err := h.prometheus.GetPortfolioInsights(r.Context(), userID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = insights
}
