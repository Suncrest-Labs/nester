package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type SettlementHandler struct {
	service     *service.SettlementService
	userService *service.UserService
}

func NewSettlementHandler(svc *service.SettlementService, userSvc *service.UserService) *SettlementHandler {
	return &SettlementHandler{service: svc, userService: userSvc}
}

func (h *SettlementHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/settlements", h.initiateSettlement)
	mux.HandleFunc("GET /api/v1/settlements/{id}", h.getSettlement)
	mux.HandleFunc("GET /api/v1/settlements", h.listUserSettlements)
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
	UserID        string              `json:"user_id"`
	VaultID       string              `json:"vault_id"`
	Amount        string              `json:"amount"`
	Currency      string              `json:"currency"`
	FiatCurrency  string              `json:"fiat_currency"`
	FiatAmount    string              `json:"fiat_amount"`
	ExchangeRate  string              `json:"exchange_rate"`
	BankAccountID *string             `json:"bank_account_id,omitempty"`
	Destination   *destinationRequest `json:"destination,omitempty"`
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

	caller, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	userID, err := uuid.Parse(caller.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid caller identity"))
		return
	}

	u, err := h.userService.GetUser(r.Context(), userID)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to check kyc status"))
		return
	}

	if string(u.KYCStatus) != "verified" {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "kyc_required", "kyc verification is required"))
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

	input := service.InitiateSettlementInput{
		UserID:       userID,
		VaultID:      vaultID,
		Amount:       amount,
		Currency:     req.Currency,
		FiatCurrency: req.FiatCurrency,
		FiatAmount:   fiatAmount,
		ExchangeRate: exchangeRate,
	}
	if req.BankAccountID != nil && strings.TrimSpace(*req.BankAccountID) != "" {
		acctID, parseErr := uuid.Parse(strings.TrimSpace(*req.BankAccountID))
		if parseErr != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("bank_account_id must be a valid UUID"))
			return
		}
		input.BankAccountID = &acctID
	} else if req.Destination != nil {
		input.Destination = offramp.Destination{
			Type:          req.Destination.Type,
			Provider:      req.Destination.Provider,
			AccountNumber: req.Destination.AccountNumber,
			AccountName:   req.Destination.AccountName,
			BankCode:      req.Destination.BankCode,
		}
	} else {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("either bank_account_id or destination is required"))
		return
	}

	model, err := h.service.InitiateSettlement(r.Context(), input)
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

	caller, ok := auth.GetUserFromContext(r.Context())
	if !ok || model.UserID.String() != caller.ID {
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("settlement"))
		return
	}

	// Always set status in response
	if model.Status == "" {
		model.Status = "initiated"
	}
	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *SettlementHandler) listUserSettlements(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.URL.Query().Get("userId"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("user id must be a valid UUID"))
		return
	}

	params, err := listquery.ParseSettlementList(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	models, total, nextCursor, err := h.service.ListUserSettlements(r.Context(), userID, offramp.UserListFilter{
		Page:                params.Page.Page,
		PerPage:             params.Page.PerPage,
		SortField:           params.Sort.Field,
		SortOrder:           params.Sort.Order,
		Cursor:              params.Page.Cursor,
		Status:              params.Status,
		DateFrom:            params.DateFrom,
		DateTo:              params.DateTo,
		MinAmount:           params.MinAmount,
		DestinationProvider: params.DestinationProvider,
		FiatCurrency:        params.FiatCurrency,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if models == nil {
		models = []offramp.Settlement{}
	}
	response.WriteJSON(w, http.StatusOK, response.PaginatedOK(models, params.Page.Page, params.Page.PerPage, total, nextCursor))
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

	newStatus, err := offramp.ParseStatus(req.Status)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("status: "+err.Error()))
		return
	}

	model, err := h.service.UpdateStatus(r.Context(), service.UpdateStatusInput{
		SettlementID: id,
		CallerID:     callerID,
		NewStatus:    newStatus,
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
