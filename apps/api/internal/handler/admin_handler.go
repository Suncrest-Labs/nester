package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
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
	GetDashboard(ctx context.Context) (admindomain.VaultHealthDashboard, error)
	ListVaults(ctx context.Context, filter admindomain.VaultListFilter) ([]admindomain.VaultSummary, int, error)
	GetVaultDetail(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error)
	PauseVault(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error)
	UnpauseVault(ctx context.Context, id uuid.UUID) (admindomain.VaultDetail, error)
	CreateAllocation(ctx context.Context, input service.CreateAllocationInput) (vault.Allocation, error)
	UpdateAllocation(ctx context.Context, input service.UpdateAllocationInput) (vault.Allocation, error)
	DeleteAllocation(ctx context.Context, input service.DeleteAllocationInput) error
	TriggerRebalance(ctx context.Context, id uuid.UUID, req admindomain.RebalanceRequest) (admindomain.RebalanceResponse, error)
	ListSettlements(ctx context.Context, filter admindomain.SettlementListFilter) ([]admindomain.SettlementSummary, int, error)
	ListUsers(ctx context.Context, filter admindomain.UserListFilter) ([]admindomain.UserSummary, int, error)
	ListVaultRebalances(ctx context.Context, vaultID uuid.UUID) ([]admindomain.VaultRebalanceRecord, error)
	GetDetailedHealth(ctx context.Context) (admindomain.DetailedHealth, error)
	// Goal template marketplace (#919): admins publish curated templates
	// beyond the pre-built defaults, growing the catalog without a redeploy.
	ListGoalTemplates(ctx context.Context) ([]savingsgoal.GoalTemplate, error)
	CreateGoalTemplate(ctx context.Context, in service.CreateGoalTemplateInput) (savingsgoal.GoalTemplate, error)
	UpdateGoalTemplate(ctx context.Context, in service.UpdateGoalTemplateInput) (savingsgoal.GoalTemplate, error)
	DeleteGoalTemplate(ctx context.Context, id uuid.UUID) error
}

// EventSyncer triggers a one-shot run of the on-chain event indexer for
// recovery or back-fill purposes.  The no-op default is used when no indexer
// is configured (e.g. in test environments).
type EventSyncer interface {
	SyncEvents(ctx context.Context) (processed int, err error)
}

type noopEventSyncer struct{}

func (noopEventSyncer) SyncEvents(_ context.Context) (int, error) { return 0, nil }

// LeadershipStatus reports the scheduler's leader-election state (#846) —
// satisfied by *scheduler.Leadership. Declared narrowly here (rather than
// importing the scheduler package's concrete type) so this handler only
// depends on the three read-only accessors it actually needs.
type LeadershipStatus interface {
	InstanceID() string
	IsLeader() bool
	Since() time.Time
}

type noopLeadershipStatus struct{}

func (noopLeadershipStatus) InstanceID() string { return "" }
func (noopLeadershipStatus) IsLeader() bool     { return false }
func (noopLeadershipStatus) Since() time.Time   { return time.Time{} }

type AdminHandler struct {
	service     adminService
	userService *service.UserService
	eventSyncer EventSyncer
	leadership  LeadershipStatus
}

func NewAdminHandler(svc adminService, userSvc *service.UserService) *AdminHandler {
	return &AdminHandler{service: svc, userService: userSvc, eventSyncer: noopEventSyncer{}, leadership: noopLeadershipStatus{}}
}

// SetEventSyncer wires a real EventSyncer.  Call this from main after the
// indexer has been initialised so the admin handler can trigger manual syncs.
func (h *AdminHandler) SetEventSyncer(es EventSyncer) {
	h.eventSyncer = es
}

// SetLeadership wires the scheduler's leader-election component (#846) so
// GET /api/v1/admin/scheduler/leadership can report which instance is
// currently the scheduler leader. Call this from main after
// scheduler.NewLeadership has been constructed.
func (h *AdminHandler) SetLeadership(l LeadershipStatus) {
	h.leadership = l
}

