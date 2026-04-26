package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

const (
	defaultAdminPage    = 1
	defaultAdminPerPage = 50
	maxAdminPerPage     = 200
)

type adminService interface {
	GetDashboard(ctx context.Context) (service.DashboardResponse, error)
	ListVaults(ctx context.Context, filter admindomain.VaultListFilter) ([]admindomain.VaultSummary, int, error)
	GetVaultDetail(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error)
	PauseVault(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error)
	UnpauseVault(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error)
	ListSettlements(ctx context.Context, filter admindomain.SettlementListFilter) ([]admindomain.SettlementSummary, int, error)
	ListUsers(ctx context.Context, filter admindomain.UserListFilter) ([]admindomain.UserSummary, int, error)
	GetDetailedHealth(ctx context.Context) (admindomain.DetailedHealth, error)
}

// EventSyncer triggers a one-shot run of the on-chain event indexer for
// recovery or back-fill purposes.  The no-op default is used when no indexer
// is configured (e.g. in test environments).
type EventSyncer interface {
	SyncEvents(ctx context.Context) (processed int, err error)
}

type noopEventSyncer struct{}

func (noopEventSyncer) SyncEvents(_ context.Context) (int, error) { return 0, nil }

type AdminHandler struct {
	service     adminService
	eventSyncer EventSyncer
}

func NewAdminHandler(svc adminService) *AdminHandler {
	return &AdminHandler{service: svc, eventSyncer: noopEventSyncer{}}
}

// SetEventSyncer wires a real EventSyncer.  Call this from main after the
// indexer has been initialised so the admin handler can trigger manual syncs.
func (h *AdminHandler) SetEventSyncer(es EventSyncer) {
	h.eventSyncer = es
}

func (h *AdminHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/dashboard", h.getDashboard)
	mux.HandleFunc("GET /api/v1/admin/vaults", h.listVaults)
	mux.HandleFunc("GET /api/v1/admin/vaults/{id}", h.getVaultDetail)
	mux.HandleFunc("POST /api/v1/admin/vaults/{id}/pause", h.pauseVault)
	mux.HandleFunc("POST /api/v1/admin/vaults/{id}/unpause", h.unpauseVault)
	mux.HandleFunc("GET /api/v1/admin/settlements", h.listSettlements)
	mux.HandleFunc("GET /api/v1/admin/users", h.listUsers)
	mux.HandleFunc("GET /api/v1/admin/health", h.getDetailedHealth)
	mux.HandleFunc("POST /api/v1/admin/sync-events", h.syncEvents)
}

// syncEvents handles POST /api/v1/admin/sync-events
//
// Triggers a one-shot indexer run synchronously.  Useful for recovery after
// an RPC outage or for back-filling events that were missed during downtime.
func (h *AdminHandler) syncEvents(w http.ResponseWriter, r *http.Request) {
	processed, err := h.eventSyncer.SyncEvents(r.Context())
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Response{
			Success: false,
			Error: &response.ErrorBody{
				Code:    "SYNC_FAILED",
				Message: "event sync failed: " + err.Error(),
			},
		})
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(map[string]any{
		"processed": processed,
	}))
}

func (h *AdminHandler) getDashboard(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetDashboard(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *AdminHandler) listVaults(w http.ResponseWriter, r *http.Request) {
	page, perPage := parseAdminPagination(r)
	filter := admindomain.VaultListFilter{
		Page:    page,
		PerPage: perPage,
		Status:  r.URL.Query().Get("status"),
		Sort:    r.URL.Query().Get("sort"),
		Order:   r.URL.Query().Get("order"),
		Search:  r.URL.Query().Get("search"),
	}

	items, total, err := h.service.ListVaults(r.Context(), filter)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	out := response.OK(items)
	out.Meta = &response.Meta{
		Page:       page,
		PerPage:    perPage,
		TotalCount: total,
	}
	response.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) getVaultDetail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	result, err := h.service.GetVaultDetail(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *AdminHandler) pauseVault(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	result, err := h.service.PauseVault(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *AdminHandler) unpauseVault(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	result, err := h.service.UnpauseVault(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *AdminHandler) listSettlements(w http.ResponseWriter, r *http.Request) {
	page, perPage := parseAdminPagination(r)
	filter := admindomain.SettlementListFilter{
		Page:    page,
		PerPage: perPage,
		Status:  r.URL.Query().Get("status"),
		Sort:    r.URL.Query().Get("sort"),
		Order:   r.URL.Query().Get("order"),
		Search:  r.URL.Query().Get("search"),
	}

	dateFrom, err := parseAdminDateQuery(r.URL.Query().Get("date_from"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("date_from must be RFC3339 or YYYY-MM-DD"))
		return
	}
	filter.DateFrom = dateFrom

	dateTo, err := parseAdminDateQuery(r.URL.Query().Get("date_to"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("date_to must be RFC3339 or YYYY-MM-DD"))
		return
	}
	filter.DateTo = dateTo

	items, total, err := h.service.ListSettlements(r.Context(), filter)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	out := response.OK(items)
	out.Meta = &response.Meta{
		Page:       page,
		PerPage:    perPage,
		TotalCount: total,
	}
	response.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) listUsers(w http.ResponseWriter, r *http.Request) {
	page, perPage := parseAdminPagination(r)
	filter := admindomain.UserListFilter{
		Page:    page,
		PerPage: perPage,
		Sort:    r.URL.Query().Get("sort"),
		Order:   r.URL.Query().Get("order"),
		Search:  r.URL.Query().Get("search"),
	}

	items, total, err := h.service.ListUsers(r.Context(), filter)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	out := response.OK(items)
	out.Meta = &response.Meta{
		Page:       page,
		PerPage:    perPage,
		TotalCount: total,
	}
	response.WriteJSON(w, http.StatusOK, out)
}

func (h *AdminHandler) getDetailedHealth(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetDetailedHealth(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func parseAdminPagination(r *http.Request) (int, int) {
	page := parseAdminIntQuery(r.URL.Query().Get("page"), defaultAdminPage)
	perPage := parseAdminIntQuery(r.URL.Query().Get("per_page"), defaultAdminPerPage)

	if page < 1 {
		page = defaultAdminPage
	}
	if perPage < 1 {
		perPage = defaultAdminPerPage
	}
	if perPage > maxAdminPerPage {
		perPage = maxAdminPerPage
	}

	return page, perPage
}

func parseAdminIntQuery(raw string, fallback int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func parseAdminDateQuery(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		utc := parsed.UTC()
		return &utc, nil
	}
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		utc := parsed.UTC()
		return &utc, nil
	}
	return nil, errors.New("invalid date format")
}

func (h *AdminHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidAdminInput):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, vault.ErrVaultNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("vault"))
	default:
		logpkg.FromContext(r.Context()).Error("admin handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
