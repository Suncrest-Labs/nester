package handler

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/service/performance"
	"github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// Analytics response payloads live in internal/domain/analytics, which models
// monetary values as decimal.Decimal. The duplicate float64 copies that used to
// sit here were unreferenced and reintroduced precision loss on the amount path
// (nester#1121).

// AnalyticsHandler handles analytics-related HTTP requests
type AnalyticsHandler struct {
	performanceService *performance.Service
}

// NewAnalyticsHandler creates a new AnalyticsHandler
func NewAnalyticsHandler(performanceService *performance.Service) *AnalyticsHandler {
	return &AnalyticsHandler{
		performanceService: performanceService,
	}
}

// Register registers the analytics routes on the given ServeMux
func (h *AnalyticsHandler) Register(mux *http.ServeMux) {
	// Registered under /analytics to avoid ServeMux conflicts with literal
	// /api/v1/users/... routes (wallet, kyc, savings-goals).
	mux.HandleFunc("GET /api/v1/analytics/users/{id}", h.getUserAnalytics)
}

// getUserAnalytics handles GET /api/v1/users/{id}/analytics?from=YYYY-MM-DD&to=YYYY-MM-DD
func (h *AnalyticsHandler) getUserAnalytics(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from path
	idStr := r.PathValue("id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid user ID"))
		return
	}

	// Parse query parameters
	fromParam := r.URL.Query().Get("from")
	toParam := r.URL.Query().Get("to")

	var fromTime, toTime time.Time
	if fromParam != "" {
		fromTime, err = time.Parse("2006-01-02", fromParam)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid 'from' date format, expected YYYY-MM-DD"))
			return
		}
	} else {
		// Default to 30 days ago
		fromTime = time.Now().AddDate(0, 0, -30)
	}

	if toParam != "" {
		toTime, err = time.Parse("2006-01-02", toParam)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid 'to' date format, expected YYYY-MM-DD"))
			return
		}
	} else {
		// Default to today
		toTime = time.Now()
	}

	// Get analytics data from service
	analyticsData, err := h.performanceService.GetUserAnalytics(r.Context(), userID, fromTime, toTime)
	if err != nil {
		logger.FromContext(r.Context()).Error("failed to get user analytics", "error", err.Error(), "user_id", userID)
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(analyticsData))
}