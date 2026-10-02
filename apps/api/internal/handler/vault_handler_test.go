package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/auth"
	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// fakeAuthMiddleware injects an auth.User into the request context for testing.
func fakeAuthMiddleware(userID uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.NewContext(r.Context(), auth.User{ID: userID.String()})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// decodeAPIData unwraps the API envelope {"success":true,"data":...} and decodes
// the inner data field into T.
func decodeAPIData[T any](t *testing.T, body io.Reader) T {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var result T
	if err := json.Unmarshal(envelope.Data, &result); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	return result
}

func TestVaultHandlerCreateGetAndList(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	repository := newHandlerRepository(userID, otherUserID)
	vaultService := service.NewVaultService(repository)

	handler := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(userID)(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

	body := bytes.NewBufferString(`{"contract_address":"CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","currency":"USDC"}`)
	response, err := http.Post(server.URL+"/api/v1/vaults", "application/json", body)
	if err != nil {
		t.Fatalf("POST /api/v1/vaults error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	created := decodeAPIData[vault.Vault](t, response.Body)

	if _, err := vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
		VaultID: created.ID,
		Amount:  decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordDeposit() error = %v", err)
	}

	if _, err := vaultService.UpdateAllocations(context.Background(), service.UpdateAllocationsInput{
		VaultID: created.ID,
		Allocations: []vault.Allocation{
			{Protocol: "aave", Amount: decimal.RequireFromString("40"), APY: decimal.RequireFromString("4.1")},
			{Protocol: "blend", Amount: decimal.RequireFromString("60"), APY: decimal.RequireFromString("5.2")},
		},
	}); err != nil {
		t.Fatalf("UpdateAllocations() error = %v", err)
	}

	getResponse, err := http.Get(server.URL + "/api/v1/vaults/" + created.ID.String())
	if err != nil {
		t.Fatalf("GET /api/v1/vaults/{id} error = %v", err)
	}
	defer getResponse.Body.Close()

	fetched := decodeAPIData[vault.Vault](t, getResponse.Body)

	if len(fetched.Allocations) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(fetched.Allocations))
	}
	if !fetched.CurrentBalance.Equal(decimal.RequireFromString("100")) {
		t.Fatalf("expected current balance 100, got %s", fetched.CurrentBalance)
	}

	if _, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID:          otherUserID,
		ContractAddress: "CA-002",
		Currency:        "USDC",
	}); err != nil {
		t.Fatalf("CreateVault(other user) error = %v", err)
	}

	// Note: fakeAuthMiddleware uses userID, so the auth check in listUserVaults will pass
	listResponse, err := http.Get(server.URL + "/api/v1/vaults?userId=" + userID.String())
	if err != nil {
		t.Fatalf("GET GET /api/v1/vaults?userId={userId} error = %v", err)
	}
	defer listResponse.Body.Close()

	vaults := decodeAPIData[[]vault.Vault](t, listResponse.Body)

	if len(vaults) != 1 {
		t.Fatalf("expected 1 vault for user, got %d", len(vaults))
	}
}

func TestVaultHandlerNotFoundAndInvalidUser(t *testing.T) {
	repository := newHandlerRepository(uuid.New())
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	server := httptest.NewServer(fakeAuthMiddleware(uuid.New())(middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux)))
	defer server.Close()

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

type handlerRepository struct {
	users        map[uuid.UUID]struct{}
	vaults       map[uuid.UUID]vault.Vault
	transactions []vault.VaultTransaction
}

func newHandlerRepository(userIDs ...uuid.UUID) *handlerRepository {
	users := make(map[uuid.UUID]struct{}, len(userIDs))
	for _, userID := range userIDs {
		users[userID] = struct{}{}
	}
	return &handlerRepository{
		users:        users,
		vaults:       make(map[uuid.UUID]vault.Vault),
		transactions: make([]vault.VaultTransaction, 0),
	}
}

func (r *handlerRepository) CreateVault(_ context.Context, model vault.Vault) (vault.Vault, error) {
	if _, ok := r.users[model.UserID]; !ok {
		return vault.Vault{}, vault.ErrUserNotFound
	}
	// Mirrors uq_vaults_contract_address_live (migration 104), scoped to live
	// rows exactly as the partial index is, so the handler's conflict branch
	// is exercised against the same rule the database enforces (#1148).
	for _, existing := range r.vaults {
		if existing.DeletedAt == nil && existing.ContractAddress == model.ContractAddress {
			return vault.Vault{}, vault.ErrContractAddressRegistered
		}
	}
	now := time.Now().UTC()
	model.CreatedAt = now
	model.UpdatedAt = now
	model.Allocations = []vault.Allocation{}
	r.vaults[model.ID] = cloneHandlerVault(model)
	return cloneHandlerVault(model), nil
}

