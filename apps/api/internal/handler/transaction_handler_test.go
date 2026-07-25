package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/transaction"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type testTxVaultRepo struct {
	vaults map[uuid.UUID]vault.Vault
}

func newTestTxVaultRepo() *testTxVaultRepo {
	return &testTxVaultRepo{vaults: make(map[uuid.UUID]vault.Vault)}
}

func (r *testTxVaultRepo) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	v, ok := r.vaults[id]
	if !ok {
		return vault.Vault{}, vault.ErrVaultNotFound
	}
	return v, nil
}

func (r *testTxVaultRepo) addVault(v vault.Vault) {
	r.vaults[v.ID] = v
}

type fakeTransactionRepository struct {
	transactions map[string]transaction.Transaction
}

func newFakeTransactionRepository() *fakeTransactionRepository {
	return &fakeTransactionRepository{transactions: make(map[string]transaction.Transaction)}
}

func (r *fakeTransactionRepository) Upsert(_ context.Context, model transaction.Transaction) (transaction.Transaction, error) {
	now := time.Now().UTC()
	model.CreatedAt = now
	model.UpdatedAt = now
	r.transactions[model.TxHash] = model
	return model, nil
}

func (r *fakeTransactionRepository) GetByHash(_ context.Context, hash string) (transaction.Transaction, error) {
	t, ok := r.transactions[hash]
	if !ok {
		return transaction.Transaction{}, transaction.ErrTransactionNotFound
	}
	return t, nil
}

func (r *fakeTransactionRepository) UpdateStatus(_ context.Context, hash string, status transaction.TransactionStatus, confirmedAt *time.Time, errorReason string) (transaction.Transaction, error) {
	t, ok := r.transactions[hash]
	if !ok {
		return transaction.Transaction{}, transaction.ErrTransactionNotFound
	}
	t.Status = status
	t.ConfirmedAt = confirmedAt
	t.ErrorReason = errorReason
	t.UpdatedAt = time.Now().UTC()
	r.transactions[hash] = t
	return t, nil
}

func (r *fakeTransactionRepository) ListPendingOlderThan(_ context.Context, _ time.Time) ([]transaction.Transaction, error) {
	return nil, nil
}

func (r *fakeTransactionRepository) ListUserTransactions(_ context.Context, _ transaction.ListFilter) ([]transaction.Transaction, int, error) {
	return nil, 0, nil
}

func TestCreateTransactionIDORReturns403ForOtherUser(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()

	vaultRepo := newTestTxVaultRepo()
	vaultID := uuid.New()
	vaultRepo.addVault(vault.Vault{
		ID:     vaultID,
		UserID: ownerID,
		Status: vault.StatusActive,
	})

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	handler.SetVaultRepository(vaultRepo)
	mux := http.NewServeMux()
	handler.Register(mux)

	otherServer := httptest.NewServer(fakeAuthMiddleware(otherID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer otherServer.Close()

	body := bytes.NewBufferString(`{"vault_id":"` + vaultID.String() + `","type":"deposit","amount":"100.00","currency":"USDC","tx_hash":"abc123"}`)
	resp, err := http.Post(otherServer.URL+"/api/v1/transactions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/transactions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-user transaction creation, got %d", resp.StatusCode)
	}
}

func TestCreateTransactionReturns201ForOwner(t *testing.T) {
	ownerID := uuid.New()

	vaultRepo := newTestTxVaultRepo()
	vaultID := uuid.New()
	vaultRepo.addVault(vault.Vault{
		ID:     vaultID,
		UserID: ownerID,
		Status: vault.StatusActive,
	})

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	handler.SetVaultRepository(vaultRepo)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"vault_id":"` + vaultID.String() + `","type":"deposit","amount":"100.00","currency":"USDC","tx_hash":"abc123"}`)
	resp, err := http.Post(server.URL+"/api/v1/transactions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/transactions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for owner creating transaction, got %d", resp.StatusCode)
	}
}

func TestCreateTransactionReturns400ForInvalidAmount(t *testing.T) {
	ownerID := uuid.New()

	vaultRepo := newTestTxVaultRepo()
	vaultID := uuid.New()
	vaultRepo.addVault(vault.Vault{
		ID:     vaultID,
		UserID: ownerID,
		Status: vault.StatusActive,
	})

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	handler.SetVaultRepository(vaultRepo)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"vault_id":"` + vaultID.String() + `","type":"deposit","amount":"not-a-number","currency":"USDC","tx_hash":"abc123"}`)
	resp, err := http.Post(server.URL+"/api/v1/transactions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/transactions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid amount, got %d", resp.StatusCode)
	}
}

