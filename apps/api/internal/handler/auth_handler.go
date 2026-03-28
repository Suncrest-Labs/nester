package handler

import (
	"errors"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type AuthHandler struct {
	authService service.AuthService
}

func NewAuthHandler(authService service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/challenge", h.handleChallenge)
	mux.HandleFunc("POST /api/v1/auth/verify", h.handleVerify)
}

type ChallengeRequest struct {
	WalletAddress string `json:"wallet_address"`
}

type ChallengeResponse struct {
	Challenge string `json:"challenge"`
}

func (h *AuthHandler) handleChallenge(w http.ResponseWriter, r *http.Request) {
	var req ChallengeRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}

	if req.WalletAddress == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("wallet_address is required"))
		return
	}

	challenge, err := h.authService.GenerateChallenge(r.Context(), req.WalletAddress)
	if err != nil {
		if errors.Is(err, service.ErrWalletInvalid) {
			response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr(err.Error()))
			return
		}
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to generate challenge"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(ChallengeResponse{Challenge: challenge}))
}

type VerifyRequest struct {
	WalletAddress string `json:"wallet_address"`
	Signature     string `json:"signature"`
	Challenge     string `json:"challenge"`
}

type VerifyResponse struct {
	Token string `json:"token"`
}

func (h *AuthHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req VerifyRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}

	if req.WalletAddress == "" || req.Signature == "" || req.Challenge == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("wallet_address, signature, and challenge are required"))
		return
	}

	token, err := h.authService.VerifyAndIssue(r.Context(), req.WalletAddress, req.Signature, req.Challenge)
	if err != nil {
		if errors.Is(err, service.ErrChallengeExpired) || errors.Is(err, service.ErrSignatureInvalid) || errors.Is(err, service.ErrWalletInvalid) {
			response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", err.Error()))
			return
		}
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "authentication failed"))
		return
	}

	response.WriteJSON(w, http.StatusOK, response.OK(VerifyResponse{Token: token}))
}
