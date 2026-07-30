package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/internal/ws"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

const maxRequestBodyBytes int64 = 1 << 20

type VaultHandler struct {
	service            *service.VaultService
	rebalanceSvc       *service.VaultRebalanceService
	wsHub              *ws.Hub
	rebalanceRateLimiter func(http.Handler) http.Handler
}

type createVaultRequest struct {
	ContractAddress string `json:"contract_address"`
	Currency        string `json:"currency"`
	Status          string `json:"status,omitempty"`
}

type depositRequest struct {
	Amount string `json:"amount"`
	Asset  string `json:"asset"`
}

type withdrawRequest struct {
	Amount string `json:"amount"`
	Asset  string `json:"asset"`
}

type rebalanceRequest struct {
	VaultID      string `json:"vault_id"`
	FromProtocol string `json:"from_protocol"`
	ToProtocol   string `json:"to_protocol"`
	Amount       string `json:"amount"`
	Currency     string `json:"currency"`
}

func NewVaultHandler(service *service.VaultService) *VaultHandler {
	return &VaultHandler{service: service}
}

// SetWSHub wires the websocket hub used to broadcast harvest events.
func (h *VaultHandler) SetWSHub(hub *ws.Hub) {
	h.wsHub = hub
}

// SetRebalanceService wires user-facing rebalance suggestion and execution.
func (h *VaultHandler) SetRebalanceService(svc *service.VaultRebalanceService) {
	h.rebalanceSvc = svc
}

// SetRebalanceRateLimiter wires the rate limiter for rebalance endpoints.
func (h *VaultHandler) SetRebalanceRateLimiter(rl func(http.Handler) http.Handler) {
	h.rebalanceRateLimiter = rl
}

func (h *VaultHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/vaults", h.createVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}", h.getVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}/allocations", h.getAllocations)
	mux.HandleFunc("POST /api/v1/vaults/{id}/harvest", h.harvestVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}/harvest/preview", h.previewHarvest)
	mux.HandleFunc("PATCH /api/v1/vaults/{id}/harvest-frequency", h.updateHarvestFrequency)
	mux.HandleFunc("GET /api/v1/vaults/{id}/my-position", h.getMyPosition)
	// GET /api/v1/vaults/{id}/projection is registered by ProjectionHandler
	mux.HandleFunc("GET /api/v1/vaults/{id}/preview-deposit", h.previewDeposit)
	mux.HandleFunc("GET /api/v1/vaults/{id}/preview-withdraw", h.previewWithdraw)
	mux.HandleFunc("GET /api/v1/vaults", h.listUserVaults)
	mux.HandleFunc("GET /api/v1/vaults/all", h.listVaults)
	mux.HandleFunc("POST /api/v1/vaults/{id}/deposit", h.depositToVault)
	mux.HandleFunc("POST /api/v1/vaults/{id}/withdraw", h.withdrawFromVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}/rebalance-suggestion", h.getRebalanceSuggestion)
	mux.HandleFunc("POST /api/v1/vaults/{id}/rebalance", h.rebalanceVault)
	var rebalanceHandler http.Handler = http.HandlerFunc(h.rebalancePosition)
	if h.rebalanceRateLimiter != nil {
		rebalanceHandler = h.rebalanceRateLimiter(rebalanceHandler)
	}
	mux.Handle("POST /api/v1/vault/rebalance", rebalanceHandler)
	mux.HandleFunc("POST /api/v1/vaults/{id}/emergency-withdraw", h.emergencyWithdraw)
	mux.HandleFunc("GET /api/v1/vaults/{id}/share-price", h.getSharePrice)
	mux.HandleFunc("GET /api/v1/vaults/{id}/convert", h.convert)
}

type harvestVaultRequest struct {
	Compound *bool `json:"compound"`
}

type updateHarvestFrequencyRequest struct {
	HarvestFrequency string `json:"harvest_frequency"`
}

