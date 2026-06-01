package handler

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

func TestVaultHandlerGetMyPositionEmpty(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	created, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	response, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/my-position")
	if err != nil {
		t.Fatalf("GET my-position error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	position := decodeAPIData[vault.UserVaultPosition](t, response.Body)
	if position.TotalDepositedUSDC != "0.000000" {
		t.Fatalf("expected zero deposited, got %s", position.TotalDepositedUSDC)
	}
}

func TestVaultHandlerGetMyPositionWithYield(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	created, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	if _, err := vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
		VaultID: created.ID,
		UserID:  userID,
		Amount:  decimal.RequireFromString("1000"),
	}); err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	if err := repository.UpdateVaultBalances(context.Background(), created.ID,
		decimal.RequireFromString("1000"),
		decimal.RequireFromString("1089.45"),
	); err != nil {
		t.Fatalf("UpdateVaultBalances() error = %v", err)
	}

	response, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/my-position")
	if err != nil {
		t.Fatalf("GET my-position error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	position := decodeAPIData[vault.UserVaultPosition](t, response.Body)
	if !strings.HasPrefix(position.UnrealizedPnLUSDC, "+") {
		t.Fatalf("expected positive pnl, got %s", position.UnrealizedPnLUSDC)
	}
}

func TestVaultHandlerGetMyPositionUnauthorized(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	handler := NewVaultHandler(service.NewVaultService(repository))

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	created, err := service.NewVaultService(repository).CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	response, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/my-position")
	if err != nil {
		t.Fatalf("GET my-position error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.StatusCode)
	}
}

func TestVaultHandlerGetMyPositionInvalidVaultID(t *testing.T) {
	userID := uuid.New()
	handler := NewVaultHandler(service.NewVaultService(newHandlerRepository(userID)))

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/vaults/not-a-uuid/my-position")
	if err != nil {
		t.Fatalf("GET my-position error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
}

func TestVaultHandlerGetMyPositionVaultNotFound(t *testing.T) {
	userID := uuid.New()
	handler := NewVaultHandler(service.NewVaultService(newHandlerRepository(userID)))

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/vaults/" + uuid.New().String() + "/my-position")
	if err != nil {
		t.Fatalf("GET my-position error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", response.StatusCode)
	}
}

func TestVaultHandlerGetMyPositionNoAuthBodyRequired(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	created, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/vaults/"+created.ID.String()+"/my-position", bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET my-position error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
}
