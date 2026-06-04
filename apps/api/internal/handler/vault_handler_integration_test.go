package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

func TestVaultHandlerIntegrationCreateGetListAndErrors(t *testing.T) {
	db := openHandlerIntegrationDB(t)
	applyHandlerIntegrationMigrations(t, db)
	resetHandlerIntegrationTables(t, db)

	userID := seedHandlerIntegrationUser(t, db)
	otherUserID := seedHandlerIntegrationUser(t, db)

	repository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	response, err := http.Post(
		server.URL+"/api/v1/vaults",
		"application/json",
		bytes.NewBufferString(`{"contract_address":"CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","currency":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", response.StatusCode)
	}

	var created vault.Vault
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	if _, err := vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100.00"),
	}); err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	if _, err := vaultService.UpdateAllocations(context.Background(), service.UpdateAllocationsInput{
		VaultID: created.ID,
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("40.00"), APY: decimal.RequireFromString("4.00"), AllocatedAt: time.Now().UTC()},
			{Protocol: "blend", Amount: decimal.RequireFromString("60.00"), APY: decimal.RequireFromString("5.00"), AllocatedAt: time.Now().UTC().Add(time.Second)},
		},
	}); err != nil {
		t.Fatalf("UpdateAllocations() error = %v", err)
	}

	if _, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          otherUserID,
		ContractAddress: "CA-H-002",
		Currency:        "USDC",
	}); err != nil {
		t.Fatalf("CreateVault(other user) error = %v", err)
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

	listResponse, err := http.Get(server.URL + "/api/v1/vaults?userId=" + userID.String())
	if err != nil {
		t.Fatalf("GET GET /api/v1/vaults?userId={userId} error = %v", err)
	}
	defer listResponse.Body.Close()

	var listed []vault.Vault
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed vault, got %d", len(listed))
	}

	notFoundResponse, err := http.Get(server.URL + "/api/v1/vaults/" + uuid.New().String())
	if err != nil {
		t.Fatalf("GET missing vault error = %v", err)
	}
	defer notFoundResponse.Body.Close()
	if notFoundResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing vault, got %d", notFoundResponse.StatusCode)
	}

	invalidUserResponse, err := http.Get(server.URL + "/api/v1/vaults?userId=not-a-uuid")
	if err != nil {
		t.Fatalf("GET invalid user error = %v", err)
	}
	defer invalidUserResponse.Body.Close()
	if invalidUserResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid user id, got %d", invalidUserResponse.StatusCode)
	}
}

func openHandlerIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func applyHandlerIntegrationMigrations(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, name := range []string{
		"001_create_users_table.up.sql",
		"004_create_vaults_table.up.sql",
		"005_create_allocations_table.up.sql",
	} {
		path := filepath.Join("..", "..", "migrations", name)
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		if _, err := db.Exec(string(contents)); err != nil {
			t.Fatalf("applying migration %q failed: %v", name, err)
		}
	}
}

func resetHandlerIntegrationTables(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`TRUNCATE TABLE allocations, vaults, users RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("TRUNCATE failed: %v", err)
	}
}

func seedHandlerIntegrationUser(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()

	userID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO users (id, email, name) VALUES ($1, $2, $3)`,
		userID.String(),
		userID.String()+"@example.com",
		"Handler Integration User",
	); err != nil {
		t.Fatalf("seed user failed: %v", err)
	}
	return userID
}

func TestVaultHandlerDepositAndWithdrawIntegration(t *testing.T) {
	db := openHandlerIntegrationDB(t)
	applyHandlerIntegrationMigrations(t, db)
	resetHandlerIntegrationTables(t, db)

	userID := seedHandlerIntegrationUser(t, db)
	otherUserID := seedHandlerIntegrationUser(t, db)

	repository := postgres.NewVaultRepository(db)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	// Create a vault first
	response, err := http.Post(
		server.URL+"/api/v1/vaults",
		"application/json",
		bytes.NewBufferString(`{"contract_address":"CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","currency":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", response.StatusCode)
	}

	var created vault.Vault
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// Test: Deposit to vault
	depositResponse, err := http.Post(
		server.URL+"/api/v1/vaults/"+created.ID.String()+"/deposit",
		"application/json",
		bytes.NewBufferString(`{"amount":"100.50","asset":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults/{id}/deposit error = %v", err)
	}
	defer depositResponse.Body.Close()

	if depositResponse.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for deposit, got %d", depositResponse.StatusCode)
	}

	var depositedVault vault.Vault
	if err := json.NewDecoder(depositResponse.Body).Decode(&depositedVault); err != nil {
		t.Fatalf("decode deposit response: %v", err)
	}

	if depositedVault.TotalDeposited.Cmp(decimal.RequireFromString("100.50")) != 0 {
		t.Fatalf("expected total_deposited 100.50, got %v", depositedVault.TotalDeposited)
	}

	// Test: Withdraw from vault
	withdrawResponse, err := http.Post(
		server.URL+"/api/v1/vaults/"+created.ID.String()+"/withdraw",
		"application/json",
		bytes.NewBufferString(`{"amount":"50.00","asset":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults/{id}/withdraw error = %v", err)
	}
	defer withdrawResponse.Body.Close()

	if withdrawResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for withdraw, got %d", withdrawResponse.StatusCode)
	}

	var withdrawnVault vault.Vault
	if err := json.NewDecoder(withdrawResponse.Body).Decode(&withdrawnVault); err != nil {
		t.Fatalf("decode withdraw response: %v", err)
	}

	if withdrawnVault.CurrentBalance.Cmp(decimal.RequireFromString("50.50")) != 0 {
		t.Fatalf("expected current_balance 50.50 after withdrawal, got %v", withdrawnVault.CurrentBalance)
	}

	// Test: Deposit with invalid amount (zero)
	invalidResponse, err := http.Post(
		server.URL+"/api/v1/vaults/"+created.ID.String()+"/deposit",
		"application/json",
		bytes.NewBufferString(`{"amount":"0","asset":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST invalid deposit error = %v", err)
	}
	defer invalidResponse.Body.Close()

	if invalidResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for zero deposit, got %d", invalidResponse.StatusCode)
	}

	// Test: Deposit with invalid amount (negative)
	invalidNegResponse, err := http.Post(
		server.URL+"/api/v1/vaults/"+created.ID.String()+"/deposit",
		"application/json",
		bytes.NewBufferString(`{"amount":"-50","asset":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST invalid negative deposit error = %v", err)
	}
	defer invalidNegResponse.Body.Close()

	if invalidNegResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for negative deposit, got %d", invalidNegResponse.StatusCode)
	}

	// Test: Deposit to non-existent vault
	notFoundResponse, err := http.Post(
		server.URL+"/api/v1/vaults/"+uuid.New().String()+"/deposit",
		"application/json",
		bytes.NewBufferString(`{"amount":"100","asset":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST to non-existent vault error = %v", err)
	}
	defer notFoundResponse.Body.Close()

	if notFoundResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent vault, got %d", notFoundResponse.StatusCode)
	}

	// Test: Unauthorized deposit (other user's vault)
	noop := service.NoopVaultDepositInvoker{}
	vaultService.SetDepositInvoker(noop)

	otherUserServer := httptest.NewServer(fakeAuthMiddleware(otherUserID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer otherUserServer.Close()

	forbiddenResponse, err := http.Post(
		otherUserServer.URL+"/api/v1/vaults/"+created.ID.String()+"/deposit",
		"application/json",
		bytes.NewBufferString(`{"amount":"100","asset":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST to other user vault error = %v", err)
	}
	defer forbiddenResponse.Body.Close()

	if forbiddenResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for other user's vault, got %d", forbiddenResponse.StatusCode)
	}
}
