package handler

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	performancesvc "github.com/suncrestlabs/nester/apps/api/internal/service/performance"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// PerformanceSnapshotsHandler serves GET /api/v1/performance/snapshots for intelligence context.
type PerformanceSnapshotsHandler struct {
	performance *performancesvc.Service
}

func NewPerformanceSnapshotsHandler(performance *performancesvc.Service) *PerformanceSnapshotsHandler {
	return &PerformanceSnapshotsHandler{performance: performance}
}

func (h *PerformanceSnapshotsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/performance/snapshots", h.list)
}

func (h *PerformanceSnapshotsHandler) list(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user identity"))
		return
	}

	toTime := time.Now().UTC()
	fromTime := toTime.Add(-30 * 24 * time.Hour)
	analytics, err := h.performance.GetUserAnalytics(r.Context(), userID, fromTime, toTime)
	if err != nil {
		response.WriteJSON(w, http.StatusOK, response.OK(map[string]any{"snapshots": []any{}}))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(map[string]any{
		"snapshots":            analytics.DailySnapshots,
		"performance_metrics":  analytics.PerformanceMetrics,
		"current_allocation":   analytics.CurrentAllocation,
		"vaults":               analytics.Vaults,
	}))
}
