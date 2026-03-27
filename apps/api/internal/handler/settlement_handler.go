package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

type SettlementHandler struct {
	service      *service.SettlementService
	vaultService *service.VaultService
}

func NewSettlementHandler(svc *service.SettlementService, vaultServices ...*service.VaultService) *SettlementHandler {
	handler := &SettlementHandler{service: svc}
	if len(vaultServices) > 0 {
		handler.vaultService = vaultServices[0]
	}
	return handler
}

func (h *SettlementHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/settlements", h.initiateSettlement)
	mux.HandleFunc("GET /api/v1/settlements/{id}", h.getSettlement)
	mux.HandleFunc("GET /api/v1/users/{userId}/settlements", h.listUserSettlements)
	mux.HandleFunc("PATCH /api/v1/settlements/{id}/status", h.updateStatus)
}

func (h *SettlementHandler) RegisterProtected(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/settlements", authMiddleware(http.HandlerFunc(h.initiateSettlement)))
	mux.Handle("GET /api/v1/settlements/{id}", authMiddleware(http.HandlerFunc(h.getSettlement)))
	mux.Handle("GET /api/v1/users/{userId}/settlements", authMiddleware(http.HandlerFunc(h.listUserSettlements)))
	mux.Handle("PATCH /api/v1/settlements/{id}/status", authMiddleware(http.HandlerFunc(h.updateStatus)))
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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	userID, err := h.resolveInitiatingUserID(r, req.UserID)
	if err != nil {
		if errors.Is(err, errAuthenticatedUserMismatch) {
			writeError(w, http.StatusForbidden, "cannot create settlement for another user")
			return
		}
		writeError(w, http.StatusBadRequest, "user_id must be a valid UUID")
		return
	}

	vaultID, err := uuid.Parse(req.VaultID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "vault_id must be a valid UUID")
		return
	}

	if h.vaultService != nil && h.authenticatedUserPresent(r) {
		vaultModel, err := h.vaultService.GetVault(r.Context(), vaultID)
		if err != nil {
			if errors.Is(err, vault.ErrVaultNotFound) {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if vaultModel.UserID != userID {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
	}

	amount, err := decimal.NewFromString(req.Amount)
	if err != nil {
		writeError(w, http.StatusBadRequest, "amount must be a valid decimal number")
		return
	}

	fiatAmount, err := decimal.NewFromString(req.FiatAmount)
	if err != nil {
		writeError(w, http.StatusBadRequest, "fiat_amount must be a valid decimal number")
		return
	}

	exchangeRate, err := decimal.NewFromString(req.ExchangeRate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "exchange_rate must be a valid decimal number")
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

	writeJSON(w, http.StatusCreated, model)
}

func (h *SettlementHandler) getSettlement(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "settlement id must be a valid UUID")
		return
	}

	model, err := h.service.GetSettlement(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if h.authenticatedUserPresent(r) && !h.isOwner(r, model.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	writeJSON(w, http.StatusOK, model)
}

func (h *SettlementHandler) listUserSettlements(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("userId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "user id must be a valid UUID")
		return
	}

	if h.authenticatedUserPresent(r) && !h.isOwner(r, userID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	statusFilter := r.URL.Query().Get("status")

	models, err := h.service.GetUserSettlements(r.Context(), userID, statusFilter)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, models)
}

func (h *SettlementHandler) updateStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "settlement id must be a valid UUID")
		return
	}

	var req updateStatusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	current, err := h.service.GetSettlement(r.Context(), id)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if h.authenticatedUserPresent(r) && !h.isOwner(r, current.UserID) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	model, err := h.service.UpdateStatus(r.Context(), service.UpdateStatusInput{
		SettlementID: id,
		NewStatus:    offramp.SettlementStatus(req.Status),
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, model)
}

// ── Error mapping ────────────────────────────────────────────────────────────

func (h *SettlementHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, offramp.ErrSettlementNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, offramp.ErrUserNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, offramp.ErrVaultNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, offramp.ErrInvalidSettlement),
		errors.Is(err, offramp.ErrInvalidAmount),
		errors.Is(err, offramp.ErrInvalidStatus),
		errors.Is(err, offramp.ErrInvalidTransition),
		errors.Is(err, offramp.ErrInvalidPrecision):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		logpkg.FromContext(r.Context()).Error("settlement handler failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func (h *SettlementHandler) isOwner(r *http.Request, resourceUserID uuid.UUID) bool {
	authenticatedUserID, err := middleware.AuthenticatedUserID(r.Context())
	if err != nil {
		return false
	}
	return authenticatedUserID == resourceUserID
}

func (h *SettlementHandler) authenticatedUserPresent(r *http.Request) bool {
	_, err := middleware.AuthenticatedUserID(r.Context())
	return err == nil
}

func (h *SettlementHandler) resolveInitiatingUserID(r *http.Request, requestedUserID string) (uuid.UUID, error) {
	if authenticatedUserID, err := middleware.AuthenticatedUserID(r.Context()); err == nil {
		if strings.TrimSpace(requestedUserID) != "" && requestedUserID != authenticatedUserID.String() {
			return uuid.Nil, errAuthenticatedUserMismatch
		}
		return authenticatedUserID, nil
	}

	return uuid.Parse(requestedUserID)
}