func (h *VaultHandler) createVault(w http.ResponseWriter, r *http.Request) {
	var request createVaultRequest
	if err := decodeJSON(r, &request); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

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

	if err := validateCurrencyCode(request.Currency); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid currency: "+err.Error()))
		return
	}

	request.ContractAddress = strings.TrimSpace(request.ContractAddress)
	if !isValidSorobanContractAddress(request.ContractAddress) {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("contract_address must be a 56-character Soroban address starting with 'C'"))
		return
	}

	model, err := h.service.CreateVault(r.Context(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: request.ContractAddress,
		Currency:        request.Currency,
		Status:          request.Status,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusCreated, response.Created(model))
}

func (h *VaultHandler) getVault(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	model, err := h.service.GetVault(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if model.UserID.String() != user.ID {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(model))
}

func (h *VaultHandler) listVaults(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := 20
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("limit must be a positive integer"))
			return
		}
		if v > 100 {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("limit must not exceed 100"))
			return
		}
		limit = v
	}

	offset := 0
	if raw := q.Get("offset"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("offset must be a non-negative integer"))
			return
		}
		offset = v
	}

	vaults, total, err := h.service.ListVaults(r.Context(), service.ListVaultsInput{
		Limit:  limit,
		Offset: offset,
		Status: q.Get("status"),
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	out := response.OK(vaults)
	out.Meta = &response.Meta{
		Page:       offset/limit + 1,
		PerPage:    limit,
		TotalCount: total,
	}
	response.WriteJSON(w, http.StatusOK, out)
}

func (h *VaultHandler) listUserVaults(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.URL.Query().Get("userId"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("user id must be a valid UUID"))
		return
	}

	authUser, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	if authUser.ID != userID.String() {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return
	}

	params, err := listquery.ParseVaultList(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	models, total, err := h.service.ListUserVaults(r.Context(), userID, vault.UserListFilter{
		Page:         params.Page.Page,
		PerPage:      params.Page.PerPage,
		SortField:    params.Sort.Field,
		SortOrder:    params.Sort.Order,
		Status:       params.Status,
		Currency:     params.Currency,
		MinBalance:   params.MinBalance,
		CreatedAfter: params.CreatedAfter,
		Search:       params.Search,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.PaginatedOK(models, params.Page.Page, params.Page.PerPage, total, ""))
}

func (h *VaultHandler) harvestVault(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	var req harvestVaultRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	authUser, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	userID, err := uuid.Parse(authUser.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid token subject"))
		return
	}

	result, err := h.service.HarvestVault(r.Context(), service.HarvestVaultInput{
		VaultID:       vaultID,
		UserID:        userID,
		WalletAddress: authUser.WalletAddress,
		Compound:      req.Compound,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if h.wsHub != nil {
		h.wsHub.BroadcastEvent(ws.Event{
			Channel:   "vault:" + vaultID.String(),
			Type:      ws.EventHarvestCompleted,
			Data:      result,
			Timestamp: time.Now().UTC(),
		})
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

// updateHarvestFrequency sets how often the harvest engine considers this
// vault for a harvest ("daily" or "weekly"). Only the vault owner may change
// it (#940).
func (h *VaultHandler) updateHarvestFrequency(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	var req updateHarvestFrequencyRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	userID, err := h.authenticatedUserID(w, r)
	if err != nil {
		return
	}

	result, err := h.service.UpdateHarvestFrequency(r.Context(), vaultID, userID, req.HarvestFrequency)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *VaultHandler) previewHarvest(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	userID, err := h.authenticatedUserID(w, r)
	if err != nil {
		return
	}

	compound := r.URL.Query().Get("compound") == "true"

	result, err := h.service.PreviewHarvest(r.Context(), service.PreviewHarvestInput{
		VaultID:  vaultID,
		UserID:   userID,
		Compound: compound,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *VaultHandler) getAllocations(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	v, err := h.service.GetVault(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if v.UserID.String() != user.ID {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(v.Allocations))
}

func (h *VaultHandler) getRebalanceSuggestion(w http.ResponseWriter, r *http.Request) {
	if h.rebalanceSvc == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "rebalance service not configured"))
		return
	}
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}
	userID, err := h.authenticatedUserID(w, r)
	if err != nil {
		return
	}
	suggestion, err := h.rebalanceSvc.GetSuggestion(r.Context(), vaultID, userID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(suggestion))
}

type rebalanceVaultRequest struct {
	Allocations []service.AllocationPct `json:"allocations"`
}

func (h *VaultHandler) rebalanceVault(w http.ResponseWriter, r *http.Request) {
	if h.rebalanceSvc == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "rebalance service not configured"))
		return
	}
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}
	userID, err := h.authenticatedUserID(w, r)
	if err != nil {
		return
	}
	var req rebalanceVaultRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("request body must be valid JSON"))
			return
		}
	}
	if len(req.Allocations) > 0 {
		if err := service.ValidateRebalanceAllocations(req.Allocations); err != nil {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
	}
	result, err := h.rebalanceSvc.TriggerRebalance(r.Context(), vaultID, userID, req.Allocations)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

// emergencyWithdraw triggers an on-chain emergency exit from all of the vault's
// active positions. Only the vault owner may invoke it.
func (h *VaultHandler) emergencyWithdraw(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	authUser, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	existing, err := h.service.GetVault(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if existing.UserID.String() != authUser.ID {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return
	}

	result, err := h.service.EmergencyWithdraw(r.Context(), service.EmergencyWithdrawInput{VaultID: vaultID})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

func (h *VaultHandler) rebalancePosition(w http.ResponseWriter, r *http.Request) {
	authUser, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	var req rebalanceRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	userID, err := uuid.Parse(authUser.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid token subject"))
		return
	}

	vaultID, err := uuid.Parse(req.VaultID)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault_id must be a valid UUID"))
		return
	}

	amount, err := stringToDecimal(req.Amount)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid amount: must be a valid decimal number"))
		return
	}

	if err := validateCurrencyCode(req.Currency); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid currency: " + err.Error()))
		return
	}

	result, err := h.service.RebalancePosition(r.Context(), service.RebalancePositionInput{
		VaultID:     vaultID,
		UserID:      userID,
		FromProtocol: req.FromProtocol,
		ToProtocol:   req.ToProtocol,
		Amount:      amount,
		Currency:    req.Currency,
		TxHash:      "",
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	type rebalanceResponse struct {
		Vault              vault.Vault          `json:"vault"`
		FromProtocolBalance decimal.Decimal      `json:"from_protocol_balance"`
		ToProtocolBalance   decimal.Decimal      `json:"to_protocol_balance"`
	}

	response.WriteJSON(w, http.StatusOK, response.OK(rebalanceResponse{
		Vault:              result.Vault,
		FromProtocolBalance: result.FromProtocolBalance,
		ToProtocolBalance:   result.ToProtocolBalance,
	}))
}

func (h *VaultHandler) authenticatedUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, error) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return uuid.Nil, errors.New("unauthorized")
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "invalid token subject"))
		return uuid.Nil, err
	}
	return userID, nil
}

