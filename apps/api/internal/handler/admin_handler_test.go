package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	admindomain "github.com/suncrestlabs/nester/apps/api/internal/domain/admin"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/user"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/pkg/response"
)

type adminHandlerStubService struct {
	dashboard   service.DashboardResponse
	vaults      map[uuid.UUID]admindomain.VaultDetail
	settlements []admindomain.SettlementSummary
	users       []admindomain.UserSummary
	health      admindomain.DetailedHealth
}

func newAdminHandlerStubService(vaultID uuid.UUID) *adminHandlerStubService {
	now := time.Now().UTC()
	userID := uuid.New()

	detail := admindomain.VaultDetail{
		VaultSummary: admindomain.VaultSummary{
			ID:              vaultID,
			UserID:          userID,
			WalletAddress:   "GADMINWALLET",
			ContractAddress: "CVAULT001",
			TotalDeposited:  decimal.RequireFromString("1000.00"),
			CurrentBalance:  decimal.RequireFromString("1100.00"),
			Currency:        "USDC",
			Status:          vault.StatusActive,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("500"), APY: decimal.RequireFromString("4.2"), AllocatedAt: now},
		},
	}

	return &adminHandlerStubService{
		dashboard: service.DashboardResponse{
			TotalTVL:              "2450000.00",
			TotalUsers:            342,
			ActiveVaults:          518,
			TotalYieldDistributed: "45230.12",
			Settlements: admindomain.DashboardSettlementMetrics{
				Total:        1205,
				Pending:      12,
				Completed24h: 45,
				Failed24h:    2,
				Volume24h:    decimal.RequireFromString("125000.00"),
			},
			SystemHealth: admindomain.DashboardSystemHealth{
				Database:           "healthy",
				StellarRPC:         "healthy",
				SettlementProvider: "healthy",
				LastEventIndexed:   now.Format(time.RFC3339),
			},
		},
		vaults: map[uuid.UUID]admindomain.VaultDetail{vaultID: detail},
		settlements: []admindomain.SettlementSummary{
			{
				Settlement: offramp.Settlement{
					ID:           uuid.New(),
					UserID:       userID,
					VaultID:      vaultID,
					Amount:       decimal.RequireFromString("20.00"),
					Currency:     "USDC",
					FiatCurrency: "USD",
					FiatAmount:   decimal.RequireFromString("20.00"),
					ExchangeRate: decimal.RequireFromString("1.00"),
					Destination:  offramp.Destination{Type: "bank_transfer", Provider: "bank", AccountNumber: "123", AccountName: "Test", BankCode: "000"},
					Status:       offramp.StatusInitiated,
					CreatedAt:    now,
				},
				WalletAddress: "GADMINWALLET",
			},
		},
		users: []admindomain.UserSummary{
			{
				ID:             userID,
				WalletAddress:  "GADMINWALLET",
				DisplayName:    "Admin User",
				KYCStatus:      user.KYCStatusVerified,
				VaultCount:     1,
				TotalDeposited: decimal.RequireFromString("1000.00"),
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		},
		health: admindomain.DetailedHealth{
			Database:           admindomain.HealthStatus{Status: "healthy", LastCheckedAt: now},
			StellarRPC:         admindomain.HealthStatus{Status: "healthy", LastCheckedAt: now},
			SettlementProvider: admindomain.HealthStatus{Status: "healthy", LastCheckedAt: now},
			EventIndexer:       admindomain.HealthStatus{Status: "healthy", LastCheckedAt: now, LastEventAt: &now},
			DiskUsage:          "40.0%",
			Uptime:             "5m0s",
		},
	}
}

func (s *adminHandlerStubService) GetDashboard(context.Context) (service.DashboardResponse, error) {
	return s.dashboard, nil
}

func (s *adminHandlerStubService) ListVaults(context.Context, admindomain.VaultListFilter) ([]admindomain.VaultSummary, int, error) {
	out := make([]admindomain.VaultSummary, 0, len(s.vaults))
	for _, detail := range s.vaults {
		out = append(out, detail.VaultSummary)
	}
	return out, len(out), nil
}

