package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// YieldOpportunitiesProvider is the service surface the handler depends on.
type YieldOpportunitiesProvider interface {
	GetYieldOpportunities(ctx context.Context, chain string, limit int) (*service.YieldOpportunitiesResponse, error)
	CompareProtocols(ctx context.Context, protocols []string) ([]service.ProtocolSummary, error)
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
	mux.HandleFunc("GET /api/v1/yield-opportunities/compare", h.compare)
}

func (h *YieldHandler) compare(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("protocols")
	protocols := make([]string, 0, maxYieldCompare)
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			protocols = append(protocols, p)
		}
	}

	summaries, err := h.svc.CompareProtocols(r.Context(), protocols)
	if err != nil {
		if errors.Is(err, service.ErrInvalidProtocolCount) {
			response.WriteJSON(w, http.StatusBadRequest, response.Err(http.StatusBadRequest, "INVALID_PROTOCOLS", err.Error()))
			return
		}
		logpkg.FromContext(r.Context()).Error("yield protocol comparison failed", "error", err.Error())
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UPSTREAM_ERROR", "yield data temporarily unavailable"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.Response{
		Success: true,
		Data:    map[string]interface{}{"data": summaries},
	})
}

// maxYieldCompare mirrors the service's upper bound for query parsing capacity.
const maxYieldCompare = 5

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

	yieldResp, err := h.svc.GetYieldOpportunities(r.Context(), chain, limit)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("yield opportunities fetch failed", "chain", chain, "error", err.Error())
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UPSTREAM_ERROR", "yield data temporarily unavailable"))
		return
	}

	// Build response with meta information
	responseData := map[string]interface{}{
		"data": yieldResp.Pools,
		"meta": yieldResp.Meta,
	}

	response.WriteJSON(w, http.StatusOK, response.Response{
		Success: true,
		Data:    responseData,
	})
}
