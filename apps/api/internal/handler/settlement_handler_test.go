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

	"github.com/suncrestlabs/nester/apps/api/internal/domain/offramp"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type settlementStubRepo struct {
	data map[uuid.UUID]*offramp.Settlement
}

func newSettlementStubRepo() *settlementStubRepo {
	return &settlementStubRepo{data: make(map[uuid.UUID]*offramp.Settlement)}
}

func (r *settlementStubRepo) Create(_ context.Context, model offramp.Settlement) (offramp.Settlement, error) {
	model.CreatedAt = time.Now().UTC()
	cp := model
	r.data[model.ID] = &cp
	return cp, nil
}

func (r *settlementStubRepo) GetByID(_ context.Context, id uuid.UUID) (offramp.Settlement, error) {
	s, ok := r.data[id]
	if !ok {
		return offramp.Settlement{}, offramp.ErrSettlementNotFound
	}
	return *s, nil
}

func (r *settlementStubRepo) GetByUserID(_ context.Context, userID uuid.UUID, filter offramp.SettlementStatus) ([]offramp.Settlement, error) {
	var out []offramp.Settlement
	for _, s := range r.data {
		if s.UserID != userID {
			continue
		}
		if filter != "" && s.Status != filter {
			continue
		}
		out = append(out, *s)
	}
	return out, nil
}

func (r *settlementStubRepo) UpdateStatus(_ context.Context, id uuid.UUID, status offramp.SettlementStatus, completedAt *time.Time) error {
	s, ok := r.data[id]
	if !ok {
		return offramp.ErrSettlementNotFound
	}
	s.Status = status
	s.CompletedAt = completedAt
	return nil
}

func validSettlementJSONBody(userID, vaultID uuid.UUID) string {
	return `{
		"user_id":"` + userID.String() + `",
		"vault_id":"` + vaultID.String() + `",
		"amount":"100.00",
		"currency":"USDC",
		"fiat_currency":"NGN",
		"fiat_amount":"150000.00",
		"exchange_rate":"1500.00",
		"destination":{
			"type":"bank_transfer",
			"provider":"bank",
			"account_number":"0123456789",
			"account_name":"Test User",
			"bank_code":"058"
		}
	}`
}

func TestSettlementHandler_PostCreates201(t *testing.T) {
	userID := uuid.New()
	vaultID := uuid.New()
	svc := service.NewSettlementService(newSettlementStubRepo())
	h := NewSettlementHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux))
	defer server.Close()

	resp, err := http.Post(
		server.URL+"/api/v1/settlements",
		"application/json",
		bytes.NewBufferString(validSettlementJSONBody(userID, vaultID)),
	)
	if err != nil {
		t.Fatalf("POST settlements: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}
	settlement := decodeAPIData[offramp.Settlement](t, resp.Body)
	if settlement.Status != offramp.StatusInitiated {
		t.Fatalf("want initiated, got %s", settlement.Status)
	}
}

func TestSettlementHandler_PostInvalidBody400(t *testing.T) {
	svc := service.NewSettlementService(newSettlementStubRepo())
	h := NewSettlementHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/settlements", "application/json", bytes.NewBufferString(`{`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

func TestSettlementHandler_PostDomainValidation400(t *testing.T) {
	userID := uuid.New()
	vaultID := uuid.New()
	svc := service.NewSettlementService(newSettlementStubRepo())
	h := NewSettlementHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	// bank_transfer without bank_code
	bad := `{
		"user_id":"` + userID.String() + `",
		"vault_id":"` + vaultID.String() + `",
		"amount":"100.00",
		"currency":"USDC",
		"fiat_currency":"NGN",
		"fiat_amount":"150000.00",
		"exchange_rate":"1500.00",
		"destination":{"type":"bank_transfer","provider":"bank","account_number":"1","account_name":"x","bank_code":""}
	}`
	resp, err := http.Post(server.URL+"/api/v1/settlements", "application/json", bytes.NewBufferString(bad))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for invalid settlement, got %d", resp.StatusCode)
	}
}

func TestSettlementHandler_Get200And404(t *testing.T) {
	userID := uuid.New()
	vaultID := uuid.New()
	repo := newSettlementStubRepo()
	svc := service.NewSettlementService(repo)
	created, err := svc.InitiateSettlement(context.Background(), service.InitiateSettlementInput{
		UserID:       userID,
		VaultID:      vaultID,
		Amount:       decimal.RequireFromString("10"),
		Currency:     "USDC",
		FiatCurrency: "NGN",
		FiatAmount:   decimal.RequireFromString("100"),
		ExchangeRate: decimal.RequireFromString("10"),
		Destination: offramp.Destination{
			Type:          "mobile_money",
			Provider:      "mpesa",
			AccountNumber: "0712345678",
			AccountName:   "Jane",
		},
	})
	if err != nil {
		t.Fatalf("InitiateSettlement: %v", err)
	}

	h := NewSettlementHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/settlements/" + created.ID.String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	resp404, err := http.Get(server.URL + "/api/v1/settlements/" + uuid.New().String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp404.Body.Close()
	if resp404.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp404.StatusCode)
	}
}

func TestSettlementHandler_PatchStatus200(t *testing.T) {
	userID := uuid.New()
	vaultID := uuid.New()
	svc := service.NewSettlementService(newSettlementStubRepo())
	created, err := svc.InitiateSettlement(context.Background(), service.InitiateSettlementInput{
		UserID:       userID,
		VaultID:      vaultID,
		Amount:       decimal.RequireFromString("10"),
		Currency:     "USDC",
		FiatCurrency: "NGN",
		FiatAmount:   decimal.RequireFromString("100"),
		ExchangeRate: decimal.RequireFromString("10"),
		Destination: offramp.Destination{
			Type:          "mobile_money",
			Provider:      "mpesa",
			AccountNumber: "0712345678",
			AccountName:   "Jane",
		},
	})
	if err != nil {
		t.Fatalf("InitiateSettlement: %v", err)
	}

	h := NewSettlementHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body := bytes.NewBufferString(`{"status":"liquidity_matched"}`)
	req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/v1/settlements/"+created.ID.String()+"/status", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	out := decodeAPIData[offramp.Settlement](t, resp.Body)
	if out.Status != offramp.StatusLiquidityMatched {
		t.Fatalf("want liquidity_matched, got %s", out.Status)
	}
}

func TestSettlementHandler_ListUserSettlementsWithStatus(t *testing.T) {
	userID := uuid.New()
	vaultID := uuid.New()
	svc := service.NewSettlementService(newSettlementStubRepo())
	h := NewSettlementHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := http.Post(server.URL+"/api/v1/settlements", "application/json", bytes.NewBufferString(validSettlementJSONBody(userID, vaultID)))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/users/" + userID.String() + "/settlements?status=initiated")
	if err != nil {
		t.Fatalf("GET list: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	list := decodeAPIData[[]offramp.Settlement](t, resp.Body)
	if len(list) != 1 {
		t.Fatalf("want 1 settlement, got %d", len(list))
	}
}
