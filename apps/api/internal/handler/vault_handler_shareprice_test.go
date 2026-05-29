package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// sharePriceRepository is a minimal vault.Repository for share-price tests.
type sharePriceRepository struct {
	vaults map[uuid.UUID]vault.Vault
}

func newSharePriceRepo(vaults ...vault.Vault) *sharePriceRepository {
	m := make(map[uuid.UUID]vault.Vault, len(vaults))
	for _, v := range vaults {
		m[v.ID] = v
	}
	return &sharePriceRepository{vaults: m}
}

func (r *sharePriceRepository) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	v, ok := r.vaults[id]
	if !ok {
		return vault.Vault{}, vault.ErrVaultNotFound
	}
	return v, nil
}

// Stub out the rest of vault.Repository.
func (r *sharePriceRepository) CreateVault(_ context.Context, v vault.Vault) (vault.Vault, error) {
	return v, nil
}
func (r *sharePriceRepository) ListUserVaults(_ context.Context, _ uuid.UUID, _ vault.UserListFilter) ([]vault.Vault, int, error) {
	return nil, 0, nil
}
func (r *sharePriceRepository) RecordDeposit(_ context.Context, _ uuid.UUID, _ decimal.Decimal) error {
	return nil
}
func (r *sharePriceRepository) UpdateVaultBalances(_ context.Context, _ uuid.UUID, _ decimal.Decimal, _ decimal.Decimal) error {
	return nil
}
func (r *sharePriceRepository) ReplaceAllocations(_ context.Context, _ uuid.UUID, _ []vault.Allocation) error {
	return nil
}
func (r *sharePriceRepository) UpdateVault(_ context.Context, _ uuid.UUID, _ string, _ vault.VaultStatus) error {
	return nil
}
func (r *sharePriceRepository) RecordWithdrawal(_ context.Context, _ uuid.UUID, _ decimal.Decimal) error {
	return nil
}
func (r *sharePriceRepository) SoftDeleteVault(_ context.Context, _ uuid.UUID) error { return nil }
func (r *sharePriceRepository) ListDeposits(_ context.Context, _ uuid.UUID) ([]vault.VaultTransaction, error) {
	return nil, nil
}

// newSharePriceServer wires up a test server with the share-price handler.
func newSharePriceServer(repo vault.Repository) *httptest.Server {
	vaultSvc := service.NewVaultService(repo)
	spSvc := service.NewSharePriceService(repo)
	h := NewVaultHandler(vaultSvc, spSvc)
	mux := http.NewServeMux()
	h.Register(mux)
	return httptest.NewServer(
		fakeAuthMiddleware(uuid.New())(
			middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux),
		),
	)
}

func makeVault(totalDeposited, currentBalance string) vault.Vault {
	return vault.Vault{
		ID:              uuid.New(),
		UserID:          uuid.New(),
		ContractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		TotalDeposited:  decimal.RequireFromString(totalDeposited),
		CurrentBalance:  decimal.RequireFromString(currentBalance),
		Currency:        "USDC",
		Status:          vault.StatusActive,
		Allocations:     []vault.Allocation{},
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

// ── share-price tests ────────────────────────────────────────────────────────

func TestGetSharePrice_knownTVL(t *testing.T) {
	// 1000 shares deposited, 1050 USDC current balance → price = 1.05
	v := makeVault("1000", "1050")
	srv := newSharePriceServer(newSharePriceRepo(v))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/vaults/" + v.ID.String() + "/share-price")
	if err != nil {
		t.Fatalf("GET share-price: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	result := decodeAPIData[service.SharePriceResult](t, resp.Body)

	if !result.USDCPerShare.Equal(decimal.RequireFromString("1.05")) {
		t.Errorf("usdc_per_share: expected 1.05, got %s", result.USDCPerShare)
	}
	if !result.TotalShares.Equal(decimal.RequireFromString("1000")) {
		t.Errorf("total_shares: expected 1000, got %s", result.TotalShares)
	}
	if !result.TotalAssetsUSDC.Equal(decimal.RequireFromString("1050")) {
		t.Errorf("total_assets_usdc: expected 1050, got %s", result.TotalAssetsUSDC)
	}
	if result.VaultID != v.ID {
		t.Errorf("vault_id mismatch")
	}
}

func TestGetSharePrice_notFound(t *testing.T) {
	srv := newSharePriceServer(newSharePriceRepo())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/vaults/" + uuid.New().String() + "/share-price")
	if err != nil {
		t.Fatalf("GET share-price: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetSharePrice_invalidID(t *testing.T) {
	srv := newSharePriceServer(newSharePriceRepo())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/vaults/not-a-uuid/share-price")
	if err != nil {
		t.Fatalf("GET share-price: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ── convert tests ────────────────────────────────────────────────────────────

func TestConvert_sharesToUSDC(t *testing.T) {
	// 1000 shares, 1050 USDC → price 1.05; convert 100 shares → 105 USDC
	v := makeVault("1000", "1050")
	srv := newSharePriceServer(newSharePriceRepo(v))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/vaults/" + v.ID.String() + "/convert?shares=100")
	if err != nil {
		t.Fatalf("GET convert: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			OutputUSDC  string `json:"output_usdc"`
			InputShares string `json:"input_shares"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}

	got := decimal.RequireFromString(envelope.Data.OutputUSDC)
	want := decimal.RequireFromString("105")
	if !got.Equal(want) {
		t.Errorf("output_usdc: expected %s, got %s", want, got)
	}
}

func TestConvert_usdcToShares(t *testing.T) {
	// 1000 shares, 1050 USDC → price 1.05; convert 210 USDC → 200 shares
	v := makeVault("1000", "1050")
	srv := newSharePriceServer(newSharePriceRepo(v))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/vaults/" + v.ID.String() + "/convert?usdc=210")
	if err != nil {
		t.Fatalf("GET convert: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			OutputShares string `json:"output_shares"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}

	got := decimal.RequireFromString(envelope.Data.OutputShares)
	want := decimal.RequireFromString("200")
	if !got.Equal(want) {
		t.Errorf("output_shares: expected %s, got %s", want, got)
	}
}

func TestConvert_bothParamsFails(t *testing.T) {
	v := makeVault("1000", "1000")
	srv := newSharePriceServer(newSharePriceRepo(v))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/vaults/" + v.ID.String() + "/convert?shares=10&usdc=10")
	if err != nil {
		t.Fatalf("GET convert: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestConvert_noParamsFails(t *testing.T) {
	v := makeVault("1000", "1000")
	srv := newSharePriceServer(newSharePriceRepo(v))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/vaults/" + v.ID.String() + "/convert")
	if err != nil {
		t.Fatalf("GET convert: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestConvert_vaultNotFound(t *testing.T) {
	srv := newSharePriceServer(newSharePriceRepo())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/vaults/" + uuid.New().String() + "/convert?shares=10")
	if err != nil {
		t.Fatalf("GET convert: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}
