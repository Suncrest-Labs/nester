package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
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
	mux.HandleFunc("GET /api/v1/activity", h.listActivity)
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

	validTypes := map[string]bool{
		string(transaction.TypeDeposit):    true,
		string(transaction.TypeWithdrawal): true,
		string(transaction.TypeSettlement): true,
	}
	if !validTypes[req.Type] {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("type must be one of: deposit, withdrawal, settlement"))
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

func (h *TransactionHandler) listActivity(w http.ResponseWriter, r *http.Request) {
	// Auth guard — require a valid JWT.
	authUser, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	q := r.URL.Query()

	// userId must match the authenticated user (prevent cross-user data access).
	userID := strings.TrimSpace(q.Get("userId"))
	if userID == "" {
		userID = authUser.ID
	}
	if userID != authUser.ID {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return
	}

	filter := transaction.ListFilter{
		UserID: userID,
		Cursor: q.Get("cursor"),
	}

	// Parse limit.
	if lStr := q.Get("limit"); lStr != "" {
		var l int
		if _, err := fmt.Sscanf(lStr, "%d", &l); err == nil {
			filter.Limit = l
		}
	}

	// Parse type (comma-separated).
	if typeStr := q.Get("type"); typeStr != "" {
		for _, t := range strings.Split(typeStr, ",") {
			filter.Types = append(filter.Types, transaction.TransactionType(strings.TrimSpace(t)))
		}
	}

	// Parse status.
	if statusStr := q.Get("status"); statusStr != "" {
		filter.Status = transaction.TransactionStatus(strings.TrimSpace(statusStr))
	}

	// Parse from/to date range.
	if fromStr := q.Get("from"); fromStr != "" {
		t, err := time.Parse(time.RFC3339, fromStr)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("from must be a valid RFC3339 date"))
			return
		}
		filter.From = &t
	}
	if toStr := q.Get("to"); toStr != "" {
		t, err := time.Parse(time.RFC3339, toStr)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("to must be a valid RFC3339 date"))
			return
		}
		filter.To = &t
	}

	// Parse vaultId.
	if vaultIDStr := q.Get("vaultId"); vaultIDStr != "" {
		filter.VaultID = strings.TrimSpace(vaultIDStr)
	}

	// Parse search.
	if searchStr := q.Get("search"); searchStr != "" {
		filter.Search = strings.TrimSpace(searchStr)
	}

	page, err := h.service.ListActivity(r.Context(), filter)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(page))
}
