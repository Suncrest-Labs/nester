package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	perfdom "github.com/suncrestlabs/nester/apps/api/internal/domain/performance"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// PerformanceService is the read-side surface PerformanceHandler depends on.
// Keeping the interface here (rather than importing the concrete service)
// keeps the handler test-double-friendly.
type PerformanceService interface {
	Summary(ctx context.Context, vaultID uuid.UUID) (perfdom.PerformanceSummary, error)
	History(ctx context.Context, vaultID uuid.UUID, since time.Time) ([]perfdom.Snapshot, error)
	APY(ctx context.Context, vaultID uuid.UUID) (map[perfdom.Period]float64, error)
}

type PerformanceHandler struct {
	service PerformanceService
	clock   func() time.Time
}

func NewPerformanceHandler(service PerformanceService) *PerformanceHandler {
	return &PerformanceHandler{
		service: service,
		clock:   func() time.Time { return time.Now().UTC() },
	}
}

// SetClock is a test seam for deterministic `since` calculation.
func (h *PerformanceHandler) SetClock(clock func() time.Time) {
	h.clock = clock
}

func (h *PerformanceHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/vaults/{id}/performance", h.summary)
	mux.HandleFunc("GET /api/v1/vaults/{id}/performance/history", h.history)
	mux.HandleFunc("GET /api/v1/vaults/{id}/performance/apy", h.apy)
}

func (h *PerformanceHandler) summary(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}
	out, err := h.service.Summary(r.Context(), vaultID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func (h *PerformanceHandler) history(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
	if period == "" {
		period = "30d"
	}
	days, err := parsePeriodDays(period)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	since := h.clock().Add(-time.Duration(days) * 24 * time.Hour)
	history, err := h.service.History(r.Context(), vaultID, since)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(history))
}

func (h *PerformanceHandler) apy(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}
	apy, err := h.service.APY(r.Context(), vaultID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(apy))
}

// parsePeriodDays accepts the canonical labels (7d, 30d, 90d, all) plus
// raw integer-day strings ("14d", "180d") so the chart UI can pick custom
// ranges without bumping the API.
func parsePeriodDays(period string) (int, error) {
	if period == "all" {
		return 365 * 5, nil // 5 years is enough for the longest sane chart.
	}
	if !strings.HasSuffix(period, "d") {
		return 0, errors.New("period must look like '7d', '30d', '90d', or 'all'")
	}
	n, err := strconv.Atoi(strings.TrimSuffix(period, "d"))
	if err != nil || n <= 0 || n > 365*5 {
		return 0, errors.New("period must be a positive number of days, capped at 5y")
	}
	return n, nil
}

func (h *PerformanceHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	logpkg.FromContext(r.Context()).Error("performance handler failed", "error", err.Error())
	response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
}