func (h *VaultHandler) getMyPosition(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

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

	position, err := h.service.GetMyPosition(r.Context(), userID, vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(position))
}

func (h *VaultHandler) depositToVault(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	var request depositRequest
	if err := decodeJSON(r, &request); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	amount, err := stringToDecimal(request.Amount)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid amount: must be a valid decimal number"))
		return
	}

	if amount.IsNegative() || amount.IsZero() {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("amount must be greater than zero"))
		return
	}

	if err := validateCurrencyCode(request.Asset); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid asset: "+err.Error()))
		return
	}

	vaultModel, err := h.service.GetVault(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if vaultModel.UserID.String() != user.ID {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return
	}

	updatedVault, err := h.service.RecordDeposit(r.Context(), service.RecordDepositInput{
		VaultID: vaultID,
		Amount:  amount,
		TxHash:  "",
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusCreated, response.Created(updatedVault))
}

func (h *VaultHandler) withdrawFromVault(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	var request withdrawRequest
	if err := decodeJSON(r, &request); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
		return
	}

	// Parse amount string to decimal
	amount, err := stringToDecimal(request.Amount)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid amount: must be a valid decimal number"))
		return
	}

	// Validate amount is positive
	if amount.IsNegative() || amount.IsZero() {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("amount must be greater than zero"))
		return
	}

	// Validate asset code
	if err := validateCurrencyCode(request.Asset); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid asset: "+err.Error()))
		return
	}

	// Verify vault ownership
	vault, err := h.service.GetVault(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	if vault.UserID.String() != user.ID {
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
		return
	}

	// Record withdrawal
	updatedVault, err := h.service.RecordWithdrawal(r.Context(), service.RecordWithdrawalInput{
		VaultID: vaultID,
		Amount:  amount,
		TxHash:  "", // TxHash would be set by the on-chain invoker or blockchain confirmation listener
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(updatedVault))
}

func (h *VaultHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, vault.ErrVaultNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("vault"))
	case errors.Is(err, vault.ErrVaultForbidden):
		response.WriteJSON(w, http.StatusForbidden, response.Err(http.StatusForbidden, "FORBIDDEN", "forbidden"))
	case errors.Is(err, vault.ErrUserNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("user"))
	case errors.Is(err, vault.ErrInvalidVault), errors.Is(err, vault.ErrInvalidAmount), errors.Is(err, vault.ErrInvalidAllocation), errors.Is(err, vault.ErrInvalidHarvestFrequency):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	case errors.Is(err, vault.ErrBelowMinDeposit):
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
	default:
		logpkg.FromContext(r.Context()).Error("vault handler failed", "error", err.Error())
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
	}
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain only one JSON object")
	}

	return nil
}

