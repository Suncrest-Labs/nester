package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/intelligence"
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
	mux.HandleFunc("GET /api/v1/vaults/{id}/recommendations", h.GetVaultRecommendations)
	mux.HandleFunc("GET /api/v1/intelligence/market", h.marketSentiment)
	mux.HandleFunc("GET /api/v1/intelligence/recommend/vault", h.recommendVaultGet)
	mux.HandleFunc("POST /api/v1/intelligence/recommend/vault", h.recommendVaultPost)
	mux.HandleFunc("POST /api/v1/intelligence/coaching", h.coaching)
	mux.HandleFunc("POST /api/v1/intelligence/analyze", h.analyze)
	mux.HandleFunc("GET /api/v1/intelligence/portfolio/{userId}", h.GetPortfolioInsights)
	mux.HandleFunc("GET /api/v1/portfolio/{user_id}/insights", h.portfolioInsightsByPath)
	mux.HandleFunc("POST /api/v1/intelligence/savings-plan", h.CreateSavingsPlan)
	mux.HandleFunc("POST /api/v1/vaults/{id}/rebalance/suggest", h.suggestAIRebalance)
	mux.HandleFunc("POST /api/v1/vaults/{id}/rebalance/execute", h.executeAIRebalance)
	mux.HandleFunc("POST /api/v1/intelligence/tools/{proposalId}/confirm", h.ConfirmTool)
}

func (h *IntelligenceHandler) GetVaultRecommendations(w http.ResponseWriter, r *http.Request) {
	vaultID := chi.URLParam(r, "id")
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
		logpkg.FromContext(r.Context()).Error("market sentiment failed", "error", err.Error())
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

func (h *IntelligenceHandler) GetPortfolioInsights(w http.ResponseWriter, r *http.Request) {
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
		logpkg.FromContext(r.Context()).Error("portfolio insights failed", "error", err.Error())
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", err.Error()))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(insights))
}

func (h *IntelligenceHandler) CreateSavingsPlan(w http.ResponseWriter, r *http.Request) {
	var req intelligence.SavingsPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}

	if h.prometheus == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}

	plan, err := h.prometheus.CreateSavingsPlan(r.Context(), req)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("create savings plan failed", "error", err.Error())
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", err.Error()))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(plan))
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

// suggestAIRebalance proxies to the intelligence service's risk-adjusted
// rebalancing engine. Distinct from the rule-based /rebalance-suggestion
// endpoint on VaultHandler: this one is Claude-assisted and scores protocols
// via the Sharpe-ratio inspired risk model.
func (h *IntelligenceHandler) suggestAIRebalance(w http.ResponseWriter, r *http.Request) {
	vaultID := r.PathValue("id")
	if vaultID == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id is required"))
		return
	}
	if h.proxy == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	h.proxy.Forward(w, r, "/vaults/"+vaultID+"/rebalance/suggest")
}

// executeAIRebalance proxies to the intelligence service to build an
// unsigned Stellar transaction for a user-approved AI rebalance suggestion.
func (h *IntelligenceHandler) executeAIRebalance(w http.ResponseWriter, r *http.Request) {
	vaultID := r.PathValue("id")
	if vaultID == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id is required"))
		return
	}
	if h.proxy == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}
	h.proxy.Forward(w, r, "/vaults/"+vaultID+"/rebalance/execute")
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

func (h *IntelligenceHandler) ConfirmTool(w http.ResponseWriter, r *http.Request) {
	if h.proxy == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "intelligence not configured"))
		return
	}

	status, body, err := h.proxy.ForwardJSON(r, "/intelligence/tools/"+r.PathValue("proposalId")+"/confirm")
	if err != nil {
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", "intelligence service unavailable"))
		return
	}

	var payload map[string]any
	if jsonErr := json.Unmarshal(body, &payload); jsonErr != nil {
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", "invalid response from intelligence service"))
		return
	}

	if status >= 400 {
		message, _ := payload["detail"].(string)
		if message == "" {
			message = "tool confirmation failed"
		}
		response.WriteJSON(w, status, response.Err(status, "UPSTREAM_ERROR", message))
		return
	}

	response.WriteJSON(w, status, response.OK(payload))
}
