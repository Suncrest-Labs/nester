package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type UserHandler struct {
	service   *service.UserService
	validator *validator.Validate
}

func NewUserHandler(service *service.UserService) *UserHandler {
	return &UserHandler{
		service:   service,
		validator: validator.New(validator.WithRequiredStructEnabled()),
	}
}

type registerUserRequest struct {
	WalletAddress string `json:"wallet_address" validate:"required"`
	DisplayName   string `json:"display_name" validate:"required"`
}

func (h *UserHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/users", h.registerUser)
	mux.HandleFunc("GET /api/v1/users/wallet/{address}", h.getUserByWallet)
	mux.HandleFunc("GET /api/v1/users/profile", h.getProfile)
	mux.HandleFunc("PATCH /api/v1/users/profile", h.updateProfile)
	mux.HandleFunc("GET /api/v1/users/{id}", h.getUserByID)
}

func (h *UserHandler) registerUser(w http.ResponseWriter, r *http.Request) {
	var req registerUserRequest
	if err := h.decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	if err := h.validator.Struct(req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	model, err := h.service.RegisterUser(r.Context(), req.WalletAddress, req.DisplayName)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusCreated, response.Created(model))
}

func (h *UserHandler) getUserByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid user ID"))
		return
	}

	model, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *UserHandler) authorizeUserAccess(w http.ResponseWriter, r *http.Request, userID uuid.UUID) bool {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return false
	}
	if user.ID != userID.String() {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return false
	}
	return true
}

func (h *UserHandler) getProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	model, err := h.service.GetProfile(r.Context(), userID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

type updateProfileRequest struct {
	RiskProfile         *string `json:"risk_profile"`
	SavingsGoal         *string `json:"savings_goal"`
	OnboardingCompleted *bool   `json:"onboarding_completed"`
}

func (h *UserHandler) updateProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticatedUserID(w, r)
	if !ok {
		return
	}
	var req updateProfileRequest
	if err := h.decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}
	in := service.UpdateProfileInput{
		SavingsGoal:         req.SavingsGoal,
		OnboardingCompleted: req.OnboardingCompleted,
	}
	if req.RiskProfile != nil {
		rp := strings.ToLower(strings.TrimSpace(*req.RiskProfile))
		switch user.RiskProfile(rp) {
		case user.RiskProfileConservative, user.RiskProfileModerate, user.RiskProfileAggressive:
			profile := user.RiskProfile(rp)
			in.RiskProfile = &profile
		default:
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("risk_profile must be conservative, moderate, or aggressive"))
			return
		}
	}
	model, err := h.service.UpdateProfile(r.Context(), userID, in)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *UserHandler) authenticatedUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	u, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "authentication required"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(u.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid user identity"))
		return uuid.Nil, false
	}
	return id, true
}

func (h *UserHandler) getUserByWallet(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")
	if address == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("wallet address is required"))
		return
	}

	model, err := h.service.GetUserByWallet(r.Context(), address)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *UserHandler) decodeJSON(r *http.Request, destination any) error {
	const maxBodyBytes = 1 << 20 // 1MB
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain only one JSON object")
	}

	return nil
}

func (h *UserHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, user.ErrUserNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("user"))
	case errors.Is(err, user.ErrDuplicateWallet):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "CONFLICT", err.Error()))
	case errors.Is(err, user.ErrInvalidWallet):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("user handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