// validateCurrencyCode verifies the currency code is valid (ISO 4217 or crypto token format)
func validateCurrencyCode(code string) error {
	code = strings.TrimSpace(code)
	if len(code) < 3 || len(code) > 4 {
		return errors.New("currency code must be 3-4 characters (e.g., USD, USDC)")
	}
	if !isAlpha(code) {
		return errors.New("currency code must contain only letters")
	}
	return nil
}

// isValidSorobanContractAddress validates a Stellar Soroban contract address:
// 56 characters long, starts with 'C', uppercase base32 alphanumeric.
func isValidSorobanContractAddress(addr string) bool {
	if len(addr) != 56 || addr[0] != 'C' {
		return false
	}
	for _, ch := range addr {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= '2' && ch <= '7')) {
			return false
		}
	}
	return true
}

// isAlpha returns true if all characters in the string are alphabetic
func isAlpha(s string) bool {
	for _, ch := range s {
		if !(ch >= 'A' && ch <= 'Z') && !(ch >= 'a' && ch <= 'z') {
			return false
		}
	}
	return len(s) > 0
}

// stringToDecimal converts a string to a decimal.Decimal value
func stringToDecimal(s string) (decimal.Decimal, error) {
	s = strings.TrimSpace(s)
	return decimal.NewFromString(s)
}

func (h *VaultHandler) previewDeposit(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.GetUserFromContext(r.Context()); !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	amountStr := r.URL.Query().Get("amount")
	if amountStr == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("amount is required"))
		return
	}

	amount, err := decimal.NewFromString(amountStr)
	if err != nil || amount.Cmp(decimal.Zero) <= 0 {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("amount must be a positive number"))
		return
	}

	out, err := h.service.PreviewDeposit(r.Context(), service.PreviewDepositInput{
		VaultID: vaultID,
		Amount:  amount,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func (h *VaultHandler) previewWithdraw(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.GetUserFromContext(r.Context()); !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}

	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	sharesStr := r.URL.Query().Get("shares")
	if sharesStr == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("shares is required"))
		return
	}

	shares, err := decimal.NewFromString(sharesStr)
	if err != nil || shares.Cmp(decimal.Zero) <= 0 {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("shares must be a positive number"))
		return
	}

	out, err := h.service.PreviewWithdraw(r.Context(), service.PreviewWithdrawInput{
		VaultID: vaultID,
		Shares:  shares,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(out))
}

func (h *VaultHandler) getSharePrice(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	sharePrice, err := h.service.GetSharePrice(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(sharePrice))
}

func (h *VaultHandler) convert(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	sharesParam := r.URL.Query().Get("shares")
	usdcParam := r.URL.Query().Get("usdc")

	if sharesParam != "" && usdcParam != "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("must provide either shares or usdc, not both"))
		return
	}
	if sharesParam == "" && usdcParam == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("must provide either shares or usdc"))
		return
	}

	resp, err := h.service.Convert(r.Context(), vaultID, service.ConvertRequest{
		Shares: sharesParam,
		USDC:   usdcParam,
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(resp))
}