func (r *handlerRepository) GetVault(_ context.Context, id uuid.UUID) (vault.Vault, error) {
	model, ok := r.vaults[id]
	if !ok {
		return vault.Vault{}, vault.ErrVaultNotFound
	}
	return cloneHandlerVault(model), nil
}

func (r *handlerRepository) ListUserVaults(_ context.Context, userID uuid.UUID, filter vault.UserListFilter) ([]vault.Vault, int, error) {
	models := make([]vault.Vault, 0)
	for _, model := range r.vaults {
		if model.UserID == userID {
			models = append(models, cloneHandlerVault(model))
		}
	}
	total := len(models)
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PerPage < 1 {
		filter.PerPage = 20
	}
	start := (filter.Page - 1) * filter.PerPage
	if start >= total {
		return []vault.Vault{}, total, nil
	}
	end := start + filter.PerPage
	if end > total {
		end = total
	}
	return models[start:end], total, nil
}

func (r *handlerRepository) UpdateVaultBalances(_ context.Context, id uuid.UUID, totalDeposited decimal.Decimal, currentBalance decimal.Decimal) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.TotalDeposited = totalDeposited
	model.CurrentBalance = currentBalance
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)
	return nil
}

func (r *handlerRepository) RecordDeposit(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if record.Amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}

	model.TotalDeposited = model.TotalDeposited.Add(record.Amount)
	model.CurrentBalance = model.CurrentBalance.Add(record.Amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)

	userID := record.UserID
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:                   uuid.New(),
		VaultID:              id,
		UserID:               &userID,
		Type:                 "deposit",
		Amount:               record.Amount,
		TransactionHash:      record.TransactionHash,
		SharesMintedOrBurned: &record.SharesMintedOrBurned,
		SharePriceAtTime:     &record.SharePriceAtTime,
		FeeCharged:           feePtr(record.FeeCharged),
		CreatedAt:            time.Now().UTC(),
	})
	return nil
}

func (r *handlerRepository) ReplaceAllocations(_ context.Context, vaultID uuid.UUID, allocations []vault.Allocation) error {
	model, ok := r.vaults[vaultID]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.Allocations = append([]vault.Allocation(nil), allocations...)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[vaultID] = cloneHandlerVault(model)
	return nil
}

func (r *handlerRepository) UpdateVault(_ context.Context, id uuid.UUID, contractAddress string, status vault.VaultStatus) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.ContractAddress = contractAddress
	model.Status = status
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)
	return nil
}

func (r *handlerRepository) UpdateHarvestFrequency(_ context.Context, id uuid.UUID, frequency string) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	model.HarvestFrequency = frequency
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)
	return nil
}

func (r *handlerRepository) RecordHarvest(_ context.Context, input vault.HarvestRecordInput) error {
	model, ok := r.vaults[input.VaultID]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if input.Compounded {
		model.TotalDeposited = model.TotalDeposited.Add(input.NetYield)
		model.CurrentBalance = model.CurrentBalance.Add(input.NetYield)
	} else {
		model.CurrentBalance = model.CurrentBalance.Sub(input.NetYield)
	}
	model.YieldEarned = model.YieldEarned.Sub(input.NetYield.Add(input.PerformanceFee))
	if model.YieldEarned.IsNegative() {
		model.YieldEarned = decimal.Zero
	}
	model.FeesPaid = model.FeesPaid.Add(input.PerformanceFee)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[input.VaultID] = cloneHandlerVault(model)
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:        uuid.New(),
		VaultID:   input.VaultID,
		Type:      "harvest",
		Amount:    input.NetYield,
		CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (r *handlerRepository) RecordWithdrawal(_ context.Context, id uuid.UUID, record vault.TransactionRecord) error {
	model, ok := r.vaults[id]
	if !ok {
		return vault.ErrVaultNotFound
	}
	if record.Amount.Cmp(decimal.Zero) <= 0 {
		return vault.ErrInvalidAmount
	}
	if record.TransactionHash != "" {
		for _, txn := range r.transactions {
			if txn.TransactionHash == record.TransactionHash {
				return vault.ErrDuplicateTransaction
			}
		}
	}

	model.CurrentBalance = model.CurrentBalance.Sub(record.Amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[id] = cloneHandlerVault(model)

	userID := record.UserID
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:                   uuid.New(),
		VaultID:              id,
		UserID:               &userID,
		Type:                 "withdrawal",
		Amount:               record.Amount,
		TransactionHash:      record.TransactionHash,
		SharesMintedOrBurned: &record.SharesMintedOrBurned,
		SharePriceAtTime:     &record.SharePriceAtTime,
		FeeCharged:           feePtr(record.FeeCharged),
		CreatedAt:            time.Now().UTC(),
	})
	return nil
}

