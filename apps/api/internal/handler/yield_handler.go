package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// YieldOpportunitiesProvider is the service surface the handler depends on.
type YieldOpportunitiesProvider interface {
	GetYieldOpportunities(ctx context.Context, chain string, limit int) ([]service.YieldPool, error)
}

// YieldHandler serves GET /api/v1/yield-opportunities.
type YieldHandler struct {
	svc YieldOpportunitiesProvider
}

func NewYieldHandler(svc YieldOpportunitiesProvider) *YieldHandler {
	return &YieldHandler{svc: svc}
}

func (h *YieldHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/yield-opportunities", h.list)
}

func (h *YieldHandler) list(w http.ResponseWriter, r *http.Request) {
	chain := strings.TrimSpace(r.URL.Query().Get("chain"))
	if chain == "" {
		chain = "Stellar"
	}

	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if n, err := strconv.Atoi(lStr); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	pools, err := h.svc.GetYieldOpportunities(r.Context(), chain, limit)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("yield opportunities fetch failed", "chain", chain, "error", err.Error())
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UPSTREAM_ERROR", "yield data temporarily unavailable"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(pools))
}
