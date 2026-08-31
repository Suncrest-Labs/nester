package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// NewYieldComparisonHandler constructs the comparison endpoint handler.
func NewYieldComparisonHandler(yieldService *service.YieldService) http.Handler {
	mux := http.NewServeMux()
	RegisterYieldComparisonEndpoint(mux, yieldService)
	return mux
}

// RegisterYieldComparisonEndpoint registers the side-by-side comparison
// endpoint on mux. The application's main router must call this function (or
// mount NewYieldComparisonHandler) for the endpoint to be reachable.
func RegisterYieldComparisonEndpoint(mux *http.ServeMux, yieldService *service.YieldService) {
	mux.HandleFunc("/api/v1/yield-opportunities/compare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeYieldComparisonError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		chain := r.URL.Query().Get("chain")
		if chain == "" {
			chain = "Stellar"
		}

		limit := 100
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 100 {
				writeYieldComparisonError(w, http.StatusBadRequest, "limit must be between 1 and 100")
				return
			}
			limit = parsed
		}

		comparison, err := yieldService.GetYieldComparison(r.Context(), chain, limit)
		if err != nil {
			writeYieldComparisonError(w, http.StatusBadGateway, "yield data unavailable")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Success bool                    `json:"success"`
			Data    service.YieldComparison `json:"data"`
		}{Success: true, Data: comparison})
	})
}

func writeYieldComparisonError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Success bool `json:"success"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}{
		Success: false,
		Error: struct {
			Message string `json:"message"`
		}{Message: message},
	})
}