func (r *handlerRepository) SoftDeleteVault(_ context.Context, id uuid.UUID) error {
	if _, ok := r.vaults[id]; !ok {
		return vault.ErrVaultNotFound
	}
	delete(r.vaults, id)
	return nil
}

func (r *handlerRepository) ListDeposits(_ context.Context, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	result := make([]vault.VaultTransaction, 0)
	for _, txn := range r.transactions {
		if txn.VaultID == vaultID && txn.Type == "deposit" {
			result = append(result, txn)
		}
	}
	return result, nil
}

func (r *handlerRepository) ListVaults(_ context.Context, filter vault.ListFilter) ([]vault.Vault, int, error) {
	out := make([]vault.Vault, 0)
	for _, v := range r.vaults {
		if filter.Status != "" && string(v.Status) != filter.Status {
			continue
		}
		out = append(out, v)
	}
	total := len(out)
	if filter.Offset < total {
		out = out[filter.Offset:]
	} else {
		out = nil
	}
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, total, nil
}

func (r *handlerRepository) RecordRebalance(_ context.Context, input vault.RebalanceRecordInput, withdrawRecord, depositRecord vault.TransactionRecord) error {
	model, ok := r.vaults[input.VaultID]
	if !ok {
		return vault.ErrVaultNotFound
	}

	// Apply withdrawal
	model.CurrentBalance = model.CurrentBalance.Sub(withdrawRecord.Amount)
	// Apply deposit
	model.CurrentBalance = model.CurrentBalance.Add(depositRecord.Amount)
	model.TotalDeposited = model.TotalDeposited.Add(depositRecord.Amount)
	model.UpdatedAt = time.Now().UTC()
	r.vaults[input.VaultID] = cloneHandlerVault(model)

	// Add withdrawal transaction
	withdrawUserID := withdrawRecord.UserID
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:                   uuid.New(),
		VaultID:              input.VaultID,
		UserID:               &withdrawUserID,
		Type:                 "withdrawal",
		Amount:               withdrawRecord.Amount,
		TransactionHash:      withdrawRecord.TransactionHash,
		SharesMintedOrBurned: &withdrawRecord.SharesMintedOrBurned,
		SharePriceAtTime:     &withdrawRecord.SharePriceAtTime,
		FeeCharged:           feePtr(withdrawRecord.FeeCharged),
		CreatedAt:            time.Now().UTC(),
	})

	// Add deposit transaction
	depositUserID := depositRecord.UserID
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:                   uuid.New(),
		VaultID:              input.VaultID,
		UserID:               &depositUserID,
		Type:                 "deposit",
		Amount:               depositRecord.Amount,
		TransactionHash:      depositRecord.TransactionHash,
		SharesMintedOrBurned: &depositRecord.SharesMintedOrBurned,
		SharePriceAtTime:     &depositRecord.SharePriceAtTime,
		FeeCharged:           feePtr(depositRecord.FeeCharged),
		CreatedAt:            time.Now().UTC(),
	})

	// Add rebalance transaction
	r.transactions = append(r.transactions, vault.VaultTransaction{
		ID:              uuid.New(),
		VaultID:         input.VaultID,
		UserID:          &input.UserID,
		Type:            "rebalance",
		Amount:          input.Amount,
		TransactionHash: input.TransactionHash,
		CreatedAt:       time.Now().UTC(),
	})

	return nil
}

func (r *handlerRepository) ListUserVaultTransactions(_ context.Context, userID uuid.UUID, vaultID uuid.UUID) ([]vault.VaultTransaction, error) {
	result := make([]vault.VaultTransaction, 0)
	for _, txn := range r.transactions {
		if txn.VaultID == vaultID && txn.UserID != nil && *txn.UserID == userID {
			result = append(result, txn)
		}
	}
	return result, nil
}

func feePtr(fee decimal.Decimal) *decimal.Decimal {
	if fee.IsZero() {
		return nil
	}
	return &fee
}

func cloneHandlerVault(model vault.Vault) vault.Vault {
	model.Allocations = append([]vault.Allocation(nil), model.Allocations...)
	return model
}

// TestWithMoneyPathFieldsPropagatesToLaterLogCalls confirms the core
// mechanism #1342 relies on: binding vault_id/protocol_id onto the request
// early in a handler makes them show up on every later log call for that
// request - including writeDomainError's own failure log, which never
// receives these fields as explicit arguments.
func TestWithMoneyPathFieldsPropagatesToLaterLogCalls(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vaults/abc/deposit", nil)
	req = req.WithContext(logpkg.WithLogger(req.Context(), baseLogger))

	req = withMoneyPathFields(req, "vault_id", "vault-123")

	// Simulate a later call site - e.g. writeDomainError - that only has the
	// request, not the vault id, and logs through logpkg.FromContext like
	// every real call site does.
	logpkg.FromContext(req.Context()).Error("vault handler failed", "error", "boom")

	var entry map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &entry); err != nil {
		t.Fatalf("log output is not valid JSON: %v (output: %q)", err, buf.String())
	}
	if entry["vault_id"] != "vault-123" {
		t.Errorf("expected vault_id=vault-123 on the later log call, got %v (full entry: %v)", entry["vault_id"], entry)
	}
	if entry["error"] != "boom" {
		t.Errorf("expected the later log call's own fields to still be present, got %v", entry["error"])
	}
}

