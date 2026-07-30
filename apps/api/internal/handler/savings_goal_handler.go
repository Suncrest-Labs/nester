package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/goalnotification"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/intelligence"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type SavingsGoalManager interface {
	Create(ctx context.Context, userID uuid.UUID, in service.CreateSavingsGoalInput) (savingsgoal.SavingsGoal, error)
	Get(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error)
	List(ctx context.Context, userID uuid.UUID, category, status string, includeArchived bool) ([]savingsgoal.SavingsGoal, error)
	ListPaginated(ctx context.Context, userID uuid.UUID, filter service.SavingsGoalListFilter) ([]savingsgoal.SavingsGoal, int, error)
	Update(ctx context.Context, userID, goalID uuid.UUID, in service.UpdateSavingsGoalInput) (savingsgoal.SavingsGoal, error)
	Delete(ctx context.Context, userID, goalID uuid.UUID) error
	Restore(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error)
	Summary(ctx context.Context, userID uuid.UUID) (savingsgoal.SavingsGoalsSummary, error)
	Pause(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error)
	Resume(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error)
	Complete(ctx context.Context, userID, goalID uuid.UUID, action string) (savingsgoal.SavingsGoal, error)
	Archive(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error)
	Unarchive(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error)
	DepositSplit(ctx context.Context, userID uuid.UUID, in service.DepositSplitInput) (service.SplitDepositResult, error)
	Share(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error)
	Unshare(ctx context.Context, userID, goalID uuid.UUID) error
	GetShared(ctx context.Context, token uuid.UUID) (savingsgoal.SharedGoalView, error)
	ListContributions(ctx context.Context, userID, goalID uuid.UUID, params listquery.PageParams) ([]savingsgoal.GoalContribution, int, string, error)
	ListTemplates(ctx context.Context) ([]savingsgoal.GoalTemplate, error)
	CreateFromTemplate(ctx context.Context, userID uuid.UUID, in service.CreateFromTemplateInput) (savingsgoal.SavingsGoal, error)
}

type SavingsGoalHandler struct {
	svc              SavingsGoalManager
	schedules        SavingsScheduleActiveReader
	notifyPrefs      GoalNotificationPreferenceManager
	coachingProvider GoalCoachingProvider
}

type SavingsScheduleActiveReader interface {
	GetActive(ctx context.Context, userID, goalID uuid.UUID) (*savingsschedule.SavingsSchedule, error)
}

// GoalNotificationPreferenceManager manages per-goal mute/digest-frequency settings.
type GoalNotificationPreferenceManager interface {
	Get(ctx context.Context, userID, goalID uuid.UUID) (goalnotification.Preference, error)
	Update(ctx context.Context, userID, goalID uuid.UUID, in service.UpdateGoalNotificationPreferenceInput) (goalnotification.Preference, error)
}

// GoalCoachingProvider requests an AI-generated progress assessment and
// deposit schedule for a savings goal from the intelligence service (#112).
type GoalCoachingProvider interface {
	GetGoalCoaching(ctx context.Context, request intelligence.CoachingRequest) (*intelligence.CoachingResponse, error)
}

func NewSavingsGoalHandler(svc SavingsGoalManager, schedules SavingsScheduleActiveReader) *SavingsGoalHandler {
	return &SavingsGoalHandler{svc: svc, schedules: schedules}
}

// SetNotificationPreferenceManager wires the optional per-goal notification
// preference endpoints (mute/frequency). Left unset, those endpoints respond 501.
func (h *SavingsGoalHandler) SetNotificationPreferenceManager(m GoalNotificationPreferenceManager) {
	h.notifyPrefs = m
}

// SetCoachingProvider wires the AI coaching backend. Left nil, the
// /coaching endpoint responds 503 rather than failing to construct the handler.
func (h *SavingsGoalHandler) SetCoachingProvider(p GoalCoachingProvider) {
	h.coachingProvider = p
}

