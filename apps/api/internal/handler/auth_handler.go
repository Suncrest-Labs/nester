package handler

import (
	"errors"
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type AuthHandler struct {
	service *service.AuthService
}

type nonceRequest struct {
	WalletAddress string `json:"wallet_address"`
}

type nonceResponse struct {
	Nonce     string `json:"nonce"`
	ExpiresAt string `json:"expires_at"`
}

type verifyRequest struct {
	WalletAddress string `json:"wallet_address"`
	Nonce         string `json:"nonce"`
	Message       string `json:"message"`
	Signature     string `json:"signature"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{service: svc}
}

func (h *AuthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/nonce", h.issueNonce)
	mux.HandleFunc("POST /api/v1/auth/verify", h.verify)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.logout)
}

func (h *AuthHandler) issueNonce(w http.ResponseWriter, r *http.Request) {
	var req nonceRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	nonce, expiresAt, err := h.service.IssueNonce(r.Context(), req.WalletAddress)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, nonceResponse{
		Nonce:     nonce,
		ExpiresAt: expiresAt.Format(http.TimeFormat),
	})
}

func (h *AuthHandler) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tokens, err := h.service.VerifyWallet(r.Context(), service.VerifyWalletInput{
		WalletAddress: req.WalletAddress,
		Nonce:         req.Nonce,
		Message:       req.Message,
		Signature:     req.Signature,
	})
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

func (h *AuthHandler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tokens, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		h.writeAuthError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.Logout(r.Context(), req.RefreshToken); err != nil {
		h.writeAuthError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) writeAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidWalletAddress),
		errors.Is(err, service.ErrInvalidSignature):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, service.ErrInvalidNonce),
		errors.Is(err, service.ErrReplayAttack),
		errors.Is(err, service.ErrInvalidToken),
		errors.Is(err, service.ErrExpiredToken),
		errors.Is(err, service.ErrSessionNotFound):
		writeError(w, http.StatusUnauthorized, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
