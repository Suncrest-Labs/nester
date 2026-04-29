package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/oracle"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// RateHandler serves GET /api/v1/rates.
type RateHandler struct {
	oracle *oracle.RateService
}

// NewRateHandler returns a RateHandler backed by the given RateService.
func NewRateHandler(svc *oracle.RateService) *RateHandler {
	return &RateHandler{oracle: svc}
}

// Register wires the handler's routes into mux.
func (h *RateHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/rates", h.getRate)
}

type rateResponse struct {
	Base      string  `json:"base"`
	Quote     string  `json:"quote"`
	Rate      float64 `json:"rate"`
	Source    string  `json:"source"`
	FetchedAt string  `json:"fetched_at"`
	ExpiresAt string  `json:"expires_at"`
	Stale     bool    `json:"stale,omitempty"`
}

func (h *RateHandler) getRate(w http.ResponseWriter, r *http.Request) {
	base := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("base")))
	quote := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("quote")))

	if base == "" || quote == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("base and quote query parameters are required"))
		return
	}

	rate, err := h.oracle.GetRate(r.Context(), base, quote)
	if err != nil {
		switch {
		case errors.Is(err, oracle.ErrUnsupportedPair):
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("unsupported currency pair: "+base+"/"+quote))
		default:
			logpkg.FromContext(r.Context()).Error("rate handler failed", "error", err.Error(), "pair", base+"/"+quote)
			response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(
				http.StatusServiceUnavailable,
				"RATE_UNAVAILABLE",
				"exchange rate temporarily unavailable",
			))
		}
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(rateResponse{
		Base:      rate.Base,
		Quote:     rate.Quote,
		Rate:      rate.Rate,
		Source:    rate.Source,
		FetchedAt: rate.FetchedAt.Format(time.RFC3339),
		ExpiresAt: rate.ExpiresAt.Format(time.RFC3339),
		Stale:     rate.Stale,
	}))
}