func (h *SavingsGoalHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/users/savings-goals", h.create)
	mux.HandleFunc("GET /api/v1/users/savings-goals/summary", h.summary)
	mux.HandleFunc("GET /api/v1/users/savings-goals", h.list)
	mux.HandleFunc("GET /api/v1/users/savings-goals/{id}", h.get)
	mux.HandleFunc("PATCH /api/v1/users/savings-goals/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/users/savings-goals/{id}", h.delete)
	// #924 soft-delete recovery
	mux.HandleFunc("POST /api/v1/users/savings-goals/{id}/restore", h.restore)
	// #718 pause/resume
	mux.HandleFunc("PATCH /api/v1/users/savings-goals/{id}/pause", h.pause)
	mux.HandleFunc("PATCH /api/v1/users/savings-goals/{id}/resume", h.resume)
	mux.HandleFunc("GET /api/v1/users/savings-goals/{id}/contributions", h.listContributions)
	// Per-goal notification preferences (mute/digest frequency).
	mux.HandleFunc("GET /api/v1/users/savings-goals/{id}/notification-preferences", h.getNotificationPreference)
	mux.HandleFunc("PATCH /api/v1/users/savings-goals/{id}/notification-preferences", h.updateNotificationPreference)
	// #112 AI progress coaching
	mux.HandleFunc("GET /api/v1/users/savings-goals/{id}/coaching", h.coaching)
	// #716 manual completion
	mux.HandleFunc("POST /api/v1/users/savings-goals/{id}/complete", h.complete)
	// #684 archive / #721 unarchive
	mux.HandleFunc("PATCH /api/v1/users/savings-goals/{id}/archive", h.archive)
	mux.HandleFunc("PATCH /api/v1/users/savings-goals/{id}/unarchive", h.unarchive)
	// #719 multi-goal deposit split
	mux.HandleFunc("POST /api/v1/users/savings-goals/deposit", h.splitDeposit)
	// Goal sharing — unauthenticated read is public; share/unshare require auth.
	mux.HandleFunc("POST /api/v1/users/savings-goals/{id}/share", h.share)
	mux.HandleFunc("DELETE /api/v1/users/savings-goals/{id}/share", h.unshare)
	mux.HandleFunc("GET /api/v1/savings-goals/shared/{token}", h.getShared)

	// #7xx templates
	mux.HandleFunc("GET /api/v1/savings-goal-templates", h.listTemplates)
	mux.HandleFunc("POST /api/v1/users/savings-goals/from-template", h.createFromTemplate)
}

type createSavingsGoalRequest struct {
	TargetAmount    json.Number  `json:"target_amount"`
	Currency        string       `json:"currency"`
	Deadline        string       `json:"deadline"`
	Description     string       `json:"description"`
	Category        string       `json:"category"`
	Name            string       `json:"name"`
	Emoji           string       `json:"emoji"`
	VaultID         *string      `json:"vault_id,omitempty"`
	MinContribution *json.Number `json:"min_contribution,omitempty"`
	MaxContribution *json.Number `json:"max_contribution,omitempty"`
}

type updateSavingsGoalRequest struct {
	TargetAmount    *json.Number `json:"target_amount"`
	Currency        *string      `json:"currency"`
	Deadline        *string      `json:"deadline"`
	Description     *string      `json:"description"`
	Category        *string      `json:"category"`
	Name            *string      `json:"name"`
	Emoji           *string      `json:"emoji"`
	AutoCompound    *bool        `json:"auto_compound"`
	MinContribution *json.Number `json:"min_contribution,omitempty"`
	MaxContribution *json.Number `json:"max_contribution,omitempty"`
}

func (h *SavingsGoalHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	var req createSavingsGoalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}
	target, err := parseTargetAmount(req.TargetAmount)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	deadline, err := time.Parse(time.RFC3339, req.Deadline)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("deadline must be RFC3339"))
		return
	}
	vaultID, err := parseOptionalUUID(req.VaultID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault_id must be a valid UUID"))
		return
	}
	minContribution, err := parseOptionalDecimal(req.MinContribution)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("min_contribution must be a valid number"))
		return
	}
	maxContribution, err := parseOptionalDecimal(req.MaxContribution)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("max_contribution must be a valid number"))
		return
	}
	goal, err := h.svc.Create(r.Context(), userID, service.CreateSavingsGoalInput{
		TargetAmount:    target,
		Currency:        req.Currency,
		Deadline:        deadline,
		Description:     req.Description,
		Category:        req.Category,
		Name:            req.Name,
		Emoji:           req.Emoji,
		VaultID:         vaultID,
		MinContribution: minContribution,
		MaxContribution: maxContribution,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(goal))
}