func (s *adminHandlerStubService) GetVaultDetail(_ context.Context, id uuid.UUID) (admindomain.VaultDetail, error) {
	detail, ok := s.vaults[id]
	if !ok {
		return admindomain.VaultDetail{}, vault.ErrVaultNotFound
	}
	return detail, nil
}

func (s *adminHandlerStubService) PauseVault(_ context.Context, id uuid.UUID) (admindomain.VaultDetail, error) {
	detail, ok := s.vaults[id]
	if !ok {
		return admindomain.VaultDetail{}, vault.ErrVaultNotFound
	}
	detail.Status = vault.StatusPaused
	detail.UpdatedAt = time.Now().UTC()
	s.vaults[id] = detail
	return detail, nil
}

func (s *adminHandlerStubService) UnpauseVault(_ context.Context, id uuid.UUID) (admindomain.VaultDetail, error) {
	detail, ok := s.vaults[id]
	if !ok {
		return admindomain.VaultDetail{}, vault.ErrVaultNotFound
	}
	detail.Status = vault.StatusActive
	detail.UpdatedAt = time.Now().UTC()
	s.vaults[id] = detail
	return detail, nil
}

func (s *adminHandlerStubService) ListSettlements(context.Context, admindomain.SettlementListFilter) ([]admindomain.SettlementSummary, int, error) {
	return s.settlements, len(s.settlements), nil
}

func (s *adminHandlerStubService) ListUsers(context.Context, admindomain.UserListFilter) ([]admindomain.UserSummary, int, error) {
	return s.users, len(s.users), nil
}

func (s *adminHandlerStubService) GetDetailedHealth(context.Context) (admindomain.DetailedHealth, error) {
	return s.health, nil
}

