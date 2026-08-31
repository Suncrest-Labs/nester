package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// Registering a vault against a contract address another live vault already
// holds must be refused with a 409 the client can act on.
//
// The attack this closes: the event indexer keys balance mutations on
// contract_address, so a second vault pointing at a victim's contract makes
// every on-chain deposit the victim performs credit the attacker's vault too —
// and that balance is withdrawable (nester#1148).
func TestVaultHandlerRejectsDuplicateContractAddress(t *testing.T) {
	const contractAddress = "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	userID := uuid.New()
	attackerID := uuid.New()
	repository := newHandlerRepository(userID, attackerID)
	vaultService := service.NewVaultService(repository)

	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	post := func(t *testing.T, as uuid.UUID) *http.Response {
		t.Helper()
		server := httptest.NewServer(
			fakeAuthMiddleware(as)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)),
		)
		t.Cleanup(server.Close)

		body := bytes.NewBufferString(`{"contract_address":"` + contractAddress + `","currency":"USDC"}`)
		response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
		if err != nil {
			t.Fatalf("POST /api/v1/vaults error = %v", err)
		}
		return response
	}

	first := post(t, userID)
	defer first.Body.Close()
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first registration: expected 201, got %d", first.StatusCode)
	}

	// A different user claiming the same address is the hostile case.
	second := post(t, attackerID)
	defer second.Body.Close()

	if second.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate registration: expected 409, got %d", second.StatusCode)
	}

	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(second.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.Error.Code != "CONTRACT_ADDRESS_REGISTERED" {
		t.Fatalf("error code = %q, want CONTRACT_ADDRESS_REGISTERED", envelope.Error.Code)
	}
}