func (h *SavingsGoalHandler) listTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.svc.ListTemplates(r.Context())
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(templates))
}

type createFromTemplateRequest struct {
	TemplateID     string       `json:"template_id"`
	OverrideAmount *json.Number `json:"override_amount"`
	OverrideMonths *int         `json:"override_months"`
	VaultID        *string      `json:"vault_id,omitempty"`
}

func (h *SavingsGoalHandler) createFromTemplate(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	var req createFromTemplateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}

	templateID, err := uuid.Parse(req.TemplateID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("template_id must be a valid UUID"))
		return
	}

	in := service.CreateFromTemplateInput{
		TemplateID: templateID,
	}
	if req.OverrideAmount != nil {
		amount, err := parseTargetAmount(*req.OverrideAmount)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
		in.OverrideAmount = &amount
	}
	if req.OverrideMonths != nil {
		in.OverrideMonths = req.OverrideMonths
	}
	vaultID, err := parseOptionalUUID(req.VaultID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault_id must be a valid UUID"))
		return
	}
	in.VaultID = vaultID

	goal, err := h.svc.CreateFromTemplate(r.Context(), userID, in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(goal))
}

func (h *SavingsGoalHandler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	goal, err := h.svc.Get(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	detail := savingsGoalDetail{SavingsGoal: goal}
	if h.schedules != nil {
		if active, err := h.schedules.GetActive(r.Context(), userID, goalID); err == nil {
			detail.ActiveSchedule = active
		}
	}
	response.WriteJSON(w, http.StatusOK, response.OK(detail))
}

type savingsGoalDetail struct {
	savingsgoal.SavingsGoal
	ActiveSchedule *savingsschedule.SavingsSchedule `json:"active_schedule,omitempty"`
}

// coaching returns an on-demand AI-generated progress assessment and deposit
// schedule for the goal (#112). The same underlying call is made on a
// weekly cadence by GoalCoachingScheduler so users get a fresh nudge even
// without opening the app.
func (h *SavingsGoalHandler) coaching(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	if h.coachingProvider == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "coaching service not configured"))
		return
	}
	goal, err := h.svc.Get(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	result, err := h.coachingProvider.GetGoalCoaching(r.Context(), goalCoachingRequest(goal))
	if err != nil {
		logpkg.FromContext(r.Context()).Error("goal coaching failed", "error", err.Error())
		response.WriteJSON(w, http.StatusBadGateway, response.Err(http.StatusBadGateway, "UPSTREAM_ERROR", err.Error()))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

// goalCoachingRequest builds the intelligence service request payload from an
// already-progress-enriched savings goal.
func goalCoachingRequest(goal savingsgoal.SavingsGoal) intelligence.CoachingRequest {
	targetAmount, _ := goal.TargetAmount.Float64()
	currentAmount, _ := goal.CurrentAmount.Float64()
	return intelligence.CoachingRequest{
		Goal: intelligence.SavingsGoalContext{
			ID:            goal.ID.String(),
			TargetAmount:  targetAmount,
			Currency:      goal.Currency,
			Deadline:      goal.Deadline.Format(time.RFC3339),
			Description:   goal.Description,
			CurrentAmount: currentAmount,
			ProgressPct:   goal.ProgressPct,
		},
		Portfolio: intelligence.PortfolioContext{
			TotalBalanceUSD: currentAmount,
		},
	}
}

func (h *SavingsGoalHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	params, err := listquery.ParseSavingsGoalList(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	goals, total, err := h.svc.ListPaginated(r.Context(), userID, service.SavingsGoalListFilter{
		Page:            params.Page.Page,
		PerPage:         params.Page.PerPage,
		SortField:       params.Sort.Field,
		SortOrder:       params.Sort.Order,
		Category:        params.Category,
		Status:          params.Status,
		IncludeArchived: params.IncludeArchived,
		Search:          params.Search,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if goals == nil {
		goals = []savingsgoal.SavingsGoal{}
	}
	response.WriteJSON(w, http.StatusOK, response.PaginatedOK(goals, params.Page.Page, params.Page.PerPage, total, ""))
}

func (h *SavingsGoalHandler) summary(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	summary, err := h.svc.Summary(r.Context(), userID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=30")
	response.WriteJSON(w, http.StatusOK, response.OK(summary))
}

func (h *SavingsGoalHandler) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	var req updateSavingsGoalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}
	in := service.UpdateSavingsGoalInput{}
	if req.TargetAmount != nil {
		t, err := parseTargetAmount(*req.TargetAmount)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
		in.TargetAmount = &t
	}
	if req.Currency != nil {
		in.Currency = req.Currency
	}
	if req.Deadline != nil {
		d, err := time.Parse(time.RFC3339, *req.Deadline)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("deadline must be RFC3339"))
			return
		}
		in.Deadline = &d
	}
	in.Description = req.Description
	in.Category = req.Category
	in.Name = req.Name
	in.Emoji = req.Emoji
	in.AutoCompound = req.AutoCompound
	minContribution, err := parseOptionalDecimal(req.MinContribution)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("min_contribution must be a valid number"))
		return
	}
	maxContribution, err := parseOptionalDecimal(req.MaxContribution)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("max_contribution must be a valid number"))
		return
	}
	in.MinContribution = minContribution
	in.MaxContribution = maxContribution

	goal, err := h.svc.Update(r.Context(), userID, goalID, in)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

