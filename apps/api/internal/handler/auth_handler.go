package handler

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/session"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/usersignal"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

// refreshCookieName holds the refresh token as an httpOnly cookie so it's
// never readable by client-side JS (an XSS on the frontend can't exfiltrate
// it). Scoped to the auth path only — no reason for it to travel on every
// API request.
const refreshCookieName = "nester_refresh_token"
const refreshCookiePath = "/api/v1/auth"

// ActivityRecorder is the narrow seam handleVerify needs to log a login
// activity event; postgres.ActivityEventRepository satisfies it without
// this package importing the postgres layer directly.
type ActivityRecorder interface {
	RecordEvent(ctx context.Context, userID uuid.UUID, eventType usersignal.EventType, occurredAt time.Time) error
}

type AuthHandler struct {
	authService service.AuthService
	// secureCookies gates the Secure flag (and SameSite=None, which requires
	// it) on the refresh cookie. False only in local development, where the
	// frontend commonly talks to the API over plain HTTP.
	secureCookies bool
	userSvc       *service.UserService
	outcomeSvc    *service.NudgeOutcomeService
	activityRepo  ActivityRecorder
}

func NewAuthHandler(authService service.AuthService, secureCookies bool, userSvc *service.UserService, outcomeSvc *service.NudgeOutcomeService, activityRepo ActivityRecorder) *AuthHandler {
	return &AuthHandler{
		authService:   authService,
		secureCookies: secureCookies,
		userSvc:       userSvc,
		outcomeSvc:    outcomeSvc,
		activityRepo:  activityRepo,
	}
}

func (h *AuthHandler) setRefreshCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: h.cookieSameSite(),
	})
}

func (h *AuthHandler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: h.cookieSameSite(),
	})
}

// cookieSameSite is None (cross-origin, requires Secure) once secureCookies
// is on, else Lax for same-origin local dev over plain HTTP.
func (h *AuthHandler) cookieSameSite() http.SameSite {
	if h.secureCookies {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

func (h *AuthHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/challenge", h.handleChallenge)
	mux.HandleFunc("POST /api/v1/auth/verify", h.handleVerify)
	mux.HandleFunc("POST /api/v1/auth/refresh", h.handleRefresh)
	mux.HandleFunc("POST /api/v1/auth/logout", h.handleLogout)
	mux.HandleFunc("POST /api/v1/auth/logout-all", h.handleLogoutAll)
	mux.HandleFunc("GET /api/v1/auth/sessions", h.handleListSessions)
	mux.HandleFunc("DELETE /api/v1/auth/sessions/{id}", h.handleRevokeSession)
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
	WalletAddress     string `json:"wallet_address"`
	Signature         string `json:"signature"`
	Challenge         string `json:"challenge"`
	DeviceFingerprint string `json:"device_fingerprint"`
	Timezone          string `json:"timezone"`
}

// TokenResponse is the shared shape returned by /auth/verify and /auth/refresh.
// The refresh token itself is never included here — it's set as an httpOnly
// cookie (see setRefreshCookie) so client-side JS can't read or exfiltrate it.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func tokenResponse(t service.Tokens) TokenResponse {
	return TokenResponse{
		AccessToken: t.AccessToken,
		ExpiresIn:   t.ExpiresIn,
		TokenType:   "Bearer",
	}
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
	if req.DeviceFingerprint == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("device_fingerprint is required"))
		return
	}

	tokens, err := h.authService.VerifyAndIssue(r.Context(), req.WalletAddress, req.Signature, req.Challenge, clientMetadata(r, req.DeviceFingerprint))
	if err != nil {
		if errors.Is(err, service.ErrChallengeExpired) || errors.Is(err, service.ErrSignatureInvalid) || errors.Is(err, service.ErrWalletInvalid) {
			response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", err.Error()))
			return
		}
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "authentication failed"))
		return
	}

	if req.Timezone != "" {
		// Best-effort: never block login on a timezone-capture failure.
		tz := req.Timezone
		_, _ = h.userSvc.UpdateProfile(r.Context(), tokens.UserID, service.UpdateProfileInput{Timezone: &tz})
	}
	if h.outcomeSvc != nil {
		_ = h.outcomeSvc.RecordReturnVisit(r.Context(), tokens.UserID, time.Now())
	}
	if h.activityRepo != nil {
		_ = h.activityRepo.RecordEvent(r.Context(), tokens.UserID, usersignal.EventTypeLogin, time.Now())
	}

	h.setRefreshCookie(w, tokens.RefreshToken, time.Duration(tokens.RefreshExpiresIn)*time.Second)
	response.WriteJSON(w, http.StatusOK, response.OK(tokenResponse(tokens)))
}

type RefreshRequest struct {
	DeviceFingerprint string `json:"device_fingerprint"`
}

