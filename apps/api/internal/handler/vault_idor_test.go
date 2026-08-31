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

func TestGetVaultIDORReturns404ForOtherUser(t *testing.T) {
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

	// 404 rather than 403: the response must not reveal that this vault
	// exists. See #1101 — a 403 here is an existence oracle.
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user vault access, got %d", getResp.StatusCode)
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

func TestGetAllocationsIDORReturns404ForOtherUser(t *testing.T) {
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

	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user allocations access, got %d", getResp.StatusCode)
	}
}

// createVaultForOwner creates a vault as ownerID and returns it, so the
// cross-user cases below share one setup path.
func createVaultForOwner(t *testing.T, mux http.Handler, ownerID uuid.UUID) vault.Vault {
	t.Helper()

	ownerServer := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer ownerServer.Close()

	body := bytes.NewBufferString(`{"contract_address":"` + testAddrGeneric + `","currency":"USDC"}`)
	createResp, err := http.Post(ownerServer.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create vault: expected 201, got %d", createResp.StatusCode)
	}
	return decodeAPIData[vault.Vault](t, createResp.Body)
}

// TestSharePriceIDORReturns404ForOtherUser pins the fix for #1150. The
// share-price response carries this vault's total assets and total shares, so
// reading it as a non-owner disclosed another user's balance. 404 rather than
// 403 for the same reason as getVault: 403 confirms the vault exists.
func TestSharePriceIDORReturns404ForOtherUser(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	repository := newHandlerRepository(ownerID, otherID)
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	created := createVaultForOwner(t, mux, ownerID)

	otherServer := httptest.NewServer(fakeAuthMiddleware(otherID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer otherServer.Close()

	resp, err := http.Get(otherServer.URL + "/api/v1/vaults/" + created.ID.String() + "/share-price")
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id}/share-price error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user share-price access, got %d", resp.StatusCode)
	}
}

func TestSharePriceReturns200ForOwner(t *testing.T) {
	ownerID := uuid.New()
	repository := newHandlerRepository(ownerID)
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	created := createVaultForOwner(t, mux, ownerID)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/share-price")
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id}/share-price error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for owner reading own share price, got %d", resp.StatusCode)
	}
}

// TestConvertIDORReturns404ForOtherUser pins the fix for #1150. Conversion is
// evaluated at the vault's own share price, so it discloses the same balance
// data as the share-price endpoint.
func TestConvertIDORReturns404ForOtherUser(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	repository := newHandlerRepository(ownerID, otherID)
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	created := createVaultForOwner(t, mux, ownerID)

	otherServer := httptest.NewServer(fakeAuthMiddleware(otherID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer otherServer.Close()

	resp, err := http.Get(otherServer.URL + "/api/v1/vaults/" + created.ID.String() + "/convert?shares=100")
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id}/convert error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-user convert access, got %d", resp.StatusCode)
	}
}

func TestConvertReturns200ForOwner(t *testing.T) {
	ownerID := uuid.New()
	repository := newHandlerRepository(ownerID)
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	created := createVaultForOwner(t, mux, ownerID)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/convert?shares=100")
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id}/convert error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for owner converting in own vault, got %d", resp.StatusCode)
	}
}

// TestListAllVaultsRouteIsGone pins the removal of GET /api/v1/vaults/all
// (#1150). It returned every vault's user_id, contract_address,
// total_deposited and current_balance to any authenticated caller, letting one
// user page the whole platform's ledger. It had no frontend consumer, and
// GET /api/v1/admin/vaults already serves the same listing behind the admin
// role with a superset of filters, so the route was removed rather than
// re-gated.
func TestListAllVaultsRouteIsGone(t *testing.T) {
	ownerID := uuid.New()
	repository := newHandlerRepository(ownerID)
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/vaults/all")
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/all error = %v", err)
	}
	defer resp.Body.Close()

	// "all" now falls through to GET /api/v1/vaults/{id}, which rejects it as
	// a malformed UUID. What matters is that no vault listing comes back.
	if resp.StatusCode == http.StatusOK {
		t.Fatal("GET /api/v1/vaults/all must not return a platform-wide vault listing")
	}
}
