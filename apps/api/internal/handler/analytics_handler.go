package handler

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/service/performance"
	"github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// AnalyticsResponse represents the analytics data for a user
type AnalyticsResponse struct {
	DailySnapshots      []DailySnapshot      `json:"daily_snapshots"`
	VaultMonthlyYield   []VaultMonthlyYield  `json:"vault_monthly_yield"`
	CurrentAllocation   []CurrentAllocation  `json:"current_allocation"`
	PerformanceMetrics  PerformanceMetrics   `json:"performance_metrics"`
	Vaults              []VaultInfo          `json:"vaults"`
}

// DailySnapshot represents a daily balance snapshot
type DailySnapshot struct {
	Date              string  `json:"date"`
	TotalBalanceUSD   float64 `json:"total_balance_usd"`
	YieldEarnedUSD    float64 `json:"yield_earned_usd"`
}

// VaultMonthlyYield represents yield per vault per month
type VaultMonthlyYield struct {
	VaultID     string `json:"vault_id"`
	VaultName   string `json:"vault_name"`
	Month       string `json:"month"` // Format: YYYY-MM
	YieldUSD    float64 `json:"yield_usd"`
}

// CurrentAllocation represents current portfolio allocation
type CurrentAllocation struct {
	Protocol      string  `json:"protocol"`
	AllocationPCT float64 `json:"allocation_pct"`
	BalanceUSD    float64 `json:"balance_usd"`
	APY           float64 `json:"apy"`
}

// PerformanceMetrics contains key performance indicators
type PerformanceMetrics struct {
	TotalYieldEarned    float64 `json:"total_yield_earned"`
	YieldChangePCT      float64 `json:"yield_change_pct"`
	BestVaultName       string  `json:"best_vault_name"`
	BestVaultAPY        float64 `json:"best_vault_apy"`
	AverageAPY          float64 `json:"average_apy"`
	TotalDeposited      float64 `json:"total_deposited"`
	TotalWithdrawn      float64 `json:"total_withdrawn"`
	NetPosition         float64 `json:"net_position"`
}

// VaultInfo represents vault information for comparison table
type VaultInfo struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	BalanceUSD      float64 `json:"balance_usd"`
	APY             float64 `json:"apy"`
	YieldEarned     float64 `json:"yield_earned"`
	LockPeriodDays  int     `json:"lock_period_days"`
}

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
	mux.HandleFunc("GET /api/v1/users/{id}/analytics", h.getUserAnalytics)
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