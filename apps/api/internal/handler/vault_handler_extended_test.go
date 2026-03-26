package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

func TestVaultHandlerGetVaultReturns200WithAllocations(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	// Create vault
	body := bytes.NewBufferString(`{"user_id":"` + userID.String() + `","contract_address":"CA-GET-ALLOC-001","currency":"USDC"}`)
	response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	var created vault.Vault
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// Add allocations
	_, err = vaultService.UpdateAllocations(context.Background(), service.UpdateAllocationsInput{
		VaultID: created.ID,
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("60"), APY: decimal.RequireFromString("4.5")},
			{Protocol: "blend", Amount: decimal.RequireFromString("40"), APY: decimal.RequireFromString("5.2")},
		},
	})
	if err != nil {
		t.Fatalf("UpdateAllocations() error = %v", err)
	}

	// Get vault with allocations
	getResponse, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String())
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id} error = %v", err)
	}
	defer getResponse.Body.Close()

	if getResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", getResponse.StatusCode)
	}

	var fetched vault.Vault
	if err := json.NewDecoder(getResponse.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode get response: %v", err)
	}

	if fetched.ID != created.ID {
		t.Fatalf("fetched ID = %v, want %v", fetched.ID, created.ID)
	}
	if len(fetched.Allocations) != 2 {
		t.Fatalf("fetched %d allocations, want 2", len(fetched.Allocations))
	}

	// Verify allocations
	protocols := make(map[string]bool)
	for _, alloc := range fetched.Allocations {
		protocols[alloc.Protocol] = true
	}
	if !protocols["aave"] {
		t.Fatal("allocation 'aave' should be present")
	}
	if !protocols["blend"] {
		t.Fatal("allocation 'blend' should be present")
	}
}

func TestVaultHandlerGetVaultReturns404WhenNotFound(t *testing.T) {
	repository := newHandlerRepository(uuid.New())
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	// Get non-existent vault
	response, err := http.Get(server.URL + "/api/v1/vaults/" + uuid.New().String())
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id} error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}

	var errorResp errorResponse
	if err := json.NewDecoder(response.Body).Decode(&errorResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}

	if errorResp.Error == "" {
		t.Fatal("error response should have error message")
	}
}

func TestVaultHandlerGetVaultReturns400ForInvalidID(t *testing.T) {
	repository := newHandlerRepository(uuid.New())
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	// Get vault with invalid ID
	response, err := http.Get(server.URL + "/api/v1/vaults/not-a-uuid")
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id} error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
}

func TestVaultHandlerListUserVaultsReturns200WithAllocations(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	// Create first vault with allocations
	body1 := bytes.NewBufferString(`{"user_id":"` + userID.String() + `","contract_address":"CA-LIST-001","currency":"USDC"}`)
	response1, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body1)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response1.Body.Close()

	var created1 vault.Vault
	if err := json.NewDecoder(response1.Body).Decode(&created1); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	_, err = vaultService.UpdateAllocations(context.Background(), service.UpdateAllocationsInput{
		VaultID: created1.ID,
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("100"), APY: decimal.RequireFromString("4.5")},
		},
	})
	if err != nil {
		t.Fatalf("UpdateAllocations() error = %v", err)
	}

	// Create second vault with allocations
	body2 := bytes.NewBufferString(`{"user_id":"` + userID.String() + `","contract_address":"CA-LIST-002","currency":"USDC"}`)
	response2, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body2)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response2.Body.Close()

	var created2 vault.Vault
	if err := json.NewDecoder(response2.Body).Decode(&created2); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	_, err = vaultService.UpdateAllocations(context.Background(), service.UpdateAllocationsInput{
		VaultID: created2.ID,
		Allocations: []vault.Allocation{
			{Protocol: "blend", Amount: decimal.RequireFromString("60"), APY: decimal.RequireFromString("5.2")},
			{Protocol: "compound", Amount: decimal.RequireFromString("40"), APY: decimal.RequireFromString("3.8")},
		},
	})
	if err != nil {
		t.Fatalf("UpdateAllocations() error = %v", err)
	}

	// List user vaults
	listResponse, err := http.Get(server.URL + "/api/v1/users/" + userID.String() + "/vaults")
	if err != nil {
		t.Fatalf("GET /api/v1/users/{userId}/vaults error = %v", err)
	}
	defer listResponse.Body.Close()

	if listResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", listResponse.StatusCode)
	}

	var vaults []vault.Vault
	if err := json.NewDecoder(listResponse.Body).Decode(&vaults); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	if len(vaults) != 2 {
		t.Fatalf("fetched %d vaults, want 2", len(vaults))
	}

	// Verify allocations are included
	for _, v := range vaults {
		if v.ID == created1.ID {
			if len(v.Allocations) != 1 {
				t.Fatalf("vault1 has %d allocations, want 1", len(v.Allocations))
			}
		} else if v.ID == created2.ID {
			if len(v.Allocations) != 2 {
				t.Fatalf("vault2 has %d allocations, want 2", len(v.Allocations))
			}
		}
	}
}

