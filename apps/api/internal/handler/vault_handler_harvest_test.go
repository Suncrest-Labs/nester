package handler

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/internal/ws"
)

func TestVaultHandlerHarvestReturnsZeroYield(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	vaultService.SetHarvestDefaultCompound(true)

	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	created, err := vaultService.CreateVault(t.Context(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	body := bytes.NewBufferString(`{}`)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/vaults/"+created.ID.String()+"/harvest", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST harvest error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	result := decodeAPIData[service.HarvestResult](t, response.Body)
	if result.GrossYieldUSDC != "0.000000" || result.NetYieldUSDC != "0.000000" {
		t.Fatalf("expected zero yield amounts, got gross=%s net=%s", result.GrossYieldUSDC, result.NetYieldUSDC)
	}
	if !result.Compounded {
		t.Fatal("expected default compound=true")
	}
}

func TestVaultHandlerHarvestForbiddenForOtherUser(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	repository := newHandlerRepository(ownerID, otherID)
	vaultService := service.NewVaultService(repository)
	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(otherID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	created, err := vaultService.CreateVault(t.Context(), service.CreateVaultInput{
		UserID:          ownerID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	body := bytes.NewBufferString(`{"compound":false}`)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/vaults/"+created.ID.String()+"/harvest", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST harvest error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", response.StatusCode)
	}
}

func TestVaultHandlerHarvestBroadcastsWebSocketEvent(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)

	handler := NewVaultHandler(vaultService)
	hub := ws.NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	handler.SetWSHub(hub)
	go hub.Run(t.Context())

	created, err := vaultService.CreateVault(t.Context(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault() error = %v", err)
	}

	model, ok := repository.vaults[created.ID]
	if !ok {
		t.Fatal("vault not found in repository")
	}
	model.YieldEarned = decimal.RequireFromString("50")
	model.CurrentBalance = decimal.RequireFromString("150")
	model.TotalDeposited = decimal.RequireFromString("100")
	repository.vaults[created.ID] = cloneHandlerVault(model)

	mux := http.NewServeMux()
	handler.Register(mux)
	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"compound":true}`)
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/vaults/"+created.ID.String()+"/harvest", body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST harvest error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	result := decodeAPIData[service.HarvestResult](t, response.Body)
	if result.GrossYieldUSDC != "50.000000" {
		t.Fatalf("expected gross yield 50.000000, got %s", result.GrossYieldUSDC)
	}
	if result.PerformanceFeeUSDC != "5.000000" {
		t.Fatalf("expected performance fee 5.000000, got %s", result.PerformanceFeeUSDC)
	}
	if result.NetYieldUSDC != "45.000000" {
		t.Fatalf("expected net yield 45.000000, got %s", result.NetYieldUSDC)
	}
}
