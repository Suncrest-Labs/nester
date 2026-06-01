package handler

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// IntelligenceHandler proxies intelligence service routes through the Go API.
type IntelligenceHandler struct {
	proxy      *service.IntelligenceProxy
	prometheus *service.PrometheusClient
}

func NewIntelligenceHandler(proxy *service.IntelligenceProxy, prometheus *service.PrometheusClient) *IntelligenceHandler {
	return &IntelligenceHandler{proxy: proxy, prometheus: prometheus}
}

func (h *IntelligenceHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/vaults/{id}/recommendations", h.vaultRecommendations)
	mux.HandleFunc("GET /api/v1/intelligence/market", h.marketSentiment)
	mux.HandleFunc("GET /api/v1/intelligence/recommend/vault", h.recommendVaultGet)
	mux.HandleFunc("POST /api/v1/intelligence/recommend/vault", h.recommendVaultPost)
	mux.HandleFunc("POST /api/v1/intelligence/coaching", h.coaching)
	mux.HandleFunc("POST /api/v1/intelligence/analyze", h.analyze)
	mux.HandleFunc("GET /api/v1/users/{userId}/insights", h.portfolioInsights)
	mux.HandleFunc("GET /api/v1/portfolio/{user_id}/insights", h.portfolioInsightsByPath)
}

func (h *IntelligenceHandler) vaultRecommendations(w http.ResponseWriter, r *http.Request) {
	vaultID := r.PathValue("id")
	if vaultID == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id is required"))
		return
	}
	if h.proxy != nil {
		h.proxy.Forward(w, r, "/vaults/"+vaultID+"/recommendations")
		return
	}
	if h.prometheus == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	recs, err := h.prometheus.GetVaultRecommendations(r.Context(), vaultID)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("vault recommendations failed", "error", err.Error())
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", err.Error()))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(recs))
}

func (h *IntelligenceHandler) marketSentiment(w http.ResponseWriter, r *http.Request) {
	if h.proxy != nil {
		h.proxy.Forward(w, r, "/market/sentiment")
		return
	}
	if h.prometheus == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	report, err := h.prometheus.GetMarketSentiment(r.Context())
	if err != nil {
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", err.Error()))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(report))
}

func (h *IntelligenceHandler) recommendVaultGet(w http.ResponseWriter, r *http.Request) {
	if h.proxy == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	h.proxy.Forward(w, r, "/recommend/vault")
}

func (h *IntelligenceHandler) recommendVaultPost(w http.ResponseWriter, r *http.Request) {
	if h.proxy == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	h.proxy.Forward(w, r, "/recommend/vault")
}

func (h *IntelligenceHandler) coaching(w http.ResponseWriter, r *http.Request) {
	if h.proxy == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	h.proxy.Forward(w, r, "/intelligence/coaching")
}

func (h *IntelligenceHandler) analyze(w http.ResponseWriter, r *http.Request) {
	if h.proxy == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	h.proxy.Forward(w, r, "/analyze")
}

func (h *IntelligenceHandler) portfolioInsights(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	if !h.authorizeUserInsights(w, r, userID) {
		return
	}
	if h.proxy != nil {
		h.proxy.Forward(w, r, "/portfolio/"+userID+"/insights")
		return
	}
	if h.prometheus == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	insights, err := h.prometheus.GetPortfolioInsights(r.Context(), userID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", err.Error()))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(insights))
}

func (h *IntelligenceHandler) portfolioInsightsByPath(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if !h.authorizeUserInsights(w, r, userID) {
		return
	}
	if h.proxy != nil {
		h.proxy.Forward(w, r, "/portfolio/"+userID+"/insights")
		return
	}
	if h.prometheus == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	insights, err := h.prometheus.GetPortfolioInsights(r.Context(), userID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", err.Error()))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(insights))
}

func (h *IntelligenceHandler) authorizeUserInsights(w http.ResponseWriter, r *http.Request, userID string) bool {
	if userID == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("user id is required"))
		return false
	}
	if _, err := uuid.Parse(userID); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("user id must be a valid UUID"))
		return false
	}
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return false
	}
	if user.ID != userID {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return false
	}
	return true
}
