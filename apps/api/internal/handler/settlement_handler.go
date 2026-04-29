package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type SettlementHandler struct {
	service *service.SettlementService
}

func NewSettlementHandler(svc *service.SettlementService) *SettlementHandler {
	return &SettlementHandler{service: svc}
}

func (h *SettlementHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/settlements", h.initiateSettlement)
	mux.HandleFunc("GET /api/v1/settlements/{id}", h.getSettlement)
	mux.HandleFunc("GET /api/v1/users/{userId}/settlements", h.listUserSettlements)
	mux.HandleFunc("PATCH /api/v1/settlements/{id}/status", h.updateStatus)
}

// ── Request / Response types ────────────────────────────────────────────────

type destinationRequest struct {
	Type          string `json:"type"`
	Provider      string `json:"provider"`
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	BankCode      string `json:"bank_code"`
}

type initiateSettlementRequest struct {
	UserID       string             `json:"user_id"`
	VaultID      string             `json:"vault_id"`
	Amount       string             `json:"amount"`
	Currency     string             `json:"currency"`
	FiatCurrency string             `json:"fiat_currency"`
	FiatAmount   string             `json:"fiat_amount"`
	ExchangeRate string             `json:"exchange_rate"`
	Destination  destinationRequest `json:"destination"`
}

type updateStatusRequest struct {
	Status string `json:"status"`
}

// ── Handlers ────────────────────────────────────────────────────────────────

func (h *SettlementHandler) initiateSettlement(w http.ResponseWriter, r *http.Request) {
	var req initiateSettlementRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("user_id must be a valid UUID"))
		return
	}

	vaultID, err := uuid.Parse(req.VaultID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault_id must be a valid UUID"))
		return
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("amount must be a valid decimal number"))
		return
	}

	fiatAmount, err := decimal.NewFromString(req.FiatAmount)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("fiat_amount must be a valid decimal number"))
		return
	}

	exchangeRate, err := decimal.NewFromString(req.ExchangeRate)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("exchange_rate must be a valid decimal number"))
		return
	}

	model, err := h.service.InitiateSettlement(r.Context(), service.InitiateSettlementInput{
		UserID:       userID,
		VaultID:      vaultID,
		Amount:       amount,
		Currency:     req.Currency,
		FiatCurrency: req.FiatCurrency,
		FiatAmount:   fiatAmount,
		ExchangeRate: exchangeRate,
		Destination: offramp.Destination{
			Type:          req.Destination.Type,
			Provider:      req.Destination.Provider,
			AccountNumber: req.Destination.AccountNumber,
			AccountName:   req.Destination.AccountName,
			BankCode:      req.Destination.BankCode,
		},
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	// Always set status in response
	if model.Status == "" {
		model.Status = "initiated"
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(model))
}

func (h *SettlementHandler) getSettlement(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("settlement id must be a valid UUID"))
		return
	}

	model, err := h.service.GetSettlement(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	// Always set status in response
	if model.Status == "" {
		model.Status = "initiated"
	}
	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *SettlementHandler) listUserSettlements(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("userId"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("user id must be a valid UUID"))
		return
	}

	statusFilter := r.URL.Query().Get("status")

	models, err := h.service.GetUserSettlements(r.Context(), userID, statusFilter)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	// Always return an array, never an object
	if models == nil {
		models = []offramp.Settlement{}
	}
	response.WriteJSON(w, http.StatusOK, response.OK(models))
}

func (h *SettlementHandler) updateStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("settlement id must be a valid UUID"))
		return
	}

	caller, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}
	callerID, err := uuid.Parse(caller.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid caller identity"))
		return
	}

	var req updateStatusRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	model, err := h.service.UpdateStatus(r.Context(), service.UpdateStatusInput{
		SettlementID: id,
		CallerID:     callerID,
		NewStatus:    offramp.SettlementStatus(req.Status),
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

// ── Error mapping ────────────────────────────────────────────────────────────

func (h *SettlementHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, offramp.ErrForbidden):
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "you do not own this settlement"))
	case errors.Is(err, offramp.ErrSettlementNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("settlement"))
	case errors.Is(err, offramp.ErrUserNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("user"))
	case errors.Is(err, offramp.ErrVaultNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("vault"))
	case errors.Is(err, offramp.ErrInvalidSettlement),
		errors.Is(err, offramp.ErrInvalidAmount),
		errors.Is(err, offramp.ErrInvalidStatus),
		errors.Is(err, offramp.ErrInvalidTransition),
		errors.Is(err, offramp.ErrInvalidPrecision):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("settlement handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