// TestWithMoneyPathFieldsSupportsMultiplePairs confirms the multi-field
// case rebalancePosition needs (from_protocol_id and to_protocol_id
// together), not just the single vault_id case.
func TestWithMoneyPathFieldsSupportsMultiplePairs(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/vault/rebalance", nil)
	req = req.WithContext(logpkg.WithLogger(req.Context(), baseLogger))

	req = withMoneyPathFields(req, "vault_id", "vault-456", "from_protocol_id", "aave", "to_protocol_id", "compound")
	logpkg.FromContext(req.Context()).Error("rebalance failed")

	out := buf.String()
	for _, want := range []string{`"vault_id":"vault-456"`, `"from_protocol_id":"aave"`, `"to_protocol_id":"compound"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected log output to contain %s, got %q", want, out)
		}
	}
}

// TestWithMoneyPathFieldsDoesNotMutateOriginalRequest confirms the
// *http.Request returned is a distinct value - callers must use the
// returned request from that point on (as every real call site does via
// `r = withMoneyPathFields(r, ...)`), and the original is left untouched,
// matching how http.Request.WithContext itself behaves.
func TestWithMoneyPathFieldsDoesNotMutateOriginalRequest(t *testing.T) {
	var buf bytes.Buffer
	baseLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	original := httptest.NewRequest(http.MethodGet, "/api/v1/vaults/abc", nil)
	original = original.WithContext(logpkg.WithLogger(original.Context(), baseLogger))

	enriched := withMoneyPathFields(original, "vault_id", "vault-789")
	if enriched == original {
		t.Fatal("expected withMoneyPathFields to return a distinct *http.Request")
	}

	logpkg.FromContext(original.Context()).Info("using the original request")
	logpkg.FromContext(enriched.Context()).Info("using the enriched request")

	entries := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}
	if strings.Contains(entries[0], "vault_id") {
		t.Errorf("the original request's logger must not have vault_id, got %q", entries[0])
	}
	if !strings.Contains(entries[1], `"vault_id":"vault-789"`) {
		t.Errorf("the enriched request's logger must have vault_id, got %q", entries[1])
	}
}

// errGetVaultBoom is an unmapped error: writeDomainError has no case for
// it, so it falls through to the default branch, which is the one branch
// that actually calls logpkg.FromContext(r.Context()).Error(...) (every
// named branch above it - ErrVaultNotFound included - is a known, expected
// outcome and deliberately not logged).
var errGetVaultBoom = errors.New("boom: unmapped repository failure")

// boomingGetVaultRepository wraps handlerRepository and makes GetVault fail
// with an error writeDomainError doesn't recognize, so the request reaches
// its logged default branch instead of a named 404 branch.
type boomingGetVaultRepository struct {
	*handlerRepository
}

func (r *boomingGetVaultRepository) GetVault(context.Context, uuid.UUID) (vault.Vault, error) {
	return vault.Vault{}, errGetVaultBoom
}

// TestVaultHandlerInternalErrorLogsVaultID confirms the end-to-end path for
// #1342: a real request through the handler that fails with an unmapped
// (logged) error produces a failure log line carrying vault_id, proving
// withMoneyPathFields and writeDomainError's default branch work together,
// not just the helper in isolation.
func TestVaultHandlerInternalErrorLogsVaultID(t *testing.T) {
	repository := &boomingGetVaultRepository{handlerRepository: newHandlerRepository(uuid.New())}
	handler := NewVaultHandler(service.NewVaultService(repository))
	mux := http.NewServeMux()
	handler.Register(mux)

	var buf bytes.Buffer
	requestLogger := slog.New(slog.NewJSONHandler(&buf, nil))

	server := httptest.NewServer(fakeAuthMiddleware(uuid.New())(middleware.Logging(requestLogger)(mux)))
	defer server.Close()

	vaultID := uuid.New()
	resp, err := http.Get(server.URL + "/api/v1/vaults/" + vaultID.String())
	if err != nil {
		t.Fatalf("GET vault error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500 for the unmapped repository error, got %d", resp.StatusCode)
	}

	found := false
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not valid JSON: %v (line: %q)", err, line)
		}
		if entry["msg"] != "vault handler failed" {
			continue
		}
		if entry["vault_id"] == vaultID.String() {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected the \"vault handler failed\" log line to carry vault_id=%s, got log output: %q", vaultID, buf.String())
	}
}