func TestAdminHandlerListPauseVerifyFlow(t *testing.T) {
	vaultID := uuid.New()
	svc := newAdminHandlerStubService(vaultID)

	h := NewAdminHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	listResp, err := http.Get(server.URL + "/api/v1/admin/vaults?page=1&per_page=50")
	if err != nil {
		t.Fatalf("GET /admin/vaults error = %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", listResp.StatusCode)
	}

	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Meta    *response.Meta  `json:"meta"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if envelope.Meta == nil || envelope.Meta.TotalCount != 1 {
		t.Fatalf("meta total_count = %+v, want 1", envelope.Meta)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/admin/vaults/"+vaultID.String()+"/pause", nil)
	if err != nil {
		t.Fatalf("build pause request: %v", err)
	}
	pauseResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST pause error = %v", err)
	}
	defer pauseResp.Body.Close()
	if pauseResp.StatusCode != http.StatusOK {
		t.Fatalf("pause status = %d, want 200", pauseResp.StatusCode)
	}

	detailResp, err := http.Get(server.URL + "/api/v1/admin/vaults/" + vaultID.String())
	if err != nil {
		t.Fatalf("GET /admin/vaults/{id} error = %v", err)
	}
	defer detailResp.Body.Close()
	if detailResp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", detailResp.StatusCode)
	}
	detail := decodeAPIData[admindomain.VaultDetail](t, detailResp.Body)
	if detail.Status != vault.StatusPaused {
		t.Fatalf("vault status = %q, want paused", detail.Status)
	}
}

func TestAdminHandlerDateFilterValidation(t *testing.T) {
	vaultID := uuid.New()
	h := NewAdminHandler(newAdminHandlerStubService(vaultID))
	mux := http.NewServeMux()
	h.Register(mux)

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/admin/settlements?date_from=not-a-date")
	if err != nil {
		t.Fatalf("GET settlements error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdminHandlerAuthListPauseVerify(t *testing.T) {
	vaultID := uuid.New()
	svc := newAdminHandlerStubService(vaultID)
	h := NewAdminHandler(svc)

	mux := http.NewServeMux()
	h.Register(mux)

	rules := []middleware.RouteRule{
		{PathPrefix: "/api/v1/admin/", Role: "admin"},
	}
	protected := middleware.Authenticate("admin-test-secret", rules)(mux)
	server := httptest.NewServer(protected)
	defer server.Close()

	nonAdminToken := makeAdminToken(t, "admin-test-secret", []string{"operator"})
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/vaults", nil)
	req.Header.Set("Authorization", "Bearer "+nonAdminToken)
	nonAdminResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET as non-admin failed: %v", err)
	}
	defer nonAdminResp.Body.Close()
	if nonAdminResp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403", nonAdminResp.StatusCode)
	}

	adminToken := makeAdminToken(t, "admin-test-secret", []string{"admin"})
	adminListReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/vaults", nil)
	adminListReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminListResp, err := http.DefaultClient.Do(adminListReq)
	if err != nil {
		t.Fatalf("GET as admin failed: %v", err)
	}
	defer adminListResp.Body.Close()
	if adminListResp.StatusCode != http.StatusOK {
		t.Fatalf("admin list status = %d, want 200", adminListResp.StatusCode)
	}

	adminPauseReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/admin/vaults/"+vaultID.String()+"/pause", nil)
	adminPauseReq.Header.Set("Authorization", "Bearer "+adminToken)
	adminPauseResp, err := http.DefaultClient.Do(adminPauseReq)
	if err != nil {
		t.Fatalf("POST pause as admin failed: %v", err)
	}
	defer adminPauseResp.Body.Close()
	if adminPauseResp.StatusCode != http.StatusOK {
		t.Fatalf("admin pause status = %d, want 200", adminPauseResp.StatusCode)
	}

	detailReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/admin/vaults/"+vaultID.String(), nil)
	detailReq.Header.Set("Authorization", "Bearer "+adminToken)
	detailResp, err := http.DefaultClient.Do(detailReq)
	if err != nil {
		t.Fatalf("GET detail as admin failed: %v", err)
	}
	defer detailResp.Body.Close()

	detail := decodeAPIData[admindomain.VaultDetail](t, detailResp.Body)
	if detail.Status != vault.StatusPaused {
		t.Fatalf("vault status after pause = %q, want paused", detail.Status)
	}
}

func makeAdminToken(t *testing.T, secret string, roles []string) string {
	t.Helper()
	signed, err := auth.MakeJWT(auth.Claims{
		Subject:   "admin-user",
		Roles:     roles,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}, secret)
	if err != nil {
		t.Fatalf("make signed token: %v", err)
	}
	return signed
}

type adminErrStub struct{}

func (adminErrStub) GetDashboard(context.Context) (service.DashboardResponse, error) {
	return service.DashboardResponse{}, errors.New("boom")
}
func (adminErrStub) ListVaults(context.Context, admindomain.VaultListFilter) ([]admindomain.VaultSummary, int, error) {
	return nil, 0, service.ErrInvalidAdminInput
}
func (adminErrStub) GetVaultDetail(context.Context, uuid.UUID) (admindomain.VaultDetail, error) {
	return admindomain.VaultDetail{}, vault.ErrVaultNotFound
}
func (adminErrStub) PauseVault(context.Context, uuid.UUID) (admindomain.VaultDetail, error) {
	return admindomain.VaultDetail{}, nil
}
func (adminErrStub) UnpauseVault(context.Context, uuid.UUID) (admindomain.VaultDetail, error) {
	return admindomain.VaultDetail{}, nil
}
func (adminErrStub) ListSettlements(context.Context, admindomain.SettlementListFilter) ([]admindomain.SettlementSummary, int, error) {
	return nil, 0, nil
}
func (adminErrStub) ListUsers(context.Context, admindomain.UserListFilter) ([]admindomain.UserSummary, int, error) {
	return nil, 0, nil
}
func (adminErrStub) GetDetailedHealth(context.Context) (admindomain.DetailedHealth, error) {
	return admindomain.DetailedHealth{}, nil
}
