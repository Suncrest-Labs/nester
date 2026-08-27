package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type handlerChainVerifier struct {
	amount decimal.Decimal
}

func (h handlerChainVerifier) VerifyVaultEvent(_ context.Context, txHash, _, _ string) (service.VerifiedVaultEvent, error) {
	return service.VerifiedVaultEvent{TxHash: txHash, EventType: "withdraw", Amount: h.amount}, nil
}

func TestVaultHandlerWithdrawForgedAmountRecordsEventAmount(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	vaultService.SetChainEventVerifier(handlerChainVerifier{amount: decimal.RequireFromString("40")})
	h := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(fakeAuthMiddleware(userID)(mux))
	t.Cleanup(server.Close)

	created, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID: userID, ContractAddress: testAddrGeneric, Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if _, err := vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("100"),
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	resp, err := http.Post(
		server.URL+"/api/v1/vaults/"+created.ID.String()+"/withdraw",
		"application/json",
		bytes.NewBufferString(`{"amount":"10","asset":"USDC","tx_hash":"hash-forged"}`),
	)
	if err != nil {
		t.Fatalf("POST withdraw: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}
	updated := decodeAPIData[vault.Vault](t, resp.Body)
	if !updated.CurrentBalance.Equal(decimal.RequireFromString("60")) {
		t.Fatalf("balance = %s, want 60 (event 40, not forged body 10)", updated.CurrentBalance)
	}
}

func TestVaultHandlerWithdrawExceedingPositionRejected(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	h := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(fakeAuthMiddleware(userID)(mux))
	t.Cleanup(server.Close)

	created, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID: userID, ContractAddress: testAddrGeneric, Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}
	if _, err := vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
		VaultID: created.ID, Amount: decimal.RequireFromString("20"),
	}); err != nil {
		t.Fatalf("RecordDeposit: %v", err)
	}

	resp, err := http.Post(
		server.URL+"/api/v1/vaults/"+created.ID.String()+"/withdraw",
		"application/json",
		bytes.NewBufferString(`{"amount":"50","asset":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST withdraw: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
}

func TestVaultHandlerDepositMissingIdempotencyKeyIs400(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	h := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	h.Register(mux)

	store := newHandlerIdempotencyStore()
	server := httptest.NewServer(fakeAuthMiddleware(userID)(
		middleware.IdempotencyMiddleware(store, middleware.VaultMoneyPathIdempotencyRoutes())(mux),
	))
	t.Cleanup(server.Close)

	created, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID: userID, ContractAddress: testAddrGeneric, Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	resp, err := http.Post(
		server.URL+"/api/v1/vaults/"+created.ID.String()+"/deposit",
		"application/json",
		bytes.NewBufferString(`{"amount":"10","asset":"USDC"}`),
	)
	if err != nil {
		t.Fatalf("POST deposit: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400 for missing Idempotency-Key", resp.StatusCode)
	}
}

func TestVaultHandlerDepositIdempotencyKeyTwentyConcurrentOneEffect(t *testing.T) {
	userID := uuid.New()
	repository := newHandlerRepository(userID)
	vaultService := service.NewVaultService(repository)
	h := NewVaultHandler(vaultService)
	mux := http.NewServeMux()
	h.Register(mux)

	store := newHandlerIdempotencyStore()
	server := httptest.NewServer(fakeAuthMiddleware(userID)(
		middleware.IdempotencyMiddleware(store, middleware.VaultMoneyPathIdempotencyRoutes())(mux),
	))
	t.Cleanup(server.Close)

	created, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
		UserID: userID, ContractAddress: testAddrGeneric, Currency: "USDC",
	})
	if err != nil {
		t.Fatalf("CreateVault: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	codes := make([]int, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/vaults/"+created.ID.String()+"/deposit", bytes.NewBufferString(`{"amount":"25","asset":"USDC"}`))
			if err != nil {
				t.Errorf("NewRequest: %v", err)
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "same-deposit-key")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("POST: %v", err)
				return
			}
			defer resp.Body.Close()
			codes[i] = resp.StatusCode
			_, _ = io.Copy(io.Discard, resp.Body)
		}(i)
	}
	wg.Wait()

	updated, err := vaultService.GetVault(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetVault: %v", err)
	}
	if !updated.TotalDeposited.Equal(decimal.RequireFromString("25")) {
		t.Fatalf("total_deposited = %s, want 25 (one effect from 20 concurrent retries)", updated.TotalDeposited)
	}

	var ok int
	for _, c := range codes {
		if c == http.StatusCreated || c == http.StatusOK {
			ok++
		}
	}
	if ok != n {
		t.Fatalf("successful responses = %d / %d, codes=%v (waiters must return the stored result)", ok, n, codes)
	}
}

type handlerIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]middleware.IdempotencyRecord
}

func newHandlerIdempotencyStore() *handlerIdempotencyStore {
	return &handlerIdempotencyStore{records: make(map[string]middleware.IdempotencyRecord)}
}

func (s *handlerIdempotencyStore) storeKey(userID uuid.UUID, key string) string {
	return userID.String() + ":" + key
}

func (s *handlerIdempotencyStore) Claim(_ context.Context, userID uuid.UUID, key, fingerprint string, _ time.Duration) (bool, middleware.IdempotencyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.storeKey(userID, key)
	if existing, ok := s.records[k]; ok {
		return false, existing, nil
	}
	s.records[k] = middleware.IdempotencyRecord{Fingerprint: fingerprint, Status: "in_progress"}
	return true, middleware.IdempotencyRecord{}, nil
}

func (s *handlerIdempotencyStore) Complete(_ context.Context, userID uuid.UUID, key string, status int, body []byte, contentType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.storeKey(userID, key)
	rec := s.records[k]
	rec.Status = "completed"
	rec.ResponseStatus = status
	rec.ResponseBody = append([]byte(nil), body...)
	rec.ResponseContentType = contentType
	s.records[k] = rec
	return nil
}

func (s *handlerIdempotencyStore) Release(_ context.Context, userID uuid.UUID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, s.storeKey(userID, key))
	return nil
}

func (s *handlerIdempotencyStore) Get(_ context.Context, userID uuid.UUID, key string) (middleware.IdempotencyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[s.storeKey(userID, key)]
	if !ok {
		return middleware.IdempotencyRecord{}, middleware.ErrIdempotencyKeyNotFound
	}
	return rec, nil
}
