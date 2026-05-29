package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/listquery"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

const maxRequestBodyBytes int64 = 1 << 20

type VaultHandler struct {
	service      *service.VaultService
	sharePriceSvc *service.SharePriceService
}

type createVaultRequest struct {
	ContractAddress string `json:"contract_address"`
	Currency        string `json:"currency"`
	Status          string `json:"status,omitempty"`
}

func NewVaultHandler(svc *service.VaultService, sharePriceSvc *service.SharePriceService) *VaultHandler {
	return &VaultHandler{service: svc, sharePriceSvc: sharePriceSvc}
}

func (h *VaultHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/vaults", h.createVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}", h.getVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}/allocations", h.getAllocations)
	mux.HandleFunc("GET /api/v1/vaults/{id}/share-price", h.getSharePrice)
	mux.HandleFunc("GET /api/v1/vaults/{id}/convert", h.convert)
	mux.HandleFunc("GET /api/v1/vaults", h.listUserVaults)
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

	model, err := h.service.GetVault(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(model))
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
	})
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.PaginatedOK(models, params.Page.Page, params.Page.PerPage, total, ""))
}

func (h *VaultHandler) getAllocations(w http.ResponseWriter, r *http.Request) {
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	v, err := h.service.GetVault(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(v.Allocations))
}

func (h *VaultHandler) getSharePrice(w http.ResponseWriter, r *http.Request) {
	if h.sharePriceSvc == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "share price service not configured"))
		return
	}
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	result, err := h.sharePriceSvc.GetSharePrice(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(result))
}

type convertResponse struct {
	VaultID      uuid.UUID       `json:"vault_id"`
	InputShares  *decimal.Decimal `json:"input_shares,omitempty"`
	InputUSDC    *decimal.Decimal `json:"input_usdc,omitempty"`
	OutputUSDC   *decimal.Decimal `json:"output_usdc,omitempty"`
	OutputShares *decimal.Decimal `json:"output_shares,omitempty"`
	USDCPerShare decimal.Decimal  `json:"usdc_per_share"`
}

func (h *VaultHandler) convert(w http.ResponseWriter, r *http.Request) {
	if h.sharePriceSvc == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, response.Err(http.StatusServiceUnavailable, "UNAVAILABLE", "share price service not configured"))
		return
	}
	vaultID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("vault id must be a valid UUID"))
		return
	}

	sharesParam := r.URL.Query().Get("shares")
	usdcParam := r.URL.Query().Get("usdc")

	if (sharesParam == "") == (usdcParam == "") {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("provide exactly one of: shares or usdc"))
		return
	}

	price, err := h.sharePriceSvc.GetSharePrice(r.Context(), vaultID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	resp := convertResponse{VaultID: vaultID, USDCPerShare: price.USDCPerShare}

	if sharesParam != "" {
		shares, ok := decimal.NewFromString(sharesParam)
		if ok != nil || shares.IsNegative() {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("shares must be a non-negative number"))
			return
		}
		usdc := shares.Mul(price.USDCPerShare).RoundBank(6)
		resp.InputShares = &shares
		resp.OutputUSDC = &usdc
	} else {
		usdc, ok := decimal.NewFromString(usdcParam)
		if ok != nil || usdc.IsNegative() {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("usdc must be a non-negative number"))
			return
		}
		var out decimal.Decimal
		if !price.USDCPerShare.IsZero() {
			out = usdc.Div(price.USDCPerShare).RoundBank(6)
		}
		resp.InputUSDC = &usdc
		resp.OutputShares = &out
	}

	response.WriteJSON(w, http.StatusOK, response.OK(resp))
}

func (h *VaultHandler) writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, vault.ErrVaultNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("vault"))
	case errors.Is(err, vault.ErrUserNotFound):
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("user"))
	case errors.Is(err, vault.ErrInvalidVault), errors.Is(err, vault.ErrInvalidAmount), errors.Is(err, vault.ErrInvalidAllocation):
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
