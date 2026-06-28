package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// YieldOpportunitiesProvider is the service surface the handler depends on.
type YieldOpportunitiesProvider interface {
	GetYieldOpportunitiesByTier(ctx context.Context, chain string, limit int, tier string) (*service.YieldOpportunitiesResponse, error)
	CompareProtocols(ctx context.Context, protocols []string) ([]service.ProtocolSummary, error)
}

// YieldBookmarkSlugLister lists bookmark slugs for sorting.
type YieldBookmarkSlugLister interface {
	ListSlugs(ctx context.Context, userID uuid.UUID) ([]string, error)
	SortPoolsByBookmarks(pools []service.YieldPool, bookmarkSlugs []string) []service.YieldPool
}

// YieldHandler serves yield opportunity endpoints.
type YieldHandler struct {
	svc       YieldOpportunitiesProvider
	bookmarks YieldBookmarkSlugLister
}

func NewYieldHandler(svc YieldOpportunitiesProvider, bookmarks YieldBookmarkSlugLister) *YieldHandler {
	return &YieldHandler{svc: svc, bookmarks: bookmarks}
}

func (h *YieldHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/yield-opportunities", h.list)
	mux.HandleFunc("GET /api/v1/yield-opportunities/compare", h.compare)
	mux.HandleFunc("GET /api/v1/yields", h.list)
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

	// Optional risk-tier filter: low|medium|high. Omitting it returns all tiers.
	tier := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("risk")))
	if tier != "" && !service.IsValidRiskTier(tier) {
		response.WriteJSON(w, http.StatusBadRequest, response.Err(http.StatusBadRequest, "INVALID_RISK_TIER", "risk must be one of: low, medium, high"))
		return
	}

	yieldResp, err := h.svc.GetYieldOpportunitiesByTier(r.Context(), chain, limit, tier)
	if err != nil {
		logpkg.FromContext(r.Context()).Error("yield opportunities fetch failed", "chain", chain, "error", err.Error())
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UPSTREAM_ERROR", "yield data temporarily unavailable"))
		return
	}

	pools := yieldResp.Pools
	if h.bookmarks != nil && r.URL.Query().Get("sort_bookmarks") == "true" {
		if user, ok := auth.GetUserFromContext(r.Context()); ok {
			if userID, err := uuid.Parse(user.ID); err == nil {
				slugs, err := h.bookmarks.ListSlugs(r.Context(), userID)
				if err == nil && len(slugs) > 0 {
					pools = h.bookmarks.SortPoolsByBookmarks(pools, slugs)
				}
			}
		}
	}

	responseData := map[string]interface{}{
		"data": pools,
		"meta": yieldResp.Meta,
	}

	response.WriteJSON(w, http.StatusOK, response.Response{
		Success: true,
		Data:    responseData,
	})
}
