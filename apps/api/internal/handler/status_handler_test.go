package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/moneypath"
	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
)

type stubSwitchService struct {
	switches []moneypath.Switch
}

func (s *stubSwitchService) List(_ context.Context) ([]moneypath.Switch, error) {
	return s.switches, nil
}

func (s *stubSwitchService) SetPaused(_ context.Context, _ moneypath.Operation, _ bool, _ string, _ *uuid.UUID, _ string) (moneypath.Switch, error) {
	return moneypath.Switch{}, nil
}

type stubFreshnessReader struct {
	snap freshness.Snapshot
}

func (r stubFreshnessReader) Snapshot() freshness.Snapshot {
	return r.snap
}

func TestStatusHandler_ReturnsPauseIndexerAndRPCStateWithCaching(t *testing.T) {
	var rpcCalls atomic.Int32
	switches := &stubSwitchService{
		switches: []moneypath.Switch{
			{Operation: moneypath.OperationDeposit, Paused: true, Reason: "scheduled maintenance"},
			{Operation: moneypath.OperationWithdrawal, Paused: false},
		},
	}
	reader := stubFreshnessReader{
		snap: freshness.Snapshot{
			Sampled:    true,
			Lag:        180 * time.Second,
			LagLedgers: 36,
			Stale:      true,
		},
	}

	h := NewStatusHandler(switches, reader, func(_ context.Context) error {
		rpcCalls.Add(1)
		return errors.New("rpc timeout")
	})
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	h.now = func() time.Time { return now }

	mux := http.NewServeMux()
	h.Register(mux)

	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec1.Code)
	}
	var env struct {
		Success bool                `json:"success"`
		Data    PublicStatusPayload `json:"data"`
	}
	if err := json.NewDecoder(rec1.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !env.Success {
		t.Fatal("expected success=true")
	}
	if !env.Data.DepositsPaused || env.Data.WithdrawalsPaused {
		t.Fatalf("unexpected pause flags: %+v", env.Data)
	}
	if env.Data.IndexerLagSeconds != 180 || !env.Data.IndexerStale {
		t.Fatalf("unexpected indexer lag state: %+v", env.Data)
	}
	if env.Data.RPCHealthy {
		t.Fatalf("expected rpc_healthy=false, got true")
	}
	if env.Data.Status != "paused" {
		t.Fatalf("expected status=paused, got %q", env.Data.Status)
	}

	// Second call within 5s cache TTL must hit cache without probing RPC again
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rpcCalls.Load() != 1 {
		t.Fatalf("expected cached response to avoid second RPC probe, got %d calls", rpcCalls.Load())
	}
}
