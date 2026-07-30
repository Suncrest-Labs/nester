package handler

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

func TestGetVaultIDORReturns403ForOtherUser(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	repository := newHandlerRepository(ownerID, otherID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	ownerServer := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer ownerServer.Close()

	// Owner creates vault
	body := bytes.NewBufferString(`{"contract_address":"` + testAddrGeneric + `","currency":"USDC"}`)
	createResp, err := http.Post(ownerServer.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create vault: expected 201, got %d", createResp.StatusCode)
	}
	created := decodeAPIData[vault.Vault](t, createResp.Body)

	// Other user tries to read owner's vault
	otherServer := httptest.NewServer(fakeAuthMiddleware(otherID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer otherServer.Close()

	getResp, err := http.Get(otherServer.URL + "/api/v1/vaults/" + created.ID.String())
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id} error = %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-user vault access, got %d", getResp.StatusCode)
	}
}

func TestGetVaultReturns200ForOwner(t *testing.T) {
	ownerID := uuid.New()
	repository := newHandlerRepository(ownerID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"contract_address":"` + testAddrGeneric + `","currency":"USDC"}`)
	createResp, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer createResp.Body.Close()
	created := decodeAPIData[vault.Vault](t, createResp.Body)

	getResp, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String())
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id} error = %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for owner reading own vault, got %d", getResp.StatusCode)
	}
}

func TestGetAllocationsIDORReturns403ForOtherUser(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	repository := newHandlerRepository(ownerID, otherID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	ownerServer := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer ownerServer.Close()

	body := bytes.NewBufferString(`{"contract_address":"` + testAddrGeneric + `","currency":"USDC"}`)
	createResp, err := http.Post(ownerServer.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer createResp.Body.Close()
	created := decodeAPIData[vault.Vault](t, createResp.Body)

	otherServer := httptest.NewServer(fakeAuthMiddleware(otherID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer otherServer.Close()

	getResp, err := http.Get(otherServer.URL + "/api/v1/vaults/" + created.ID.String() + "/allocations")
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id}/allocations error = %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-user allocations access, got %d", getResp.StatusCode)
	}
}
