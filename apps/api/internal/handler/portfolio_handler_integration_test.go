package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/repository/postgres"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

func TestPortfolioHandlerSummaryIntegration(t *testing.T) {
	db := openHandlerIntegrationDB(t)
	applyHandlerIntegrationMigrations(t, db)
	resetHandlerIntegrationTables(t, db)

	userID := seedHandlerIntegrationUser(t, db)
	otherUserID := seedHandlerIntegrationUser(t, db)

	repository := postgres.NewVaultRepository(db)
	portfolioService := service.NewPortfolioService(repository)
	portfolioHandler := NewPortfolioHandler(portfolioService)
	mux := http.NewServeMux()
	portfolioHandler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	vaultService := service.NewVaultService(repository)

	vaultA, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault(vaultA) error = %v", err)
	}

	if _, err := vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
		VaultID: vaultA.ID,
		Amount:  decimal.RequireFromString("120.00"),
	}); err != nil {
		t.Fatalf("RecordDeposit(vaultA) error = %v", err)
	}

	if _, err := vaultService.UpdateAllocations(context.Background(), service.UpdateAllocationsInput{
		VaultID: vaultA.ID,
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("60.00"), APY: decimal.RequireFromString("4.00"), AllocatedAt: time.Now().UTC()},
			{Protocol: "compound", Amount: decimal.RequireFromString("60.00"), APY: decimal.RequireFromString("6.00"), AllocatedAt: time.Now().UTC().Add(time.Second)},
		},
	}); err != nil {
		t.Fatalf("UpdateAllocations(vaultA) error = %v", err)
	}

	vaultB, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          userID,
		ContractAddress: "CAABBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		Currency:        "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault(vaultB) error = %v", err)
	}

	if _, err := vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
		VaultID: vaultB.ID,
		Amount:  decimal.RequireFromString("230.50"),
	}); err != nil {
		t.Fatalf("RecordDeposit(vaultB) error = %v", err)
	}

	if _, err := vaultService.UpdateAllocations(context.Background(), service.UpdateAllocationsInput{
		VaultID: vaultB.ID,
		Allocations: []vault.Allocation{
			{Protocol: "angle", Amount: decimal.RequireFromString("100.00"), APY: decimal.RequireFromString("3.25"), AllocatedAt: time.Now().UTC()},
			{Protocol: "yield", Amount: decimal.RequireFromString("130.50"), APY: decimal.RequireFromString("5.10"), AllocatedAt: time.Now().UTC().Add(time.Second)},
		},
	}); err != nil {
		t.Fatalf("UpdateAllocations(vaultB) error = %v", err)
	}

	if _, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          otherUserID,
		ContractAddress: "CAXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
		Currency:        "USDC",
	}); err != nil {
		t.Fatalf("CreateVault(other user) error = %v", err)
	}

	response, err := http.Get(server.URL + "/api/v1/portfolio/summary")
	if err != nil {
		t.Fatalf("GET /api/v1/portfolio/summary error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for portfolio summary, got %d", response.StatusCode)
	}

	summary := decodeAPIData[portfolio.Summary](t, response.Body)

	if summary.TotalDepositedUSDC.Cmp(decimal.RequireFromString("350.50")) != 0 {
		t.Fatalf("expected total_deposited_usdc 350.50, got %s", summary.TotalDepositedUSDC)
	}
	if summary.TotalCurrentValueUSDC.Cmp(decimal.RequireFromString("350.50")) != 0 {
		t.Fatalf("expected total_current_value_usdc 350.50, got %s", summary.TotalCurrentValueUSDC)
	}
	if summary.TotalYieldEarnedUSDC.Cmp(decimal.Zero) != 0 {
		t.Fatalf("expected total_yield_earned_usdc 0.00, got %s", summary.TotalYieldEarnedUSDC)
	}
	if len(summary.Positions) != 2 {
		t.Fatalf("expected 2 positions, got %d", len(summary.Positions))
	}

	for _, position := range summary.Positions {
		if position.VaultID != vaultA.ID && position.VaultID != vaultB.ID {
			t.Fatalf("unexpected vault_id %s in portfolio positions", position.VaultID)
		}
		if position.VaultName == "" {
			t.Fatalf("expected vault_name to be set for vault %s", position.VaultID)
		}
	}
}

func TestPortfolioHandlerSummaryReturnsZeroForEmptyPortfolio(t *testing.T) {
	db := openHandlerIntegrationDB(t)
	applyHandlerIntegrationMigrations(t, db)
	resetHandlerIntegrationTables(t, db)

	userID := seedHandlerIntegrationUser(t, db)

	repository := postgres.NewVaultRepository(db)
	portfolioService := service.NewPortfolioService(repository)
	portfolioHandler := NewPortfolioHandler(portfolioService)
	mux := http.NewServeMux()
	portfolioHandler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/portfolio/summary")
	if err != nil {
		t.Fatalf("GET /api/v1/portfolio/summary error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for empty portfolio summary, got %d", response.StatusCode)
	}

	summary := decodeAPIData[portfolio.Summary](t, response.Body)

	if !summary.TotalDepositedUSDC.Equal(decimal.Zero) {
		t.Fatalf("expected total_deposited_usdc 0, got %s", summary.TotalDepositedUSDC)
	}
	if !summary.TotalCurrentValueUSDC.Equal(decimal.Zero) {
		t.Fatalf("expected total_current_value_usdc 0, got %s", summary.TotalCurrentValueUSDC)
	}
	if !summary.TotalYieldEarnedUSDC.Equal(decimal.Zero) {
		t.Fatalf("expected total_yield_earned_usdc 0, got %s", summary.TotalYieldEarnedUSDC)
	}
	if len(summary.Positions) != 0 {
		t.Fatalf("expected 0 positions for empty portfolio, got %d", len(summary.Positions))
	}
}
