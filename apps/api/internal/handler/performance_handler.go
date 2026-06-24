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
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	vaultdom "github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
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
	GetAPYHistory(ctx context.Context, vaultID uuid.UUID, period, interval string) (perfdom.APYHistoryResponse, error)
}

// VaultOwnershipChecker lets the handler verify vault ownership without
// depending on the full vault service.
type VaultOwnershipChecker interface {
	// OwnerIDForVault returns the uuid.UUID owner of the given vault, or
	// vaultdom.ErrVaultNotFound when the vault does not exist.
	OwnerIDForVault(ctx context.Context, vaultID uuid.UUID) (uuid.UUID, error)
}

// vaultGetterForOwnership is the minimal repo surface we need.
type vaultGetterForOwnership interface {
	GetVault(ctx context.Context, id uuid.UUID) (vaultdom.Vault, error)
}

// VaultOwnerAdapter wraps any type that can GetVault and exposes OwnerIDForVault.
type VaultOwnerAdapter struct{ repo vaultGetterForOwnership }

func NewVaultOwnerAdapter(repo vaultGetterForOwnership) *VaultOwnerAdapter {
	return &VaultOwnerAdapter{repo: repo}
}

func (a *VaultOwnerAdapter) OwnerIDForVault(ctx context.Context, vaultID uuid.UUID) (uuid.UUID, error) {
	v, err := a.repo.GetVault(ctx, vaultID)
	if err != nil {
		return uuid.Nil, err
	}
	return v.UserID, nil
}

type PerformanceHandler struct {
	service    PerformanceService
	vaultOwner VaultOwnershipChecker
	clock      func() time.Time
}

func NewPerformanceHandler(service PerformanceService, vaultOwner VaultOwnershipChecker) *PerformanceHandler {
	return &PerformanceHandler{
		service:    service,
		vaultOwner: vaultOwner,
		clock:      func() time.Time { return time.Now().UTC() },
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
	mux.HandleFunc("GET /api/v1/vaults/{id}/apy-history", h.apyHistory)
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

func (h *PerformanceHandler) apyHistory(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	// Auth check
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

	// Ownership check — return 404 (not 403) to avoid vault-existence oracle
	ownerID, err := h.vaultOwner.OwnerIDForVault(r.Context(), vaultID)
	if err != nil || ownerID != userID {
		response.WriteJSON(w, http.StatusNotFound, response.Err(http.StatusNotFound, "NOT_FOUND", "vault not found"))
		return
	}

	period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
	if period == "" {
		period = "30d"
	}
	switch period {
	case "7d", "30d", "90d", "all":
	default:
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("period must be 7d, 30d, 90d, or all"))
		return
	}

	interval := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("interval")))
	if interval == "" {
		interval = "daily"
	}
	switch interval {
	case "daily", "weekly":
	default:
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("interval must be daily or weekly"))
		return
	}

	history, err := h.service.GetAPYHistory(r.Context(), vaultID, period, interval)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(history))
}

func (h *PerformanceHandler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	logpkg.FromContext(r.Context()).Error("performance handler failed", "error", err.Error())
	response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
}
