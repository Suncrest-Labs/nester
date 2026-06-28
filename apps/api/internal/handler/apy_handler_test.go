package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/apysnapshot"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// mockAPYRepo is an in-memory implementation of apysnapshot.Repository for tests.
type mockAPYRepo struct {
	snapshots []apysnapshot.APYSnapshot
	upserted  []apysnapshot.APYSnapshot
	pruned    []time.Duration
}

func (m *mockAPYRepo) Upsert(_ context.Context, snap apysnapshot.APYSnapshot) error {
	m.upserted = append(m.upserted, snap)
	m.snapshots = append(m.snapshots, snap)
	return nil
}

func (m *mockAPYRepo) ListByProtocol(_ context.Context, slug string, since time.Time) ([]apysnapshot.APYSnapshot, error) {
	var out []apysnapshot.APYSnapshot
	for _, s := range m.snapshots {
		if s.ProtocolSlug == slug && !s.CapturedAt.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockAPYRepo) PruneOlderThan(_ context.Context, age time.Duration) error {
	m.pruned = append(m.pruned, age)
	cutoff := time.Now().UTC().Add(-age)
	var remaining []apysnapshot.APYSnapshot
	for _, s := range m.snapshots {
		if !s.CapturedAt.Before(cutoff) {
			remaining = append(remaining, s)
		}
	}
	m.snapshots = remaining
	return nil
}

// newAPYTestServer wires a mock DeFiLlama server → APYService → APYHandler.
func newAPYTestServer(t *testing.T, defiLlamaBody string) (*httptest.Server, *mockAPYRepo) {
	t.Helper()

	defiLlama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(defiLlamaBody))
	}))
	t.Cleanup(defiLlama.Close)

	repo := &mockAPYRepo{}
	svc := service.NewAPYServiceWithClient(repo, defiLlama.URL, defiLlama.Client())
	mux := http.NewServeMux()
	NewAPYHandler(svc).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, repo
}

// TestAPYPoller_SuccessfulPolling verifies that snapshots are written for Stellar pools.
func TestAPYPoller_SuccessfulPolling(t *testing.T) {
	const body = `{"data":[
		{"project":"blend","chain":"Stellar","apy":5.23,"tvlUsd":1000000},
		{"project":"aqua","chain":"Stellar","apy":8.10,"tvlUsd":500000},
		{"project":"ethereum-pool","chain":"Ethereum","apy":4.00,"tvlUsd":9000000}
	]}`

	repo := &mockAPYRepo{}
	defiLlama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(defiLlama.Close)

	svc := service.NewAPYServiceWithClient(repo, defiLlama.URL, defiLlama.Client())
	svc.PollOnce(context.Background())

	if len(repo.upserted) != 2 {
		t.Fatalf("expected 2 upserted snapshots (Stellar only), got %d", len(repo.upserted))
	}
	slugs := map[string]bool{}
	for _, s := range repo.upserted {
		slugs[s.ProtocolSlug] = true
	}
	if !slugs["blend"] || !slugs["aqua"] {
		t.Fatalf("expected blend and aqua, got %v", slugs)
	}
}

// TestAPYHandler_APIResponse verifies GET /api/v1/yields/{slug}/apy-history shape.
func TestAPYHandler_APIResponse(t *testing.T) {
	now := time.Now().UTC()
	repo := &mockAPYRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{ID: uuid.New(), ProtocolSlug: "blend", APY: decimal.NewFromFloat(5.23), TVL: decimal.NewFromFloat(1000000), CapturedAt: now.Add(-2 * 24 * time.Hour)},
			{ID: uuid.New(), ProtocolSlug: "blend", APY: decimal.NewFromFloat(5.50), TVL: decimal.NewFromFloat(1100000), CapturedAt: now.Add(-1 * 24 * time.Hour)},
		},
	}
	svc := service.NewAPYService(repo)
	mux := http.NewServeMux()
	NewAPYHandler(svc).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/yields/blend/apy-history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data service.APYHistoryResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body.Data.ProtocolSlug != "blend" {
		t.Errorf("protocol_slug = %q, want %q", body.Data.ProtocolSlug, "blend")
	}
	if len(body.Data.Snapshots) != 2 {
		t.Fatalf("snapshots count = %d, want 2", len(body.Data.Snapshots))
	}
	if body.Data.Snapshots[0].APY != "5.2300" {
		t.Errorf("snapshot[0].apy = %q, want %q", body.Data.Snapshots[0].APY, "5.2300")
	}
}

