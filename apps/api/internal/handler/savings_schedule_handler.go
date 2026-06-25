package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsschedule"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type SavingsScheduleManager interface {
	Create(ctx context.Context, userID, goalID uuid.UUID, in service.CreateSavingsScheduleInput) (savingsschedule.SavingsSchedule, error)
	List(ctx context.Context, userID, goalID uuid.UUID) ([]savingsschedule.SavingsSchedule, error)
	Cancel(ctx context.Context, userID, goalID, scheduleID uuid.UUID) error
}

type SavingsScheduleHandler struct {
	svc SavingsScheduleManager
}

func NewSavingsScheduleHandler(svc SavingsScheduleManager) *SavingsScheduleHandler {
	return &SavingsScheduleHandler{svc: svc}
}

func (h *SavingsScheduleHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/users/savings-goals/{id}/schedules", h.create)
	mux.HandleFunc("GET /api/v1/users/savings-goals/{id}/schedules", h.list)
	mux.HandleFunc("DELETE /api/v1/users/savings-goals/{id}/schedules/{schedule_id}", h.cancel)
}

type createSavingsScheduleRequest struct {
	Amount    json.Number `json:"amount"`
	Currency  string      `json:"currency"`
	Frequency string      `json:"frequency"`
	VaultID   string      `json:"vault_id"`
}

func (h *SavingsScheduleHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8*1024))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	var req createSavingsScheduleRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON"))
		return
	}
	amount, err := parseScheduleAmount(req.Amount)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	vaultID, err := uuid.Parse(req.VaultID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault_id must be a valid UUID"))
		return
	}
	schedule, err := h.svc.Create(r.Context(), userID, goalID, service.CreateSavingsScheduleInput{
		Amount:    amount,
		Currency:  req.Currency,
		Frequency: req.Frequency,
		VaultID:   vaultID,
	})
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(schedule))
}

func (h *SavingsScheduleHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	schedules, err := h.svc.List(r.Context(), userID, goalID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(schedules))
}

func (h *SavingsScheduleHandler) cancel(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goalID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("goal id must be a valid UUID"))
		return
	}
	scheduleID, err := uuid.Parse(r.PathValue("schedule_id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("schedule id must be a valid UUID"))
		return
	}
	if err := h.svc.Cancel(r.Context(), userID, goalID, scheduleID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SavingsScheduleHandler) authenticatedUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
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

func (h *SavingsScheduleHandler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, savingsgoal.ErrGoalNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("savings goal"))
	case errors.Is(err, savingsschedule.ErrScheduleNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("savings schedule"))
	case errors.Is(err, savingsschedule.ErrActiveScheduleExists):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "ACTIVE_SCHEDULE_EXISTS", "an active schedule already exists for this goal"))
	case errors.Is(err, savingsschedule.ErrUnauthorizedVault):
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "vault does not belong to user"))
	case errors.Is(err, vault.ErrVaultNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("vault"))
	case errors.Is(err, savingsschedule.ErrInvalidSchedule):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("savings schedule handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}

func parseScheduleAmount(n json.Number) (decimal.Decimal, error) {
	f, err := n.Float64()
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(f), nil
}
