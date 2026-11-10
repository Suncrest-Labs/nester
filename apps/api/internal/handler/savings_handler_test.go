package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/savings"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type stubSavingsChain struct{}

func (stubSavingsChain) State(context.Context) (savings.ChainState, error) {
	return savings.ChainState{
		SharePrice:  decimal.NewFromInt(10_000_000),
		TotalAssets: decimal.NewFromInt(50_000_000_000),
		FeeBps:      1000,
		Timelock:    72 * time.Hour,
	}, nil
}

func (stubSavingsChain) Balance(context.Context, string) (decimal.Decimal, error) {
	return decimal.NewFromInt(10_000_000), nil
}

func (stubSavingsChain) ConvertToAssets(_ context.Context, s decimal.Decimal) (decimal.Decimal, error) {
	return s, nil
}

type emptySavingsRepo struct{}

func (emptySavingsRepo) Insert(context.Context, savings.Snapshot) error { return nil }
func (emptySavingsRepo) Since(context.Context, time.Time) ([]savings.Snapshot, error) {
	return nil, nil
}
func (emptySavingsRepo) Latest(context.Context) (savings.Snapshot, error) {
	return savings.Snapshot{}, savings.ErrSnapshotNotFound
}

func newSavingsTestMux(configured bool) *http.ServeMux {
	var svc *service.SavingsService
	if configured {
		svc = service.NewSavingsService(stubSavingsChain{}, emptySavingsRepo{})
	}
	mux := http.NewServeMux()
	NewSavingsHandler(svc).Register(mux)
	return mux
}

func savingsGet(mux *http.ServeMux, path string, user *auth.User) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if user != nil {
		req = req.WithContext(auth.NewContext(req.Context(), *user))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func savingsData(t *testing.T, rec *httptest.ResponseRecorder) map[string]json.RawMessage {
	t.Helper()
	var body struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}

func TestSavingsHandler_Summary_NullRatesOnFreshDeploy(t *testing.T) {
	rec := savingsGet(newSavingsTestMux(true), "/api/v1/savings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	data := savingsData(t, rec)
	for _, k := range []string{"rate7d", "rate30d"} {
		v, ok := data[k]
		if !ok || string(v) != "null" {
			t.Errorf("%s = %s (present=%v), want null", k, v, ok)
		}
	}
	if string(data["sharePrice"]) != `"1"` || string(data["totalAssets"]) != `"5000"` {
		t.Errorf("sharePrice/totalAssets = %s/%s", data["sharePrice"], data["totalAssets"])
	}
}

func TestSavingsHandler_History_RejectsBadPeriod(t *testing.T) {
	mux := newSavingsTestMux(true)
	for _, p := range []string{"", "1y", "7"} {
		rec := savingsGet(mux, "/api/v1/savings/history?period="+p, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("period %q: status = %d, want 400", p, rec.Code)
		}
	}
	if rec := savingsGet(mux, "/api/v1/savings/history?period=30d", nil); rec.Code != http.StatusOK {
		t.Errorf("30d: status = %d", rec.Code)
	}
}

func TestSavingsHandler_Position_RequiresAuth(t *testing.T) {
	rec := savingsGet(newSavingsTestMux(true), "/api/v1/savings/position", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSavingsHandler_Position_ReturnsHolding(t *testing.T) {
	user := &auth.User{ID: "u1", WalletAddress: "GWALLET"}
	rec := savingsGet(newSavingsTestMux(true), "/api/v1/savings/position", user)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	data := savingsData(t, rec)
	if string(data["shares"]) != `"1"` || string(data["deposited"]) != "null" {
		t.Errorf("data = %s", rec.Body)
	}
}

func TestSavingsHandler_Position_RequiresWallet(t *testing.T) {
	rec := savingsGet(newSavingsTestMux(true), "/api/v1/savings/position", &auth.User{ID: "u1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestSavingsHandler_Unconfigured_Returns503(t *testing.T) {
	mux := newSavingsTestMux(false)
	user := &auth.User{ID: "u1", WalletAddress: "GWALLET"}
	for _, p := range []string{"/api/v1/savings", "/api/v1/savings/history?period=7d", "/api/v1/savings/position"} {
		if rec := savingsGet(mux, p, user); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status = %d, want 503", p, rec.Code)
		}
	}
}
