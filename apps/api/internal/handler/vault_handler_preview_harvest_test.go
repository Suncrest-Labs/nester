package handler

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

func TestVaultHandlerPreviewHarvestNormalCase(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
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

	model := repository.vaults[created.ID]
	model.YieldEarned = decimal.RequireFromString("12.5")
	model.CurrentBalance = decimal.RequireFromString("112.5")
	model.TotalDeposited = decimal.RequireFromString("100")
	repository.vaults[created.ID] = cloneHandlerVault(model)

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/harvest/preview")
	if err != nil {
		t.Fatalf("GET harvest/preview error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	preview := decodeAPIData[service.HarvestPreview](t, resp.Body)

	if preview.GrossYieldUSDC != "12.500000" {
		t.Fatalf("gross_yield_usdc = %s, want 12.500000", preview.GrossYieldUSDC)
	}
	if preview.PerformanceFeeUSDC != "1.250000" {
		t.Fatalf("performance_fee_usdc = %s, want 1.250000", preview.PerformanceFeeUSDC)
	}
	if preview.NetYieldUSDC != "11.250000" {
		t.Fatalf("net_yield_usdc = %s, want 11.250000", preview.NetYieldUSDC)
	}
	if preview.PerformanceFeeBPS != 1000 {
		t.Fatalf("performance_fee_bps = %d, want 1000", preview.PerformanceFeeBPS)
	}
}

func TestVaultHandlerPreviewHarvestImpairedCase(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
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

	model := repository.vaults[created.ID]
	model.CurrentBalance = decimal.RequireFromString("80")
	model.TotalDeposited = decimal.RequireFromString("100")
	repository.vaults[created.ID] = cloneHandlerVault(model)

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/harvest/preview")
	if err != nil {
		t.Fatalf("GET harvest/preview error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	preview := decodeAPIData[service.HarvestPreview](t, resp.Body)

	if preview.GrossYieldUSDC != "0.000000" {
		t.Fatalf("gross_yield_usdc = %s, want 0.000000", preview.GrossYieldUSDC)
	}
	if preview.PerformanceFeeUSDC != "0.000000" {
		t.Fatalf("performance_fee_usdc = %s, want 0.000000", preview.PerformanceFeeUSDC)
	}
	if !preview.Impaired {
		t.Fatal("expected impaired=true")
	}
}

func TestVaultHandlerPreviewHarvestCompoundParam(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
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

	model := repository.vaults[created.ID]
	model.YieldEarned = decimal.RequireFromString("10")
	model.CurrentBalance = decimal.RequireFromString("110")
	model.TotalDeposited = decimal.RequireFromString("100")
	repository.vaults[created.ID] = cloneHandlerVault(model)

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/harvest/preview?compound=true")
	if err != nil {
		t.Fatalf("GET harvest/preview?compound=true error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	preview := decodeAPIData[service.HarvestPreview](t, resp.Body)

	if !preview.Compounded {
		t.Fatal("expected compounded=true")
	}
	if preview.EstimatedNewShares == "" {
		t.Fatal("expected estimated_new_shares when compound=true")
	}
}

func TestVaultHandlerPreviewHarvestForbiddenForOtherUser(t *testing.T) {
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

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String() + "/harvest/preview")
	if err != nil {
		t.Fatalf("GET harvest/preview error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", resp.StatusCode)
	}
}
