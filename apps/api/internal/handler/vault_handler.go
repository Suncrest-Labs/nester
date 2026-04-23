package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

const maxRequestBodyBytes int64 = 1 << 20

type VaultHandler struct {
	service *service.VaultService
}

type createVaultRequest struct {
	ContractAddress string `json:"contract_address"`
	Currency        string `json:"currency"`
	Status          string `json:"status,omitempty"`
}

func NewVaultHandler(service *service.VaultService) *VaultHandler {
	return &VaultHandler{service: service}
}

func (h *VaultHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/vaults", h.createVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}", h.getVault)
	mux.HandleFunc("GET /api/v1/vaults/{id}/allocations", h.getAllocations)
	mux.HandleFunc("GET /api/v1/users/{userId}/vaults", h.listUserVaults)
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
	userID, err := uuid.Parse(r.PathValue("userId"))
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

	models, err := h.service.GetUserVaults(r.Context(), userID)
	if err != nil {
		h.writeDomainError(w, r, err)
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(models))
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

// isAlpha returns true if all characters in the string are alphabetic
func isAlpha(s string) bool {
	for _, ch := range s {
		if !(ch >= 'A' && ch <= 'Z') && !(ch >= 'a' && ch <= 'z') {
			return false
		}
	}
	return len(s) > 0
}