// TestAPYHandler_SummaryCalculations verifies avg/min/max computation.
func TestAPYHandler_SummaryCalculations(t *testing.T) {
	now := time.Now().UTC()
	repo := &mockAPYRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{ID: uuid.New(), ProtocolSlug: "blend", APY: decimal.NewFromFloat(4.00), TVL: decimal.Zero, CapturedAt: now.Add(-3 * 24 * time.Hour)},
			{ID: uuid.New(), ProtocolSlug: "blend", APY: decimal.NewFromFloat(6.00), TVL: decimal.Zero, CapturedAt: now.Add(-2 * 24 * time.Hour)},
			{ID: uuid.New(), ProtocolSlug: "blend", APY: decimal.NewFromFloat(5.00), TVL: decimal.Zero, CapturedAt: now.Add(-1 * 24 * time.Hour)},
		},
	}
	svc := service.NewAPYService(repo)
	history, err := svc.GetHistory(context.Background(), "blend")
	if err != nil {
		t.Fatal(err)
	}

	if history.Summary.AvgAPY != "5.0000" {
		t.Errorf("avg_apy = %q, want %q", history.Summary.AvgAPY, "5.0000")
	}
	if history.Summary.MinAPY != "4.0000" {
		t.Errorf("min_apy = %q, want %q", history.Summary.MinAPY, "4.0000")
	}
	if history.Summary.MaxAPY != "6.0000" {
		t.Errorf("max_apy = %q, want %q", history.Summary.MaxAPY, "6.0000")
	}
}

// TestAPYHandler_Pruning verifies records older than 90 days are pruned.
func TestAPYHandler_Pruning(t *testing.T) {
	old := time.Now().UTC().Add(-100 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-1 * 24 * time.Hour)

	repo := &mockAPYRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{ID: uuid.New(), ProtocolSlug: "blend", APY: decimal.NewFromFloat(5.0), TVL: decimal.Zero, CapturedAt: old},
			{ID: uuid.New(), ProtocolSlug: "blend", APY: decimal.NewFromFloat(5.5), TVL: decimal.Zero, CapturedAt: recent},
		},
	}

	if err := repo.PruneOlderThan(context.Background(), 90*24*time.Hour); err != nil {
		t.Fatal(err)
	}

	if len(repo.snapshots) != 1 {
		t.Fatalf("expected 1 snapshot after pruning, got %d", len(repo.snapshots))
	}
	if repo.snapshots[0].CapturedAt.Equal(old) {
		t.Error("old snapshot should have been pruned")
	}
}

// TestAPYHandler_EmptyHistory verifies 404 when no snapshots exist for a protocol.
func TestAPYHandler_EmptyHistory(t *testing.T) {
	repo := &mockAPYRepo{}
	svc := service.NewAPYService(repo)
	mux := http.NewServeMux()
	NewAPYHandler(svc).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/yields/unknown-protocol/apy-history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestAPYHandler_ProtocolNotFound verifies 404 when protocol has no data.
func TestAPYHandler_ProtocolNotFound(t *testing.T) {
	now := time.Now().UTC()
	repo := &mockAPYRepo{
		snapshots: []apysnapshot.APYSnapshot{
			{ID: uuid.New(), ProtocolSlug: "blend", APY: decimal.NewFromFloat(5.23), TVL: decimal.Zero, CapturedAt: now},
		},
	}
	svc := service.NewAPYService(repo)
	mux := http.NewServeMux()
	NewAPYHandler(svc).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/yields/nonexistent/apy-history")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
