package handler

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type TransactionHandler struct {
	service *service.TransactionService
}

func NewTransactionHandler(service *service.TransactionService) *TransactionHandler {
	return &TransactionHandler{service: service}
}

func (h *TransactionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/transactions", h.createTransaction)
	mux.HandleFunc("GET /api/v1/transactions/{hash}", h.getTransactionByHash)
}

type createTransactionRequest struct {
	VaultID  string `json:"vault_id"`
	Type     string `json:"type"`
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	TxHash   string `json:"tx_hash"`
}

func (h *TransactionHandler) createTransaction(w http.ResponseWriter, r *http.Request) {
	var req createTransactionRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
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

	model, err := h.service.RegisterTransaction(r.Context(), service.RegisterTransactionInput{
		VaultID:  vaultID,
		Type:     transaction.TransactionType(req.Type),
		Amount:   amount,
		Currency: req.Currency,
		TxHash:   req.TxHash,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusCreated, response.Created(model))
}

func (h *TransactionHandler) getTransactionByHash(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if hash == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("transaction hash is required"))
		return
	}

	model, err := h.service.GetTransaction(r.Context(), hash)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *TransactionHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, transaction.ErrTransactionNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("transaction"))
	case errors.Is(err, transaction.ErrInvalidTransaction),
		errors.Is(err, transaction.ErrInvalidStatus),
		errors.Is(err, transaction.ErrInvalidType):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("transaction handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}
