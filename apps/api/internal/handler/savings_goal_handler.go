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
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type SavingsGoalManager interface {
	Create(ctx context.Context, userID uuid.UUID, in service.CreateSavingsGoalInput) (savingsgoal.SavingsGoal, error)
	Get(ctx context.Context, userID, goalID uuid.UUID) (savingsgoal.SavingsGoal, error)
	List(ctx context.Context, userID uuid.UUID, category string) ([]savingsgoal.SavingsGoal, error)
	Update(ctx context.Context, userID, goalID uuid.UUID, in service.UpdateSavingsGoalInput) (savingsgoal.SavingsGoal, error)
	Delete(ctx context.Context, userID, goalID uuid.UUID) error
	Summary(ctx context.Context, userID uuid.UUID) (savingsgoal.SavingsGoalsSummary, error)
}

type SavingsGoalHandler struct {
	svc SavingsGoalManager
}

func NewSavingsGoalHandler(svc SavingsGoalManager) *SavingsGoalHandler {
	return &SavingsGoalHandler{svc: svc}
}

func (h *SavingsGoalHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/users/savings-goals", h.create)
	mux.HandleFunc("GET /api/v1/users/savings-goals/summary", h.summary)
	mux.HandleFunc("GET /api/v1/users/savings-goals", h.list)
	mux.HandleFunc("GET /api/v1/users/savings-goals/{id}", h.get)
	mux.HandleFunc("PATCH /api/v1/users/savings-goals/{id}", h.update)
	mux.HandleFunc("DELETE /api/v1/users/savings-goals/{id}", h.delete)
}

type createSavingsGoalRequest struct {
	TargetAmount json.Number `json:"target_amount"`
	Currency     string      `json:"currency"`
	Deadline     string      `json:"deadline"`
	Description  string      `json:"description"`
	Category     string      `json:"category"`
}

type updateSavingsGoalRequest struct {
	TargetAmount *json.Number `json:"target_amount"`
	Currency     *string      `json:"currency"`
	Deadline     *string      `json:"deadline"`
	Description  *string      `json:"description"`
	Category     *string      `json:"category"`
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
	goal, err := h.svc.Create(r.Context(), userID, service.CreateSavingsGoalInput{
		TargetAmount: target,
		Currency:     req.Currency,
		Deadline:     deadline,
		Description:  req.Description,
		Category:     req.Category,
	})
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
	response.WriteJSON(w, http.StatusOK, response.OK(goal))
}

func (h *SavingsGoalHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	goals, err := h.svc.List(r.Context(), userID, strings.TrimSpace(r.URL.Query().Get("category")))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if goals == nil {
		goals = []savingsgoal.SavingsGoal{}
	}
	response.WriteJSON(w, http.StatusOK, response.OK(goals))
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
	case errors.Is(err, savingsgoal.ErrInvalidGoal):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("savings goal handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}

func parseTargetAmount(n json.Number) (decimal.Decimal, error) {
	f, err := n.Float64()
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(f), nil
}

func readJSONBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 8*1024))
}
