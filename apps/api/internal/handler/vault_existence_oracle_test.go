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

// TestVaultNotAnExistenceOracle is the direct test for the #1101 criterion
// "non-owner access to an existing resource returns 404, not 403, so the
// endpoint is not an existence oracle".
//
// Asserting the status code alone is not enough: if a non-owner gets 404 but
// with a distinguishable body, the oracle still works. This compares the two
// responses a non-owner can provoke — one for a vault that exists and belongs
// to someone else, one for a vault ID that was never created — and requires
// them to be indistinguishable.
func TestVaultNotAnExistenceOracle(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	repository := newHandlerRepository(ownerID, otherID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))

	ownerServer := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(discard)(mux)))
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
	created := decodeAPIData[vault.Vault](t, createResp.Body)

	otherServer := httptest.NewServer(fakeAuthMiddleware(otherID)(middleware.Logging(discard)(mux)))
	defer otherServer.Close()

	get := func(t *testing.T, path string) (int, string) {
		t.Helper()
		resp, err := http.Get(otherServer.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		defer resp.Body.Close()
		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body for %s: %v", path, err)
		}
		return resp.StatusCode, string(payload)
	}

	// A vault that exists but belongs to another user.
	existingCode, existingBody := get(t, "/api/v1/vaults/"+created.ID.String())
	// A vault that does not exist at all.
	missingCode, missingBody := get(t, "/api/v1/vaults/"+uuid.NewString())

	if existingCode != http.StatusNotFound {
		t.Errorf("someone else's existing vault: got %d, want 404", existingCode)
	}
	if missingCode != http.StatusNotFound {
		t.Errorf("nonexistent vault: got %d, want 404", missingCode)
	}
	if existingCode != missingCode || existingBody != missingBody {
		t.Errorf("responses are distinguishable, so the endpoint is an existence oracle:\n"+
			"  existing vault owned by another user: %d %s\n"+
			"  vault that does not exist:            %d %s",
			existingCode, existingBody, missingCode, missingBody)
	}
}

// The owner must still be able to read their own vault, otherwise the test
// above would be satisfied by an endpoint that 404s for everyone.
func TestVaultOwnerStillReadsOwnVault(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	repository := newHandlerRepository(ownerID, otherID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	ownerServer := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(discard)(mux)))
	defer ownerServer.Close()

	body := bytes.NewBufferString(`{"contract_address":"` + testAddrGeneric + `","currency":"USDC"}`)
	createResp, err := http.Post(ownerServer.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer createResp.Body.Close()
	created := decodeAPIData[vault.Vault](t, createResp.Body)

	resp, err := http.Get(ownerServer.URL + "/api/v1/vaults/" + created.ID.String())
	if err != nil {
		t.Fatalf("owner GET error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner reading own vault: got %d, want 200", resp.StatusCode)
	}
}