func (h *AdminHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/dashboard", h.getDashboard)
	mux.HandleFunc("GET /api/v1/admin/vaults", h.listVaults)
	mux.HandleFunc("GET /api/v1/admin/vaults/{id}", h.getVaultDetail)
	mux.HandleFunc("POST /api/v1/admin/vaults/{id}/pause", h.pauseVault)
	mux.HandleFunc("POST /api/v1/admin/vaults/{id}/unpause", h.unpauseVault)
	mux.HandleFunc("POST /api/v1/admin/vaults/{id}/rebalance", h.rebalanceVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}/rebalance-history", h.getRebalanceHistory)
	mux.HandleFunc("POST /api/v1/admin/vaults/{id}/allocations", h.createAllocation)
	mux.HandleFunc("PATCH /api/v1/admin/vaults/{id}/allocations/{alloc_id}", h.updateAllocation)
	mux.HandleFunc("DELETE /api/v1/admin/vaults/{id}/allocations/{alloc_id}", h.deleteAllocation)
	mux.HandleFunc("GET /api/v1/admin/settlements", h.listSettlements)
	mux.HandleFunc("GET /api/v1/admin/users", h.listUsers)
	mux.HandleFunc("GET /api/v1/admin/health", h.getDetailedHealth)
	mux.HandleFunc("GET /api/v1/admin/scheduler/leadership", h.getSchedulerLeadership)
	mux.HandleFunc("POST /api/v1/admin/sync-events", h.syncEvents)
	mux.HandleFunc("PATCH /api/v1/admin/users/{id}/kyc", h.reviewUserKYC)

	// Goal template marketplace (#919).
	mux.HandleFunc("GET /api/v1/admin/savings-goal-templates", h.listGoalTemplates)
	mux.HandleFunc("POST /api/v1/admin/savings-goal-templates", h.createGoalTemplate)
	mux.HandleFunc("PATCH /api/v1/admin/savings-goal-templates/{id}", h.updateGoalTemplate)
	mux.HandleFunc("DELETE /api/v1/admin/savings-goal-templates/{id}", h.deleteGoalTemplate)
}

// getSchedulerLeadership handles GET /api/v1/admin/scheduler/leadership
// (#846): reports whether THIS instance currently holds the scheduler
// leader-election lock, since when, and this instance's identity — so
// operators can confirm leadership is held by exactly one replica at a
// time and observe failover (a new instance_id / since after a leader
// dies) without needing a metrics backend.
func (h *AdminHandler) getSchedulerLeadership(w http.ResponseWriter, r *http.Request) {
	isLeader := h.leadership.IsLeader()
	var since *string
	if isLeader {
		s := h.leadership.Since().UTC().Format(time.RFC3339)
		since = &s
	}
	response.WriteJSON(w, http.StatusOK, response.Response{
		Success: true,
		Data: map[string]any{
			"instance_id": h.leadership.InstanceID(),
			"is_leader":   isLeader,
			"since":       since,
		},
	})
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

func (h *AdminHandler) reviewUserKYC(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid user ID"))
		return
	}

	var req struct {
		Status          string `json:"status"`
		RejectionReason string `json:"rejection_reason"`
	}
	// Note: decodeJSON is not available in AdminHandler, we'll parse it manually
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}

	if req.Status != "verified" && req.Status != "rejected" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("status must be verified or rejected"))
		return
	}

	var reason *string
	if req.Status == "rejected" {
		if req.RejectionReason == "" {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("rejection_reason is required when rejecting"))
			return
		}
		reason = &req.RejectionReason
	}

	var kycStatus user.KYCStatus
	if req.Status == "verified" {
		kycStatus = user.KYCStatusVerified
	} else {
		kycStatus = user.KYCStatusRejected
	}

	if err := h.userService.UpdateKYCStatus(r.Context(), userID, kycStatus, reason); err != nil {
		h.writeError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(map[string]string{"status": string(kycStatus)}))
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

func (h *AdminHandler) rebalanceVault(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}

	var req admindomain.RebalanceRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("request body must be valid JSON"))
			return
		}
	}

	result, err := h.service.TriggerRebalance(r.Context(), id, req)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *AdminHandler) getRebalanceHistory(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	history, err := h.service.ListVaultRebalances(r.Context(), vaultID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(history))
}

type createAllocationRequest struct {
	Protocol string          `json:"protocol"`
	Weight   decimal.Decimal `json:"weight"`
	APY      decimal.Decimal `json:"apy"`
}

