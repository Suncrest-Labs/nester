package handler_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/toolaudit"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type noopRevocationChecker struct{}

func (noopRevocationChecker) IsRevoked(context.Context, string) (bool, error) { return false, nil }

type mockToolAuditRepo struct {
	latestHash string
	latestErr  error
	insertErr  error
}

func (m *mockToolAuditRepo) InsertChained(ctx context.Context, inv toolaudit.ToolInvocation) (toolaudit.ToolInvocation, error) {
	if m.latestErr != nil {
		return toolaudit.ToolInvocation{}, m.latestErr
	}
	if m.insertErr != nil {
		return toolaudit.ToolInvocation{}, m.insertErr
	}
	inv.PrevHash = m.latestHash
	inv.EntryHash = inv.ComputeHash(m.latestHash)
	return inv, nil
}

func TestToolAuditHandler_Auth(t *testing.T) {
	repo := &mockToolAuditRepo{latestHash: "0000"}
	svc := service.NewToolAuditService(repo)
	h := handler.NewToolAuditHandler(svc)

	mux := http.NewServeMux()
	h.Register(mux)

	secret := "test-secret"
	serviceKey := "test-service-key"
	authRules := []middleware.RouteRule{
		{PathPrefix: "/api/v1/internal/", Role: "service"},
	}
	authenticator := middleware.Authenticate(secret, serviceKey, authRules, noopRevocationChecker{})

	server := httptest.NewServer(authenticator(mux))
	defer server.Close()

	// Create user JWT (no service role)
	userToken, err := auth.MakeJWT(auth.Claims{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}, secret)
	if err != nil {
		t.Fatalf("make signed token: %v", err)
	}

	// Test 1: Normal user token should fail
	req1, _ := http.NewRequest("POST", server.URL+"/api/v1/internal/intelligence/tool-audit", bytes.NewBuffer([]byte(`{"user_id":"user-1"}`)))
	req1.Header.Set("Authorization", "Bearer "+userToken)
	resp1, err := http.DefaultClient.Do(req1)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp1.StatusCode)

	// Test 2: Service API key should pass
	req2, _ := http.NewRequest("POST", server.URL+"/api/v1/internal/intelligence/tool-audit", bytes.NewBuffer([]byte(`{"user_id":"user-1", "tool_name":"test"}`)))
	req2.Header.Set("Authorization", "Bearer "+serviceKey)
	req2.Header.Set("X-User-Id", "service-1") // The middleware requires X-User-Id
	resp2, err := http.DefaultClient.Do(req2)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}
