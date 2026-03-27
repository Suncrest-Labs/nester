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
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

func TestVaultHandlerCreateGetAndList(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	repository := newHandlerRepository(userID, otherUserID)
	vaultService := service.NewVaultService(repository)

	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	body := bytes.NewBufferString(`{"user_id":"` + userID.String() + `","contract_address":"CA-001","currency":"USDC"}`)
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

	if _, err := vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	if _, err := vaultService.UpdateAllocations(context.Background(), service.UpdateAllocationsInput{
		VaultID: created.ID,
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("40"), APY: decimal.RequireFromString("4.1")},
			{Protocol: "blend", Amount: decimal.RequireFromString("60"), APY: decimal.RequireFromString("5.2")},
		},
	}); err != nil {
		t.Fatalf("UpdateAllocations() error = %v", err)
	}

	getResponse, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String())
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id} error = %v", err)
	}
	defer getResponse.Body.Close()

	var fetched vault.Vault
	if err := json.NewDecoder(getResponse.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode get response: %v", err)
	}

	if len(fetched.Allocations) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(fetched.Allocations))
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("expected current balance 100, got %s", fetched.CurrentBalance)
	}

	if _, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          otherUserID,
		ContractAddress: "CA-002",
		Currency:        "USDC",
	}); err != nil {
		t.Fatalf("CreateVault(other user) error = %v", err)
	}

	listResponse, err := http.Get(server.URL + "/api/v1/users/" + userID.String() + "/vaults")
	if err != nil {
		t.Fatalf("GET /api/v1/users/{userId}/vaults error = %v", err)
	}
	defer listResponse.Body.Close()

	var vaults []vault.Vault
	if err := json.NewDecoder(listResponse.Body).Decode(&vaults); err != nil {
		t.Fatalf("decode list response: %v", err)
	}

	if len(vaults) != 1 {
		t.Fatalf("expected 1 vault for user, got %d", len(vaults))
	}
}

func TestVaultHandlerNotFoundAndInvalidUser(t *testing.T) {
	repository := newHandlerRepository(uuid.New())
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	notFoundResponse, err := http.Get(server.URL + "/api/v1/vaults/" + uuid.New().String())
	if err != nil {
		t.Fatalf("GET missing vault error = %v", err)
	}
	defer notFoundResponse.Body.Close()

	if notFoundResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing vault, got %d", notFoundResponse.StatusCode)
	}

	invalidUserResponse, err := http.Get(server.URL + "/api/v1/users/not-a-uuid/vaults")
	if err != nil {
		t.Fatalf("GET invalid user error = %v", err)
	}
	defer invalidUserResponse.Body.Close()

	if invalidUserResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid user id, got %d", invalidUserResponse.StatusCode)
	}
}

func TestVaultHandlerProtectedRoutesRequireOwnership(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	repository := newHandlerRepository(userID, otherUserID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.RegisterProtected(mux, middleware.AuthMiddleware(stubAccessAuthenticator{
		principal: service.AuthPrincipal{UserID: userID, WalletAddress: "GTEST"},
	}))

	server := httptest.NewServer(mux)
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/users/" + otherUserID.String() + "/vaults")
	if err != nil {
		t.Fatalf("GET protected vault list error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth header, got %d", response.StatusCode)
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/users/"+otherUserID.String()+"/vaults", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Authorization", "Bearer valid-token")

	authenticatedResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("authenticated GET error = %v", err)
	}
	defer authenticatedResponse.Body.Close()

	if authenticatedResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for another user's vaults, got %d", authenticatedResponse.StatusCode)
	}
}

type handlerRepository struct {
	users  map[uuid.UUID]struct{}
	vaults map[uuid.UUID]vault.Vault
}

type stubAccessAuthenticator struct {
	principal service.AuthPrincipal
}

func (s stubAccessAuthenticator) AuthenticateAccessToken(_ string) (service.AuthPrincipal, error) {
	return s.principal, nil
}

func newHandlerRepository(userIDs ...uuid.UUID) *handlerRepository {
	users := make(map[uuid.UUID]struct{}, len(userIDs))
	for _, userID := range userIDs {
		users[userID] = struct{}{}
	}
	return &handlerRepository{
		users:  users,
		vaults: make(map[uuid.UUID]vault.Vault),
	}
}

func (r *handlerRepository) CreateVault(_ context.Context, model vault.Vault) (vault.Vault, error) {
	if _, ok := r.users[model.UserID]; !ok {
		return vault.Vault{}, vault.ErrUserNotFound
	}
	now := time.Now().UTC()
	model.CreatedAt = now
	model.UpdatedAt = now
	model.Allocations = []vault.Allocation{}
	r.vaults[model.ID] = cloneHandlerVault(model)
	return cloneHandlerVault(model), nil
}

func (r *handlerRepository) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	model, ok := r.vaults[id]
	if !ok {
		return vault.Vault{}, vault.ErrVaultNotFound
	}
	return cloneHandlerVault(model), nil
}

func (r *handlerRepository) GetUserVaults(_ context.Context, userID uuid.UUID) ([]vault.Vault, error) {
	models := make([]vault.Vault, 0)
	for _, model := range r.vaults {
		if model.UserID == userID {
			models = append(models, cloneHandlerVault(model))
		}
	}
	return models, nil
}

func (r *handlerRepository) UpdateVaultBalances(_ context.Context, id uuid.UUID, totalDeposited decimal.Decimal, currentBalance decimal.Decimal) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.TotalDeposited = totalDeposited
	model.CurrentBalance = currentBalance
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)
	return nil
}

func (r *handlerRepository) RecordDeposit(_ context.Context, id uuid.UUID, amount decimal.Decimal) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	model.TotalDeposited = model.TotalDeposited.Add(amount)
	model.CurrentBalance = model.CurrentBalance.Add(amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)
	return nil
}

func (r *handlerRepository) ReplaceAllocations(_ context.Context, vaultID uuid.UUID, allocations []vault.Allocation) error {
	model, ok := r.vaults[vaultID]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.Allocations = append([]vault.Allocation(nil), allocations...)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[vaultID] = cloneHandlerVault(model)
	return nil
}

func cloneHandlerVault(model vault.Vault) vault.Vault {
	model.Allocations = append([]vault.Allocation(nil), model.Allocations...)
	return model
}