func (h *AdminHandler) createAllocation(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	var req createAllocationRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	result, err := h.service.CreateAllocation(r.Context(), service.CreateAllocationInput{
		VaultID:  vaultID,
		Protocol: req.Protocol,
		Weight:   req.Weight,
		APY:      req.APY,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(result))
}

type updateAllocationRequest struct {
	Protocol *string          `json:"protocol,omitempty"`
	Weight   *decimal.Decimal `json:"weight,omitempty"`
	APY      *decimal.Decimal `json:"apy,omitempty"`
}

func (h *AdminHandler) updateAllocation(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	allocationID, err := uuid.Parse(r.PathValue("alloc_id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("allocation id must be a valid UUID"))
		return
	}

	var req updateAllocationRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	if req.Protocol == nil && req.Weight == nil && req.APY == nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("at least one of protocol, weight, or apy must be provided"))
		return
	}

	result, err := h.service.UpdateAllocation(r.Context(), service.UpdateAllocationInput{
		VaultID:      vaultID,
		AllocationID: allocationID,
		Protocol:     req.Protocol,
		Weight:       req.Weight,
		APY:          req.APY,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *AdminHandler) deleteAllocation(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	allocationID, err := uuid.Parse(r.PathValue("alloc_id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("allocation id must be a valid UUID"))
		return
	}

	force := strings.EqualFold(r.URL.Query().Get("force"), "true")
	if err := h.service.DeleteAllocation(r.Context(), service.DeleteAllocationInput{
		VaultID:      vaultID,
		AllocationID: allocationID,
		Force:        force,
	}); err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(map[string]string{"status": "deleted"}))
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
	case errors.Is(err, service.ErrRebalanceInFlight):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "REBALANCE_IN_FLIGHT", err.Error()))
	case errors.Is(err, service.ErrRebalanceNotEligible):
		response.WriteJSON(w, http.StatusBadRequest, response.Err(http.StatusBadRequest, "REBALANCE_NOT_ELIGIBLE", err.Error()))
	case errors.Is(err, service.ErrChainNotConfigured):
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "CHAIN_NOT_CONFIGURED", err.Error()))
	case errors.Is(err, vault.ErrVaultNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("vault"))
	case errors.Is(err, vault.ErrAllocationNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("allocation"))
	case errors.Is(err, vault.ErrInvalidAllocation), errors.Is(err, vault.ErrInvalidPrecision), errors.Is(err, vault.ErrDuplicateProtocol):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, vault.ErrAllocationHasBalance):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "ALLOCATION_HAS_BALANCE", err.Error()))
	case errors.Is(err, savingsgoal.ErrGoalNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("goal template"))
	default:
		logpkg.FromContext(r.Context()).Error("admin handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}

// listGoalTemplates handles GET /api/v1/admin/savings-goal-templates (#919):
// returns the full catalog (pre-built defaults plus any admin-published
// templates) for the admin catalog management UI.
func (h *AdminHandler) listGoalTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.service.ListGoalTemplates(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(templates))
}

type createGoalTemplateRequest struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Category        string          `json:"category"`
	SuggestedAmount decimal.Decimal `json:"suggested_amount"`
	Currency        string          `json:"currency"`
	SuggestedMonths int             `json:"suggested_months"`
	Icon            string          `json:"icon"`
}

// createGoalTemplate handles POST /api/v1/admin/savings-goal-templates
// (#919): lets admins publish a curated template without a redeploy, on top
// of the fixed pre-built set from #778.
func (h *AdminHandler) createGoalTemplate(w http.ResponseWriter, r *http.Request) {
	var req createGoalTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	template, err := h.service.CreateGoalTemplate(r.Context(), service.CreateGoalTemplateInput{
		Name:            req.Name,
		Description:     req.Description,
		Category:        req.Category,
		SuggestedAmount: req.SuggestedAmount,
		Currency:        req.Currency,
		SuggestedMonths: req.SuggestedMonths,
		Icon:            req.Icon,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(template))
}

type updateGoalTemplateRequest struct {
	Name            *string          `json:"name,omitempty"`
	Description     *string          `json:"description,omitempty"`
	Category        *string          `json:"category,omitempty"`
	SuggestedAmount *decimal.Decimal `json:"suggested_amount,omitempty"`
	Currency        *string          `json:"currency,omitempty"`
	SuggestedMonths *int             `json:"suggested_months,omitempty"`
	Icon            *string          `json:"icon,omitempty"`
}

// updateGoalTemplate handles PATCH /api/v1/admin/savings-goal-templates/{id} (#919).
func (h *AdminHandler) updateGoalTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("template id must be a valid UUID"))
		return
	}

	var req updateGoalTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	template, err := h.service.UpdateGoalTemplate(r.Context(), service.UpdateGoalTemplateInput{
		ID:              id,
		Name:            req.Name,
		Description:     req.Description,
		Category:        req.Category,
		SuggestedAmount: req.SuggestedAmount,
		Currency:        req.Currency,
		SuggestedMonths: req.SuggestedMonths,
		Icon:            req.Icon,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(template))
}

// deleteGoalTemplate handles DELETE /api/v1/admin/savings-goal-templates/{id} (#919).
func (h *AdminHandler) deleteGoalTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("template id must be a valid UUID"))
		return
	}
	if err := h.service.DeleteGoalTemplate(r.Context(), id); err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(map[string]string{"status": "deleted"}))
}
