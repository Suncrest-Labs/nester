package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type stubAuthenticator struct {
	principal service.AuthPrincipal
	err       error
}

func (s stubAuthenticator) AuthenticateAccessToken(_ string) (service.AuthPrincipal, error) {
	if s.err != nil {
		return service.AuthPrincipal{}, s.err
	}
	return s.principal, nil
}

func TestAuthMiddlewareRejectsMissingToken(t *testing.T) {
	handler := AuthMiddleware(stubAuthenticator{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestAuthMiddlewareInjectsPrincipal(t *testing.T) {
	userID := uuid.New()
	handler := AuthMiddleware(stubAuthenticator{
		principal: service.AuthPrincipal{
			UserID:        userID,
			WalletAddress: "GTESTWALLET",
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID, err := AuthenticatedUserID(r.Context())
		if err != nil {
			t.Fatalf("AuthenticatedUserID() error = %v", err)
		}
		gotWallet, err := AuthenticatedWalletAddress(r.Context())
		if err != nil {
			t.Fatalf("AuthenticatedWalletAddress() error = %v", err)
		}
		if gotUserID != userID {
			t.Fatalf("expected user %s, got %s", userID, gotUserID)
		}
		if gotWallet != "GTESTWALLET" {
			t.Fatalf("expected wallet injected")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
}

func TestAuthMiddlewareRejectsInvalidToken(t *testing.T) {
	handler := AuthMiddleware(stubAuthenticator{err: errors.New("bad token")})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}
