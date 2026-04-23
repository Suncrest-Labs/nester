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

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// fakeAuthMiddleware injects an auth.User into the request context for testing.
func fakeAuthMiddleware(userID uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.NewContext(r.Context(), auth.User{ID: userID.String()})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// decodeAPIData unwraps the API envelope {"success":true,"data":...} and decodes
// the inner data field into T.
func decodeAPIData[T any](t *testing.T, body io.Reader) T {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var result T
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	return result
}

func TestVaultHandlerCreateGetAndList(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	repository := newHandlerRepository(userID, otherUserID)
	vaultService := service.NewVaultService(repository)

	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"contract_address":"CA-001","currency":"USDC"}`)
	response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	created := decodeAPIData[vault.Vault](t, response.Body)

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

	fetched := decodeAPIData[vault.Vault](t, getResponse.Body)

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

	// Note: fakeAuthMiddleware uses userID, so the auth check in listUserVaults will pass
	listResponse, err := http.Get(server.URL + "/api/v1/users/" + userID.String() + "/vaults")
	if err != nil {
		t.Fatalf("GET /api/v1/users/{userId}/vaults error = %v", err)
	}
	defer listResponse.Body.Close()

	vaults := decodeAPIData[[]vault.Vault](t, listResponse.Body)

	if len(vaults) != 1 {
		t.Fatalf("expected 1 vault for user, got %d", len(vaults))
	}
}

func TestVaultHandlerNotFoundAndInvalidUser(t *testing.T) {
	repository := newHandlerRepository(uuid.New())
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(uuid.New())(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
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

type handlerRepository struct {
	users        map[uuid.UUID]struct{}
	vaults       map[uuid.UUID]vault.Vault
	transactions []vault.VaultTransaction
}

func newHandlerRepository(userIDs ...uuid.UUID) *handlerRepository {
	users := make(map[uuid.UUID]struct{}, len(userIDs))
	for _, userID := range userIDs {
		users[userID] = struct{}{}
	}
	return &handlerRepository{
		users:        users,
		vaults:       make(map[uuid.UUID]vault.Vault),
		transactions: make([]vault.VaultTransaction, 0),
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
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:        uuid.New(),
		VaultID:   id,
		Type:      "deposit",
		Amount:    amount,
		CreatedAt: time.Now().UTC(),
	})
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

func (r *handlerRepository) UpdateVault(_ context.Context, id uuid.UUID, contractAddress string, status vault.VaultStatus) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.ContractAddress = contractAddress
	model.Status = status
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)
	return nil
}

func (r *handlerRepository) RecordWithdrawal(_ context.Context, id uuid.UUID, amount decimal.Decimal) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	model.CurrentBalance = model.CurrentBalance.Sub(amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:        uuid.New(),
		VaultID:   id,
		Type:      "withdrawal",
		Amount:    amount,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (r *handlerRepository) SoftDeleteVault(_ context.Context, id uuid.UUID) error {
	if _, ok := r.vaults[id]; !ok {
		return vault.ErrVaultNotFound
	}
	delete(r.vaults, id)
	return nil
}

func (r *handlerRepository) ListDeposits(_ context.Context, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	result := make([]vault.VaultTransaction, 0)
	for _, txn := range r.transactions {
		if txn.VaultID == vaultID && txn.Type == "deposit" {
			result = append(result, txn)
		}
	}
	return result, nil
}

func cloneHandlerVault(model vault.Vault) vault.Vault {
	model.Allocations = append([]vault.Allocation(nil), model.Allocations...)
	return model
}
