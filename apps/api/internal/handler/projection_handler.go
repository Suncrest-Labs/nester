package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/projection"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savingsgoal"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// ProjectionHandler handles compound interest projection endpoints
type ProjectionHandler struct {
	service *service.ProjectionService
}

// NewProjectionHandler creates a new projection handler
func NewProjectionHandler(service *service.ProjectionService) *ProjectionHandler {
	return &ProjectionHandler{service: service}
}

// Register registers the projection routes
func (h *ProjectionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/tools/projection", h.calculateGenericProjection)
	mux.HandleFunc("GET /api/v1/vaults/{id}/projection", h.calculateVaultProjection)
	// Monte Carlo savings forecast (#843): P10/P50/P90 band, goal-success
	// probability, and a deposit/deadline sensitivity grid. Requires auth
	// (unlike the deterministic generic-projection endpoint above) since a
	// request can carry a goal_id, and goal-success resolution reads that
	// goal's target amount and deadline.
	mux.HandleFunc("POST /api/v1/tools/simulation", h.simulateProjection)
}

// genericProjectionRequest represents the JSON payload for generic projections
type genericProjectionRequest struct {
	InitialDeposit      string `json:"initial_deposit"`
	MonthlyContribution string `json:"monthly_contribution"`
	APY                 string `json:"apy"`
	PeriodMonths        int    `json:"period_months"`
	CompoundFrequency   string `json:"compound_frequency"`
}

// Validate validates the request
func (r *genericProjectionRequest) Validate() error {
	if r.InitialDeposit == "" {
		return projection.ErrInvalidAmount
	}
	if r.APY == "" {
		return projection.ErrInvalidAPY
	}
	if r.PeriodMonths <= 0 {
		return projection.ErrInvalidPeriod
	}
	if r.CompoundFrequency == "" {
		r.CompoundFrequency = "monthly" // default
	}
	return nil
}

// toProjectionInput converts the request to domain input
func (r *genericProjectionRequest) toProjectionInput() (projection.ProjectionInput, error) {
	initialDeposit, err := decimal.NewFromString(r.InitialDeposit)
	if err != nil {
		return projection.ProjectionInput{}, projection.ErrInvalidAmount
	}

	monthlyContribution := decimal.Zero
	if r.MonthlyContribution != "" {
		monthlyContribution, err = decimal.NewFromString(r.MonthlyContribution)
		if err != nil {
			return projection.ProjectionInput{}, projection.ErrInvalidAmount
		}
	}

	apy, err := decimal.NewFromString(r.APY)
	if err != nil {
		return projection.ProjectionInput{}, projection.ErrInvalidAPY
	}

	compoundFreq, err := projection.ParseCompoundFrequency(r.CompoundFrequency)
	if err != nil {
		return projection.ProjectionInput{}, err
	}

	return projection.ProjectionInput{
		InitialDeposit:      initialDeposit,
		MonthlyContribution: monthlyContribution,
		APY:                 apy,
		PeriodMonths:        r.PeriodMonths,
		CompoundFrequency:   compoundFreq,
	}, nil
}

// calculateGenericProjection handles POST /api/v1/tools/projection
func (h *ProjectionHandler) calculateGenericProjection(w http.ResponseWriter, r *http.Request) {
	var req genericProjectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON payload"))
		return
	}

	if err := req.Validate(); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	input, err := req.toProjectionInput()
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	result, err := h.service.CalculateCompoundProjection(r.Context(), input)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "CALCULATION_ERROR", err.Error()))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

// calculateVaultProjection handles GET /api/v1/vaults/{id}/projection
func (h *ProjectionHandler) calculateVaultProjection(w http.ResponseWriter, r *http.Request) {
	// Parse vault ID
	vaultIDStr := r.PathValue("id")
	vaultID, err := uuid.Parse(vaultIDStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid vault ID"))
		return
	}

	// Extract query parameters
	query := r.URL.Query()

	// Required: deposit amount
	depositStr := query.Get("deposit")
	if depositStr == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("deposit parameter is required"))
		return
	}

	deposit, err := decimal.NewFromString(depositStr)
	if err != nil || deposit.LessThanOrEqual(decimal.Zero) {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid deposit amount"))
		return
	}

	// Required: period
	period := query.Get("period")
	if period == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("period parameter is required"))
		return
	}

	// Optional: compound frequency (default: monthly)
	compound := query.Get("compound")
	if compound == "" {
		compound = "monthly"
	}

	// Optional: APY override
	var apyOverride *decimal.Decimal
	if apyStr := query.Get("apy"); apyStr != "" {
		apy, err := decimal.NewFromString(apyStr)
		if err != nil || apy.LessThanOrEqual(decimal.Zero) {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid APY override"))
			return
		}
		apyOverride = &apy
	}

	// Verify user has access to this vault (basic auth check)
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return
	}

	_, err = uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user ID"))
		return
	}

	input := projection.VaultProjectionInput{
		VaultID:           vaultID,
		Deposit:           deposit,
		Period:            period,
		CompoundFrequency: compound,
		APYOverride:       apyOverride,
	}

	result, err := h.service.CalculateVaultProjection(r.Context(), input)
	if err != nil {
		if err.Error() == "vault not found" {
			response.WriteJSON(w, http.StatusNotFound, response.NotFound("vault"))
			return
		}
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "CALCULATION_ERROR", err.Error()))
		return
	}

	// Additional auth check: ensure vault belongs to user (if needed)
	// This would require updating the service to return vault info or checking permissions

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

