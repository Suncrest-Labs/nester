package handler

import (
	"errors"
	"net/http"
	"strconv"
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
	mux.HandleFunc("GET /api/v1/transactions", h.listTransactions)
	mux.HandleFunc("POST /api/v1/transactions", h.createTransaction)
	mux.HandleFunc("GET /api/v1/transactions/{hash}", h.getTransactionByHash)
}

// transactionView is the public representation of a transaction.
// "completed" status is surfaced as "confirmed", and "currency" as "asset".
type transactionView struct {
	ID          string  `json:"id"`
	VaultID     string  `json:"vault_id"`
	Type        string  `json:"type"`
	Amount      string  `json:"amount"`
	Asset       string  `json:"asset"`
	Status      string  `json:"status"`
	TxHash      string  `json:"tx_hash,omitempty"`
	CreatedAt   string  `json:"created_at"`
	ConfirmedAt *string `json:"confirmed_at,omitempty"`
}

type listTransactionsData struct {
	Data       []transactionView    `json:"data"`
	Pagination transactionPagination `json:"pagination"`
}

type transactionPagination struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func toTransactionView(t transaction.Transaction) transactionView {
	status := string(t.Status)
	if status == string(transaction.StatusCompleted) {
		status = "confirmed"
	}
	v := transactionView{
		ID:        t.ID.String(),
		VaultID:   t.VaultID.String(),
		Type:      string(t.Type),
		Amount:    t.Amount.StringFixed(6),
		Asset:     t.Currency,
		Status:    status,
		TxHash:    t.TxHash,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
	}
	if t.ConfirmedAt != nil {
		s := t.ConfirmedAt.UTC().Format(time.RFC3339)
		v.ConfirmedAt = &s
	}
	return v
}

func (h *TransactionHandler) listTransactions(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid token subject"))
		return
	}

	q := r.URL.Query()

	var vaultID uuid.UUID
	if raw := q.Get("vault_id"); raw != "" {
		vaultID, err = uuid.Parse(raw)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault_id must be a valid UUID"))
			return
		}
	}

	txType := q.Get("type")
	if txType != "" && txType != "deposit" && txType != "withdrawal" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("type must be deposit or withdrawal"))
		return
	}

	txStatus := q.Get("status")
	if txStatus != "" && txStatus != "pending" && txStatus != "confirmed" && txStatus != "failed" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("status must be pending, confirmed, or failed"))
		return
	}

	limit := 20
	if raw := q.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("limit must be a positive integer"))
			return
		}
	}

	offset := 0
	if raw := q.Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("offset must be a non-negative integer"))
			return
		}
	}

	txns, total, err := h.service.ListUserTransactions(r.Context(), service.ListUserTransactionsInput{
		UserID:  userID,
		VaultID: vaultID,
		Type:    txType,
		Status:  txStatus,
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		logpkg.FromContext(r.Context()).Error("list transactions failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
		return
	}

	views := make([]transactionView, len(txns))
	for i, t := range txns {
		views[i] = toTransactionView(t)
	}

	response.WriteJSON(w, http.StatusOK, response.OK(listTransactionsData{
		Data: views,
		Pagination: transactionPagination{
			Total:  total,
			Limit:  limit,
			Offset: offset,
		},
	}))
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
