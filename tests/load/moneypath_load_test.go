package load

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
	"github.com/suncrestlabs/nester/apps/api/internal/handler"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// TestMoneyPathLaunchVolumeLoadProfile runs a concurrent load test simulating expected
// launch volume on the core money path (deposits and withdrawals), measuring throughput,
// latency percentiles (p50, p95, p99), error rates, and concurrency bottlenecks.
func TestMoneyPathLaunchVolumeLoadProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in short mode")
	}

	// Launch volume profile parameters:
	// - Concurrent virtual users: 50
	// - Requests per user: 20
	// - Total transactions: 1,000
	const concurrency = 50
	const requestsPerUser = 20
	totalRequests := concurrency * requestsPerUser

	userID := uuid.New()
	repository := newLoadTestRepository(userID)
	vaultService := service.NewVaultService(repository)
	h := handler.NewVaultHandler(vaultService)

	mux := http.NewServeMux()
	h.Register(mux)

	// Setup test server with mock auth middleware injecting our load test user
	server := httptest.NewServer(loadTestAuthMiddleware(userID)(mux))
	t.Cleanup(server.Close)

	// Pre-create vaults for users to deposit/withdraw against
	vaultIDs := make([]uuid.UUID, concurrency)
	for i := 0; i < concurrency; i++ {
		created, err := vaultService.CreateVault(context.Background(), service.CreateVaultInput{
			UserID:          userID,
			ContractAddress: fmt.Sprintf("CLOADTEST%04d", i),
			Currency:        "USDC",
		})
		if err != nil {
			t.Fatalf("failed to create vault %d: %v", i, err)
		}
		// Seed initial balance so withdrawals have funds
		_, err = vaultService.RecordDeposit(context.Background(), service.RecordDepositInput{
			VaultID: created.ID,
			Amount:  decimal.NewFromInt(1000),
		})
		if err != nil {
			t.Fatalf("failed to seed vault %d: %v", i, err)
		}
		vaultIDs[i] = created.ID
	}

	var successCount int64
	var errorCount int64
	latencies := make([]time.Duration, totalRequests)
	var latencyMu sync.Mutex
	var latencyIdx int

	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(userIndex int) {
			defer wg.Done()
			vaultID := vaultIDs[userIndex]
			client := server.Client()

			for j := 0; j < requestsPerUser; j++ {
				// Alternate between deposit and withdrawal
				endpoint := "deposit"
				payload := `{"amount":"10","asset":"USDC"}`
				if j%2 == 1 {
					endpoint = "withdraw"
					payload = `{"amount":"5","asset":"USDC"}`
				}

				req, err := http.NewRequest(
					http.MethodPost,
					fmt.Sprintf("%s/api/v1/vaults/%s/%s", server.URL, vaultID.String(), endpoint),
					bytes.NewBufferString(payload),
				)
				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Idempotency-Key", fmt.Sprintf("load-test-%d-%d", userIndex, j))

				reqStart := time.Now()
				resp, err := client.Do(req)
				duration := time.Since(reqStart)

				latencyMu.Lock()
				latencies[latencyIdx] = duration
				latencyIdx++
				latencyMu.Unlock()

				if err != nil || resp.StatusCode >= 400 {
					atomic.AddInt64(&errorCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}

				if resp != nil {
					_ = resp.Body.Close()
				}
			}
		}(i)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	// Calculate metrics
	successes := atomic.LoadInt64(&successCount)
	errorsVal := atomic.LoadInt64(&errorCount)
	throughput := float64(totalRequests) / totalDuration.Seconds()

	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i] < latencies[j]
	});

	p50 := latencies[totalRequests*50/100]
	p95 := latencies[totalRequests*95/100]
	p99 := latencies[totalRequests*99/100]

	t.Logf("=== Money Path Load Test Results ===")
	t.Logf("Total Requests: %d", totalRequests)
	t.Logf("Successes: %d, Errors: %d", successes, errorsVal)
	t.Logf("Throughput: %.2f req/sec", throughput)
	t.Logf("Duration: %v", totalDuration)
	t.Logf("Latency p50: %v", p50)
	t.Logf("Latency p95: %v", p95)
	t.Logf("Latency p99: %v", p99)

	errorRate := float64(errorsVal) / float64(totalRequests)
	if errorRate > 0.01 {
		t.Errorf("error rate %.2f%% exceeds 1%% launch threshold", errorRate*100)
	}
	if p95 > 500*time.Millisecond {
		t.Errorf("p95 latency %v exceeds 500ms launch threshold", p95)
	}
}

// In-memory mock repository for load testing money path without external DB constraints
type loadTestRepository struct {
	mu     sync.Mutex
	users  map[uuid.UUID]bool
	vaults map[uuid.UUID]*vault.Vault
}

func newLoadTestRepository(userID uuid.UUID) *loadTestRepository {
	return &loadTestRepository{
		users:  map[uuid.UUID]bool{userID: true},
		vaults: make(map[uuid.UUID]*vault.Vault),
	}
}

func (r *loadTestRepository) CreateVault(_ context.Context, v *vault.Vault) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vaults[v.ID] = v
	return nil
}

func (r *loadTestRepository) GetVault(_ context.Context, id uuid.UUID) (*vault.Vault, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.vaults[id]
	if !ok {
		return nil, vault.ErrNotFound
	}
	return v, nil
}

func (r *loadTestRepository) ListVaultsByUserID(_ context.Context, userID uuid.UUID) ([]vault.Vault, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []vault.Vault
	for _, v := range r.vaults {
		if v.UserID == userID {
			out = append(out, *v)
		}
	}
	return out, nil
}

func (r *loadTestRepository) UpdateVaultBalance(_ context.Context, id uuid.UUID, deltaBalance, deltaDeposited, deltaWithdrawn decimal.Decimal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.vaults[id]
	if !ok {
		return vault.ErrNotFound
	}
	v.CurrentBalance = v.CurrentBalance.Add(deltaBalance)
	v.TotalDeposited = v.TotalDeposited.Add(deltaDeposited)
	v.TotalWithdrawn = v.TotalWithdrawn.Add(deltaWithdrawn)
	return nil
}

func (r *loadTestRepository) RecordVaultTransaction(_ context.Context, tx *vault.Transaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return nil
}

func (r *loadTestRepository) GetVaultByContractAddress(_ context.Context, _ string) (*vault.Vault, error) {
	return nil, vault.ErrNotFound
}

func loadTestAuthMiddleware(userID uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), authKey{}, authUser{ID: userID.String()})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type authKey struct{}
type authUser struct{ ID string }
