package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type contextKey string

const (
	authUserIDContextKey        contextKey = "auth.user_id"
	authWalletAddressContextKey contextKey = "auth.wallet_address"
)

type AccessTokenAuthenticator interface {
	AuthenticateAccessToken(token string) (service.AuthPrincipal, error)
}

func AuthMiddleware(authenticator AccessTokenAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeUnauthorized(w)
				return
			}

			principal, err := authenticator.AuthenticateAccessToken(token)
			if err != nil {
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), authUserIDContextKey, principal.UserID)
			ctx = context.WithValue(ctx, authWalletAddressContextKey, principal.WalletAddress)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AuthenticatedUserID(ctx context.Context) (uuid.UUID, error) {
	value := ctx.Value(authUserIDContextKey)
	userID, ok := value.(uuid.UUID)
	if !ok || userID == uuid.Nil {
		return uuid.Nil, errors.New("authenticated user missing from context")
	}
	return userID, nil
}

func AuthenticatedWalletAddress(ctx context.Context) (string, error) {
	value := ctx.Value(authWalletAddressContextKey)
	walletAddress, ok := value.(string)
	if !ok || strings.TrimSpace(walletAddress) == "" {
		return "", errors.New("authenticated wallet missing from context")
	}
	return walletAddress, nil
}

func bearerToken(headerValue string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(headerValue, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(headerValue, prefix))
	return token, token != ""
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"authentication required"}`))
}
