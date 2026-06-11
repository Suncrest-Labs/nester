package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/bankaccount"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type BankAccountHandler struct {
	service *service.BankAccountService
}

func NewBankAccountHandler(svc *service.BankAccountService) *BankAccountHandler {
	return &BankAccountHandler{service: svc}
}

func (h *BankAccountHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/users/{id}/bank-accounts", h.listAccounts)
	mux.HandleFunc("POST /api/v1/users/{id}/bank-accounts", h.addAccount)
	mux.HandleFunc("PATCH /api/v1/users/{id}/bank-accounts/{acctId}", h.patchAccount)
	mux.HandleFunc("DELETE /api/v1/users/{id}/bank-accounts/{acctId}", h.deleteAccount)
}

type addBankAccountRequest struct {
	BankName      string `json:"bank_name"`
	BankCode      string `json:"bank_code"`
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	Currency      string `json:"currency"`
	Country       string `json:"country"`
	IsDefault     bool   `json:"is_default"`
}

type patchBankAccountRequest struct {
	IsDefault *bool   `json:"is_default"`
	BankName  *string `json:"bank_name"`
}

func (h *BankAccountHandler) listAccounts(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authorizeUserPath(r)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	accounts, err := h.service.List(r.Context(), userID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	if accounts == nil {
		accounts = []bankaccount.PublicView{}
	}
	response.WriteJSON(w, http.StatusOK, response.OK(accounts))
}

func (h *BankAccountHandler) addAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authorizeUserPath(r)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	var req addBankAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	view, err := h.service.Add(r.Context(), userID, bankaccount.AddInput{
		BankName:      req.BankName,
		BankCode:      req.BankCode,
		AccountNumber: req.AccountNumber,
		AccountName:   req.AccountName,
		Currency:      req.Currency,
		Country:       req.Country,
		SetAsDefault:  req.IsDefault,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusCreated, response.Created(view))
}

func (h *BankAccountHandler) patchAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authorizeUserPath(r)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	acctID, err := uuid.Parse(r.PathValue("acctId"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("acctId must be a valid UUID"))
		return
	}

	var req patchBankAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	view, err := h.service.Update(r.Context(), userID, acctID, bankaccount.UpdateInput{
		SetAsDefault: req.IsDefault,
		BankName:     req.BankName,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(view))
}

func (h *BankAccountHandler) deleteAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := h.authorizeUserPath(r)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	acctID, err := uuid.Parse(r.PathValue("acctId"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("acctId must be a valid UUID"))
		return
	}

	if err := h.service.Remove(r.Context(), userID, acctID); err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(map[string]string{"deleted": acctID.String()}))
}

func (h *BankAccountHandler) authorizeUserPath(r *http.Request) (uuid.UUID, error) {
	pathID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, errPathUserInvalid
	}
	caller, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		return uuid.Nil, errUnauthorized
	}
	callerID, err := uuid.Parse(caller.ID)
	if err != nil {
		return uuid.Nil, errUnauthorized
	}
	if callerID != pathID {
		return uuid.Nil, errForbidden
	}
	return pathID, nil
}

var (
	errUnauthorized     = errors.New("unauthorized")
	errForbidden        = errors.New("forbidden")
	errPathUserInvalid  = errors.New("invalid user id")
)

func (h *BankAccountHandler) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnauthorized):
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
	case errors.Is(err, errForbidden):
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "you cannot access another user's bank accounts"))
	case errors.Is(err, errPathUserInvalid):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("user id must be a valid UUID"))
	default:
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	}
}

func (h *BankAccountHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, bankaccount.ErrNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("bank account"))
	case errors.Is(err, bankaccount.ErrForbidden):
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", err.Error()))
	case errors.Is(err, bankaccount.ErrDuplicateAccount):
		response.WriteJSON(w, http.StatusConflict, response.Err(http.StatusConflict, "DUPLICATE_BANK_ACCOUNT", err.Error()))
	case errors.Is(err, bankaccount.ErrInvalidInput):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, bankaccount.ErrEncryptionUnavailable):
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "ENCRYPTION_UNAVAILABLE", err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("bank account handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
