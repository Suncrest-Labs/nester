package handler

import (
	"errors"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// SavingsHandler serves the savings-rate vault endpoints. A nil service means
// the feature is not configured (SAVINGS_CORE_CONTRACT_ID unset) and every
// route answers 503.
type SavingsHandler struct {
	service *service.SavingsService
}

func NewSavingsHandler(svc *service.SavingsService) *SavingsHandler {
	return &SavingsHandler{service: svc}
}

func (h *SavingsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/savings", h.summary)
	mux.HandleFunc("GET /api/v1/savings/history", h.history)
	mux.HandleFunc("GET /api/v1/savings/position", h.position)
}

func (h *SavingsHandler) configured(w http.ResponseWriter) bool {
	if h.service != nil {
		return true
	}
	response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "savings vault not configured"))
	return false
}

func (h *SavingsHandler) writeServiceError(w http.ResponseWriter, r *http.Request, op string, err error) {
	if isUpstreamUnavailable(err) {
		writeUpstreamUnavailable(w, err)
		return
	}
	logpkg.FromContext(r.Context()).Error("savings handler failed", "op", op, "error", err.Error())
	response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
}

func (h *SavingsHandler) summary(w http.ResponseWriter, r *http.Request) {
	if !h.configured(w) {
		return
	}
	out, err := h.service.Summary(r.Context())
	if err != nil {
		h.writeServiceError(w, r, "summary", err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func (h *SavingsHandler) history(w http.ResponseWriter, r *http.Request) {
	if !h.configured(w) {
		return
	}
	out, err := h.service.History(r.Context(), r.URL.Query().Get("period"))
	if err != nil {
		if errors.Is(err, service.ErrInvalidSavingsPeriod) {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
		h.writeServiceError(w, r, "history", err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func (h *SavingsHandler) position(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}
	if !h.configured(w) {
		return
	}
	if user.WalletAddress == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("session has no wallet address"))
		return
	}
	out, err := h.service.Position(r.Context(), user.WalletAddress)
	if err != nil {
		h.writeServiceError(w, r, "position", err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(out))
}
