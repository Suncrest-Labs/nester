package handler

import (
	"errors"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// ProtocolComparisonHandler exposes the protocol-level yield comparison
// endpoint (#1324): a time series per protocol, not just a point-in-time
// snapshot, so callers can answer "which protocol has been most consistent
// over 30/90 days".
type ProtocolComparisonHandler struct {
	service *service.ProtocolComparisonService
}

// NewProtocolComparisonHandler constructs a ProtocolComparisonHandler.
func NewProtocolComparisonHandler(svc *service.ProtocolComparisonService) *ProtocolComparisonHandler {
	return &ProtocolComparisonHandler{service: svc}
}

// Register registers the routes on the given ServeMux.
func (h *ProtocolComparisonHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/analytics/protocols/comparison", h.compare)
}

func (h *ProtocolComparisonHandler) compare(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "30d"
	}

	result, err := h.service.Compare(r.Context(), period)
	if err != nil {
		if errors.Is(err, service.ErrInvalidPeriod) {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to compute protocol comparison"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}