func (h *AuthHandler) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := decodeJSON(r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("invalid request body"))
		return
	}
	if req.DeviceFingerprint == "" {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("device_fingerprint is required"))
		return
	}

	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "refresh token is invalid or expired, please sign in again"))
		return
	}

	tokens, err := h.authService.Refresh(r.Context(), cookie.Value, clientMetadata(r, req.DeviceFingerprint))
	if err != nil {
		// Every failure mode (invalid, expired, reused, device mismatch,
		// session revoked/expired) collapses to the same response — the
		// distinction must not leak to the caller, only to the audit log.
		// Clear the dead cookie so the browser stops resending it.
		h.clearRefreshCookie(w)
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "refresh token is invalid or expired, please sign in again"))
		return
	}

	h.setRefreshCookie(w, tokens.RefreshToken, time.Duration(tokens.RefreshExpiresIn)*time.Second)
	response.WriteJSON(w, http.StatusOK, response.OK(tokenResponse(tokens)))
}

func (h *AuthHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	userID, sessionID, ok := authenticatedSession(w, r)
	if !ok {
		return
	}
	if err := h.authService.Logout(r.Context(), userID, sessionID); err != nil {
		h.writeSessionError(w, err)
		return
	}
	h.clearRefreshCookie(w)
	response.WriteJSON(w, http.StatusOK, response.OK(struct{}{}))
}

type LogoutAllResponse struct {
	RevokedCount int `json:"revoked_count"`
}

func (h *AuthHandler) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "invalid user id"))
		return
	}

	count, err := h.authService.LogoutAll(r.Context(), userID)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to sign out of all sessions"))
		return
	}

	h.clearRefreshCookie(w)
	response.WriteJSON(w, http.StatusOK, response.OK(LogoutAllResponse{RevokedCount: count}))
}

type SessionView struct {
	ID                string  `json:"id"`
	DeviceFingerprint string  `json:"device_fingerprint"`
	UserAgent         *string `json:"user_agent,omitempty"`
	IPAddress         *string `json:"ip_address,omitempty"`
	CreatedAt         string  `json:"created_at"`
	LastActiveAt      string  `json:"last_active_at"`
	AbsoluteExpiresAt string  `json:"absolute_expires_at"`
	IsCurrent         bool    `json:"is_current"`
}

type ListSessionsResponse struct {
	Sessions []SessionView `json:"sessions"`
}

func (h *AuthHandler) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUserFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "invalid user id"))
		return
	}

	sessions, err := h.authService.ListSessions(r.Context(), userID)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "failed to list sessions"))
		return
	}

	views := make([]SessionView, len(sessions))
	for i, s := range sessions {
		views[i] = SessionView{
			ID:                s.ID.String(),
			DeviceFingerprint: s.DeviceFingerprint,
			UserAgent:         s.UserAgent,
			IPAddress:         s.IPAddress,
			CreatedAt:         s.CreatedAt.Format(timeLayout),
			LastActiveAt:      s.LastActiveAt.Format(timeLayout),
			AbsoluteExpiresAt: s.AbsoluteExpiresAt.Format(timeLayout),
			IsCurrent:         s.ID.String() == user.SessionID,
		}
	}

	response.WriteJSON(w, http.StatusOK, response.OK(ListSessionsResponse{Sessions: views}))
}

func (h *AuthHandler) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	userID, _, ok := authenticatedSession(w, r)
	if !ok {
		return
	}
	sessionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, response.ValidationErr("session id must be a valid UUID"))
		return
	}

	if err := h.authService.RevokeSession(r.Context(), userID, sessionID); err != nil {
		h.writeSessionError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, response.OK(struct{}{}))
}

func (h *AuthHandler) writeSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrSessionNotFound) {
		response.WriteJSON(w, http.StatusNotFound, response.NotFound("session"))
		return
	}
	response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"))
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

// authenticatedSession extracts the caller's user ID and current session ID
// from the request context, writing an error response and returning ok=false
// if either is missing/invalid.
func authenticatedSession(w http.ResponseWriter, r *http.Request) (userID, sessionID uuid.UUID, ok bool) {
	user, present := auth.GetUserFromContext(r.Context())
	if !present {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return uuid.UUID{}, uuid.UUID{}, false
	}
	userID, err := uuid.Parse(user.ID)
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, response.Err(http.StatusInternalServerError, "INTERNAL_ERROR", "invalid user id"))
		return uuid.UUID{}, uuid.UUID{}, false
	}
	sessionID, err = uuid.Parse(user.SessionID)
	if err != nil {
		response.WriteJSON(w, http.StatusUnauthorized, response.Err(http.StatusUnauthorized, "UNAUTHORIZED", "request was not authenticated with a session"))
		return uuid.UUID{}, uuid.UUID{}, false
	}
	return userID, sessionID, true
}

// clientMetadata builds SessionMetadata from request headers/remote address.
func clientMetadata(r *http.Request, deviceFingerprint string) service.SessionMetadata {
	return service.SessionMetadata{
		DeviceFingerprint: deviceFingerprint,
		UserAgent:         r.Header.Get("User-Agent"),
		IPAddress:         clientIP(r),
	}
}

func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