func TestVaultHandlerCreateVaultReturns201OnSuccess(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	body := bytes.NewBufferString(`{"user_id":"` + userID.String() + `","contract_address":"CA-CREATE-001","currency":"USDC"}`)
	response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	var created vault.Vault
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Fatal("created vault should have non-nil ID")
	}
	if created.UserID != userID {
		t.Fatalf("created vault UserID = %v, want %v", created.UserID, userID)
	}
	if created.ContractAddress != "CA-CREATE-001" {
		t.Fatalf("created vault ContractAddress = %q, want %q", created.ContractAddress, "CA-CREATE-001")
	}
	if created.Currency != "USDC" {
		t.Fatalf("created vault Currency = %q, want %q", created.Currency, "USDC")
	}
	if created.Status != vault.StatusActive {
		t.Fatalf("created vault Status = %q, want %q", created.Status, vault.StatusActive)
	}
}

func TestVaultHandlerCreateVaultReturns422OnInvalidInput(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	tests := []struct {
		name           string
		body           string
		expectedStatus int
	}{
		{
			name:           "invalid JSON",
			body:           `{"user_id":"` + userID.String() + `","contract_address":"CA-001"`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid user_id",
			body:           `{"user_id":"not-a-uuid","contract_address":"CA-001","currency":"USDC"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "empty contract_address",
			body:           `{"user_id":"` + userID.String() + `","contract_address":"","currency":"USDC"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid currency",
			body:           `{"user_id":"` + userID.String() + `","contract_address":"CA-001","currency":"INVALID"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "currency too short",
			body:           `{"user_id":"` + userID.String() + `","contract_address":"CA-001","currency":"US"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "currency too long",
			body:           `{"user_id":"` + userID.String() + `","contract_address":"CA-001","currency":"USDCX"}`,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "currency with numbers",
			body:           `{"user_id":"` + userID.String() + `","contract_address":"CA-001","currency":"US1C"}`,
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := bytes.NewBufferString(tt.body)
			response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
			if err != nil {
				t.Fatalf("POST /api/v1/vaults error = %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != tt.expectedStatus {
				t.Fatalf("expected status %d, got %d", tt.expectedStatus, response.StatusCode)
			}
		})
	}
}

func TestVaultHandlerCreateVaultReturns404ForNonExistentUser(t *testing.T) {
	repository := newHandlerRepository(uuid.New()) // Only this user exists
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	// Try to create vault for non-existent user
	nonExistentUserID := uuid.New()
	body := bytes.NewBufferString(`{"user_id":"` + nonExistentUserID.String() + `","contract_address":"CA-001","currency":"USDC"}`)
	response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}
}

func TestVaultHandlerListUserVaultsReturns400ForInvalidUserID(t *testing.T) {
	repository := newHandlerRepository(uuid.New())
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	// List vaults with invalid user ID
	response, err := http.Get(server.URL + "/api/v1/users/not-a-uuid/vaults")
	if err != nil {
		t.Fatalf("GET /api/v1/users/{userId}/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
}

func TestVaultHandlerCreateVaultWithCustomStatus(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	body := bytes.NewBufferString(`{"user_id":"` + userID.String() + `","contract_address":"CA-STATUS-001","currency":"USDC","status":"paused"}`)
	response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	var created vault.Vault
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	if created.Status != vault.StatusPaused {
		t.Fatalf("created vault Status = %q, want %q", created.Status, vault.StatusPaused)
	}
}

func TestVaultHandlerCreateVaultNormalizesCurrency(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	body := bytes.NewBufferString(`{"user_id":"` + userID.String() + `","contract_address":"CA-NORM-001","currency":"usdc"}`)
	response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	var created vault.Vault
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	if created.Currency != "USDC" {
		t.Fatalf("created vault Currency = %q, want %q", created.Currency, "USDC")
	}
}

func TestVaultHandlerCreateVaultTrimsWhitespace(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	body := bytes.NewBufferString(`{"user_id":"` + userID.String() + `","contract_address":"  CA-TRIM-001  ","currency":"  USDC  "}`)
	response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	var created vault.Vault
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	if created.ContractAddress != "CA-TRIM-001" {
		t.Fatalf("created vault ContractAddress = %q, want %q", created.ContractAddress, "CA-TRIM-001")
	}
	if created.Currency != "USDC" {
		t.Fatalf("created vault Currency = %q, want %q", created.Currency, "USDC")
	}
}