func (h *SavingsGoalHandler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	if err := h.svc.Delete(r.Context(), userID, goalID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// restore undoes a soft delete within the recovery window (#924).
func (h *SavingsGoalHandler) restore(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	goal, err := h.svc.Restore(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

func (h *SavingsGoalHandler) pause(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	goal, err := h.svc.Pause(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

func (h *SavingsGoalHandler) resume(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	goal, err := h.svc.Resume(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

func (h *SavingsGoalHandler) archive(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	goal, err := h.svc.Archive(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

func (h *SavingsGoalHandler) unarchive(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	goal, err := h.svc.Unarchive(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

type completeGoalRequest struct {
	Action string `json:"action"`
}

func (h *SavingsGoalHandler) complete(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	var req completeGoalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}
	goal, err := h.svc.Complete(r.Context(), userID, goalID, req.Action)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

type splitDepositAllocationRequest struct {
	GoalID     string `json:"goal_id"`
	Amount     string `json:"amount"`
	Percentage string `json:"percentage"`
}

type splitDepositRequest struct {
	TotalAmount string                          `json:"total_amount"`
	Currency    string                          `json:"currency"`
	Allocations []splitDepositAllocationRequest `json:"allocations"`
}

func (h *SavingsGoalHandler) splitDeposit(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	var req splitDepositRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}
	totalAmount, err := decimal.NewFromString(req.TotalAmount)
	if err != nil || !totalAmount.IsPositive() {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("total_amount must be a positive decimal"))
		return
	}
	if len(req.Allocations) == 0 {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("allocations must not be empty"))
		return
	}
	allocations := make([]service.DepositSplitAllocation, len(req.Allocations))
	for i, a := range req.Allocations {
		goalID, err := uuid.Parse(a.GoalID)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("allocation goal_id must be a valid UUID"))
			return
		}
		var amt, pct decimal.Decimal
		if a.Amount != "" {
			amt, err = decimal.NewFromString(a.Amount)
			if err != nil {
				response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("allocation amount must be a valid decimal"))
				return
			}
		}
		if a.Percentage != "" {
			pct, err = decimal.NewFromString(a.Percentage)
			if err != nil {
				response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("allocation percentage must be a valid decimal"))
				return
			}
		}
		allocations[i] = service.DepositSplitAllocation{
			GoalID:     goalID,
			Amount:     amt,
			Percentage: pct,
		}
	}
	result, err := h.svc.DepositSplit(r.Context(), userID, service.DepositSplitInput{
		TotalAmount: totalAmount,
		Currency:    req.Currency,
		Allocations: allocations,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(result))
}

func (h *SavingsGoalHandler) listContributions(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	params, err := listquery.ParsePage(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	contributions, total, nextCursor, err := h.svc.ListContributions(r.Context(), userID, goalID, params)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.PaginatedOK(contributions, params.Page, params.PerPage, total, nextCursor))
}

func (h *SavingsGoalHandler) getNotificationPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	if h.notifyPrefs == nil {
		response.WriteJSON(w, http.StatusNotImplemented, response.Err(http.StatusNotImplemented, "NOT_IMPLEMENTED", "notification preferences are not available"))
		return
	}
	pref, err := h.notifyPrefs.Get(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(pref))
}

type updateGoalNotificationPreferenceRequest struct {
	Muted           *bool   `json:"muted"`
	DigestFrequency *string `json:"digest_frequency"`
}

func (h *SavingsGoalHandler) updateNotificationPreference(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	if h.notifyPrefs == nil {
		response.WriteJSON(w, http.StatusNotImplemented, response.Err(http.StatusNotImplemented, "NOT_IMPLEMENTED", "notification preferences are not available"))
		return
	}
	body, err := readJSONBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	var req updateGoalNotificationPreferenceRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}
	pref, err := h.notifyPrefs.Update(r.Context(), userID, goalID, service.UpdateGoalNotificationPreferenceInput{
		Muted:           req.Muted,
		DigestFrequency: req.DigestFrequency,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(pref))
}

func (h *SavingsGoalHandler) authenticatedUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user identity"))
		return uuid.Nil, false
	}
	return id, true
}

func (h *SavingsGoalHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, savingsgoal.ErrGoalNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("savings goal"))
	case errors.Is(err, savingsgoal.ErrGoalCompleted):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "GOAL_COMPLETED", err.Error()))
	case errors.Is(err, savingsgoal.ErrGoalPaused):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "GOAL_PAUSED", err.Error()))
	case errors.Is(err, savingsgoal.ErrGoalArchived):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "GOAL_ARCHIVED", err.Error()))
	case errors.Is(err, savingsgoal.ErrGoalNotDeleted):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "GOAL_NOT_DELETED", err.Error()))
	case errors.Is(err, savingsgoal.ErrRecoveryWindowExpired):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "RECOVERY_WINDOW_EXPIRED", err.Error()))
	case errors.Is(err, savingsgoal.ErrUnauthorized):
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "vault does not belong to you"))
	case errors.Is(err, savingsgoal.ErrInvalidGoal):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, goalnotification.ErrInvalidPreference):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, savingsgoal.ErrInvalidAmount):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, savingsgoal.ErrInvalidContributionLimits):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, savingsgoal.ErrContributionOutOfRange):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("savings goal handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}

// parseOptionalUUID parses an optional UUID field, treating an absent or blank
// value as "not provided" rather than an error.
func parseOptionalUUID(raw *string) (*uuid.UUID, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseTargetAmount(n json.Number) (decimal.Decimal, error) {
	f, err := n.Float64()
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(f), nil
}

// parseOptionalDecimal parses an optional per-contribution limit (#922),
// treating an absent field as "not provided" (nil, nil).
func parseOptionalDecimal(n *json.Number) (*decimal.Decimal, error) {
	if n == nil {
		return nil, nil
	}
	amount, err := parseTargetAmount(*n)
	if err != nil {
		return nil, err
	}
	return &amount, nil
}

func readJSONBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 8*1024))
}

func (h *SavingsGoalHandler) share(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	goal, err := h.svc.Share(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

func (h *SavingsGoalHandler) unshare(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	if err := h.svc.Unshare(r.Context(), userID, goalID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SavingsGoalHandler) getShared(w http.ResponseWriter, r *http.Request) {
	rawToken := r.PathValue("token")
	token, err := uuid.Parse(rawToken)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("share token must be a valid UUID"))
		return
	}
	view, err := h.svc.GetShared(r.Context(), token)
	if err != nil {
		if errors.Is(err, savingsgoal.ErrGoalNotFound) {
			response.WriteJSON(w, http.StatusNotFound, response.NotFound("shared goal"))
			return
		}
		logpkg.FromContext(r.Context()).Error("get shared goal failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(view))
}