// simulationRequest is the JSON payload for POST /api/v1/tools/simulation.
// Amounts are wire-format strings (matching genericProjectionRequest above)
// rather than the domain projection.SimulationInput's native decimal.Decimal
// / projection.CompoundFrequency types, so the wire format stays consistent
// with the rest of this handler (e.g. "monthly"/"daily" rather than a raw
// enum int).
type simulationRequest struct {
	VaultID             string `json:"vault_id,omitempty"`
	GoalID              string `json:"goal_id,omitempty"`
	InitialDeposit      string `json:"initial_deposit"`
	MonthlyContribution string `json:"monthly_contribution"`
	APY                 string `json:"apy,omitempty"`
	PeriodMonths        int    `json:"period_months,omitempty"`
	CompoundFrequency   string `json:"compound_frequency,omitempty"`
	TargetAmount        string `json:"target_amount,omitempty"`
	DeadlineMonths      int    `json:"deadline_months,omitempty"`
	PathCount           int    `json:"path_count,omitempty"`
}

// toSimulationInput converts the wire request into the domain type consumed
// by ProjectionService.SimulateVaultProjection.
func (r *simulationRequest) toSimulationInput() (projection.SimulationInput, error) {
	var input projection.SimulationInput

	if r.VaultID != "" {
		id, err := uuid.Parse(r.VaultID)
		if err != nil {
			return input, errors.New("invalid vault_id")
		}
		input.VaultID = &id
	}
	if r.GoalID != "" {
		id, err := uuid.Parse(r.GoalID)
		if err != nil {
			return input, errors.New("invalid goal_id")
		}
		input.GoalID = &id
	}

	if r.InitialDeposit != "" {
		v, err := decimal.NewFromString(r.InitialDeposit)
		if err != nil {
			return input, projection.ErrInvalidAmount
		}
		input.InitialDeposit = v
	}

	if r.MonthlyContribution != "" {
		v, err := decimal.NewFromString(r.MonthlyContribution)
		if err != nil {
			return input, projection.ErrInvalidAmount
		}
		input.MonthlyContribution = v
	}

	if r.APY != "" {
		v, err := decimal.NewFromString(r.APY)
		if err != nil {
			return input, projection.ErrInvalidAPY
		}
		input.APY = &v
	}

	input.PeriodMonths = r.PeriodMonths

	freqStr := r.CompoundFrequency
	if freqStr == "" {
		freqStr = "monthly"
	}
	freq, err := projection.ParseCompoundFrequency(freqStr)
	if err != nil {
		return input, err
	}
	input.CompoundFrequency = freq

	if r.TargetAmount != "" {
		v, err := decimal.NewFromString(r.TargetAmount)
		if err != nil {
			return input, errors.New("invalid target_amount")
		}
		input.TargetAmount = &v
	}
	if r.DeadlineMonths > 0 {
		d := r.DeadlineMonths
		input.DeadlineMonths = &d
	}
	input.PathCount = r.PathCount

	return input, nil
}

// simulateProjection handles POST /api/v1/tools/simulation: the Monte Carlo
// savings forecast (#843).
func (h *ProjectionHandler) simulateProjection(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user ID"))
		return
	}

	var req simulationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid JSON payload"))
		return
	}

	input, err := req.toSimulationInput()
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	result, err := h.service.SimulateVaultProjection(r.Context(), userID, input)
	if err != nil {
		switch {
		case errors.Is(err, savingsgoal.ErrUnauthorized):
			response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", err.Error()))
		case errors.Is(err, vault.ErrVaultNotFound), errors.Is(err, savingsgoal.ErrGoalNotFound):
			response.WriteJSON(w, http.StatusNotFound, response.NotFound("resource"))
		case errors.Is(err, projection.ErrInvalidAmount),
			errors.Is(err, projection.ErrInvalidAPY),
			errors.Is(err, projection.ErrInvalidPeriod):
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		default:
			response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "CALCULATION_ERROR", err.Error()))
		}
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}