func TestCreateTransactionReturns400ForInvalidType(t *testing.T) {
	ownerID := uuid.New()

	vaultRepo := newTestTxVaultRepo()
	vaultID := uuid.New()
	vaultRepo.addVault(vault.Vault{
		ID:     vaultID,
		UserID: ownerID,
		Status: vault.StatusActive,
	})

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	handler.SetVaultRepository(vaultRepo)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"vault_id":"` + vaultID.String() + `","type":"invalid","amount":"100.00","currency":"USDC","tx_hash":"abc123"}`)
	resp, err := http.Post(server.URL+"/api/v1/transactions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/transactions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid type, got %d", resp.StatusCode)
	}
}

func TestCreateTransactionReturns400ForInvalidVaultID(t *testing.T) {
	ownerID := uuid.New()

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"vault_id":"not-a-uuid","type":"deposit","amount":"100.00","currency":"USDC","tx_hash":"abc123"}`)
	resp, err := http.Post(server.URL+"/api/v1/transactions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/transactions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid vault_id, got %d", resp.StatusCode)
	}
}

func TestCreateTransactionReturns404ForNonexistentVault(t *testing.T) {
	ownerID := uuid.New()

	vaultRepo := newTestTxVaultRepo()
	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	handler.SetVaultRepository(vaultRepo)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"vault_id":"` + uuid.New().String() + `","type":"deposit","amount":"100.00","currency":"USDC","tx_hash":"abc123"}`)
	resp, err := http.Post(server.URL+"/api/v1/transactions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/transactions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent vault, got %d", resp.StatusCode)
	}
}

func TestCreateTransactionWithoutVaultRepoSkipsOwnershipCheck(t *testing.T) {
	ownerID := uuid.New()

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"vault_id":"` + uuid.New().String() + `","type":"deposit","amount":"100.00","currency":"USDC","tx_hash":"abc123"}`)
	resp, err := http.Post(server.URL+"/api/v1/transactions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/transactions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 when vaultRepo is nil (backward compat), got %d", resp.StatusCode)
	}
}

func TestCreateTransactionReturns401WithoutAuth(t *testing.T) {
	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	body := bytes.NewBufferString(`{"vault_id":"` + uuid.New().String() + `","type":"deposit","amount":"100.00","currency":"USDC","tx_hash":"abc123"}`)
	resp, err := http.Post(server.URL+"/api/v1/transactions", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/transactions error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

func TestGetTransactionByIDORReturns403ForOtherUser(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()

	vaultRepo := newTestTxVaultRepo()
	vaultID := uuid.New()
	vaultRepo.addVault(vault.Vault{
		ID:     vaultID,
		UserID: ownerID,
		Status: vault.StatusActive,
	})

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	handler.SetVaultRepository(vaultRepo)
	mux := http.NewServeMux()
	handler.Register(mux)

	otherServer := httptest.NewServer(fakeAuthMiddleware(otherID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer otherServer.Close()

	txHash := "tx_hash_123"
	txRepo.transactions[txHash] = transaction.Transaction{
		ID:      uuid.New(),
		VaultID: vaultID,
		Type:    transaction.TypeDeposit,
		TxHash:  txHash,
		Status:  transaction.StatusPending,
	}

	resp, err := http.Get(otherServer.URL + "/api/v1/transactions/" + txHash)
	if err != nil {
		t.Fatalf("GET /api/v1/transactions/{hash} error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-user transaction read, got %d", resp.StatusCode)
	}
}

func TestGetTransactionByHashReturns200ForOwner(t *testing.T) {
	ownerID := uuid.New()

	vaultRepo := newTestTxVaultRepo()
	vaultID := uuid.New()
	vaultRepo.addVault(vault.Vault{
		ID:     vaultID,
		UserID: ownerID,
		Status: vault.StatusActive,
	})

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	handler.SetVaultRepository(vaultRepo)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	txHash := "tx_hash_owner"
	txRepo.transactions[txHash] = transaction.Transaction{
		ID:      uuid.New(),
		VaultID: vaultID,
		Type:    transaction.TypeDeposit,
		Amount:  decimal.NewFromFloat(100.0),
		Currency: "USDC",
		TxHash:  txHash,
		Status:  transaction.StatusPending,
	}

	resp, err := http.Get(server.URL + "/api/v1/transactions/" + txHash)
	if err != nil {
		t.Fatalf("GET /api/v1/transactions/{hash} error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for owner reading own transaction, got %d", resp.StatusCode)
	}
}

func TestGetTransactionByHashReturns401WithoutAuth(t *testing.T) {
	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/transactions/some_hash")
	if err != nil {
		t.Fatalf("GET /api/v1/transactions/{hash} error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

func TestGetTransactionByHashWithoutVaultRepoSkipsOwnershipCheck(t *testing.T) {
	ownerID := uuid.New()

	txRepo := newFakeTransactionRepository()
	txService := service.NewTransactionService(txRepo, "")

	handler := NewTransactionHandler(txService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(ownerID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	txHash := "tx_hash_novault"
	txRepo.transactions[txHash] = transaction.Transaction{
		ID:      uuid.New(),
		VaultID: uuid.New(),
		Type:    transaction.TypeDeposit,
		TxHash:  txHash,
		Status:  transaction.StatusPending,
	}

	resp, err := http.Get(server.URL + "/api/v1/transactions/" + txHash)
	if err != nil {
		t.Fatalf("GET /api/v1/transactions/{hash} error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 when vaultRepo is nil (backward compat), got %d", resp.StatusCode)
	}
}
