package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/bank"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// ---------------------------------------------------------------------------
// Stub resolvers for handler tests
// ---------------------------------------------------------------------------

type stubResolver struct {
	banks      []bank.Bank
	listErr    error
	info       *bank.AccountInfo
	resolveErr error
}

func (s *stubResolver) ListBanks(_ context.Context, _ string) ([]bank.Bank, error) {
	return s.banks, s.listErr
}

func (s *stubResolver) ResolveAccount(_ context.Context, _, _ string) (*bank.AccountInfo, error) {
	return s.info, s.resolveErr
}

func newTestHandler(primary, fallback bank.BankResolver) *handler.BankHandler {
	svc := service.NewBankService(primary, fallback)
	return handler.NewBankHandler(svc)
}

// ---------------------------------------------------------------------------
// GET /api/v1/banks
// ---------------------------------------------------------------------------

func TestListBanksHandler_Returns200WithBanks(t *testing.T) {
	banks := []bank.Bank{
		{Name: "Guaranty Trust Bank", Code: "058", Country: "NG"},
		{Name: "Zenith Bank", Code: "057", Country: "NG"},
	}
	h := newTestHandler(&stubResolver{banks: banks}, &stubResolver{})

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/banks?country=NG", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["success"] != true {
		t.Errorf("expected success=true")
	}
}

func TestListBanksHandler_DefaultsToNG(t *testing.T) {
	banks := []bank.Bank{{Name: "GTB", Code: "058", Country: "NG"}}
	h := newTestHandler(&stubResolver{banks: banks}, &stubResolver{})

	mux := http.NewServeMux()
	h.Register(mux)

	// No country param — should default to NG.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/banks", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestListBanksHandler_Returns400ForUnsupportedCountry(t *testing.T) {
	h := newTestHandler(&stubResolver{}, &stubResolver{})

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/banks?country=US", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported country, got %d", rec.Code)
	}
}

func TestListBanksHandler_Returns503WhenProviderUnavailable(t *testing.T) {
	primary := &stubResolver{listErr: errors.New("down")}
	fallback := &stubResolver{listErr: errors.New("down")}
	h := newTestHandler(primary, fallback)

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/banks?country=NG", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/banks/resolve
// ---------------------------------------------------------------------------

func TestResolveHandler_Returns200OnSuccess(t *testing.T) {
	info := &bank.AccountInfo{
		AccountNumber: "0123456789",
		AccountName:   "JOHN DOE",
		BankCode:      "058",
		BankName:      "GTB",
	}
	h := newTestHandler(&stubResolver{info: info}, &stubResolver{})

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/banks/resolve?account_number=0123456789&bank_code=058&country=NG", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["success"] != true {
		t.Errorf("expected success=true")
	}
}

func TestResolveHandler_Returns400WhenAccountNumberMissing(t *testing.T) {
	h := newTestHandler(&stubResolver{}, &stubResolver{})

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/banks/resolve?bank_code=058", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestResolveHandler_Returns400WhenBankCodeMissing(t *testing.T) {
	h := newTestHandler(&stubResolver{}, &stubResolver{})

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/banks/resolve?account_number=0123456789", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestResolveHandler_Returns404WhenAccountNotFound(t *testing.T) {
	primary := &stubResolver{resolveErr: bank.ErrAccountNotFound}
	fallback := &stubResolver{resolveErr: bank.ErrAccountNotFound}
	h := newTestHandler(primary, fallback)

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/banks/resolve?account_number=0123456789&bank_code=058&country=NG", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errBody, _ := resp["error"].(map[string]any)
	if errBody["code"] != "ACCOUNT_NOT_FOUND" {
		t.Errorf("expected ACCOUNT_NOT_FOUND error code, got %v", errBody["code"])
	}
}

func TestResolveHandler_Returns503WhenBothProvidersFail(t *testing.T) {
	primary := &stubResolver{resolveErr: errors.New("down")}
	fallback := &stubResolver{resolveErr: errors.New("down")}
	h := newTestHandler(primary, fallback)

	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/banks/resolve?account_number=0123456789&bank_code=058&country=NG", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	errBody, _ := resp["error"].(map[string]any)
	if errBody["code"] != "PROVIDER_UNAVAILABLE" {
		t.Errorf("expected PROVIDER_UNAVAILABLE, got %v", errBody["code"])
	}
}

func TestResolveHandler_Returns400ForInvalidAccountNumber(t *testing.T) {
	h := newTestHandler(&stubResolver{}, &stubResolver{})

	mux := http.NewServeMux()
	h.Register(mux)

	// 9 digits — too short.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/banks/resolve?account_number=012345678&bank_code=058&country=NG", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short account number, got %d", rec.Code)
	}
}
