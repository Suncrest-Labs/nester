package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/yieldharvest"
	"github.com/suncrestlabs/nester/apps/api/internal/middleware"
	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

// fakeYieldHarvestService is a test double for yieldHarvestServicer.
type fakeYieldHarvestService struct {
	output service.ListYieldHarvestsOutput
	err    error
	// captures the last call arguments for assertion
	lastInput service.ListYieldHarvestsInput
}

func (f *fakeYieldHarvestService) ListHarvests(_ context.Context, input service.ListYieldHarvestsInput) (service.ListYieldHarvestsOutput, error) {
	f.lastInput = input
	return f.output, f.err
}

func newYieldHarvestServer(t *testing.T, userID uuid.UUID, svc yieldHarvestServicer) *httptest.Server {
	t.Helper()
	h := NewYieldHarvestHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	return httptest.NewServer(
		fakeAuthMiddleware(userID)(
			middleware.Logging(slog.New(slog.NewTextHandler(io.Discard, nil)))(mux),
		),
	)
}

func decodeYieldHarvestData(t *testing.T, body io.Reader) service.ListYieldHarvestsOutput {
	t.Helper()
	var env struct {
		Success bool                        `json:"success"`
		Data    service.ListYieldHarvestsOutput `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return env.Data
}

func TestYieldHarvestHandler_Unauthenticated(t *testing.T) {
	svc := &fakeYieldHarvestService{}
	h := NewYieldHarvestHandler(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	// No auth middleware — context has no user.
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/yields/harvests")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestYieldHarvestHandler_EmptyHistory(t *testing.T) {
	userID := uuid.New()
	svc := &fakeYieldHarvestService{
		output: service.ListYieldHarvestsOutput{Items: []yieldharvest.YieldHarvest{}, NextCursor: ""},
	}
	srv := newYieldHarvestServer(t, userID, svc)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/yields/harvests")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	out := decodeYieldHarvestData(t, resp.Body)
	if len(out.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(out.Items))
	}
	if out.NextCursor != "" {
		t.Errorf("expected empty next_cursor, got %q", out.NextCursor)
	}
}

func TestYieldHarvestHandler_MultipleHarvests(t *testing.T) {
	userID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	items := []yieldharvest.YieldHarvest{
		{ID: uuid.New(), UserID: userID, VaultID: uuid.New(), Amount: decimal.NewFromFloat(10.5), Currency: "USDC", Protocol: "blend", HarvestedAt: now, TxHash: "abc123"},
		{ID: uuid.New(), UserID: userID, VaultID: uuid.New(), Amount: decimal.NewFromFloat(5.25), Currency: "USDC", Protocol: "blend", HarvestedAt: now.Add(-time.Hour), TxHash: "def456"},
	}
	svc := &fakeYieldHarvestService{
		output: service.ListYieldHarvestsOutput{Items: items, NextCursor: ""},
	}
	srv := newYieldHarvestServer(t, userID, svc)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/yields/harvests")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	out := decodeYieldHarvestData(t, resp.Body)
	if len(out.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(out.Items))
	}
}

func TestYieldHarvestHandler_Pagination(t *testing.T) {
	userID := uuid.New()
	nextCursor := "bmV4dC1wYWdl" // arbitrary base64
	svc := &fakeYieldHarvestService{
		output: service.ListYieldHarvestsOutput{
			Items:      []yieldharvest.YieldHarvest{{ID: uuid.New(), UserID: userID, VaultID: uuid.New(), Amount: decimal.NewFromInt(1), Currency: "USDC"}},
			NextCursor: nextCursor,
		},
	}
	srv := newYieldHarvestServer(t, userID, svc)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/yields/harvests?limit=1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	out := decodeYieldHarvestData(t, resp.Body)
	if out.NextCursor != nextCursor {
		t.Errorf("expected next_cursor %q, got %q", nextCursor, out.NextCursor)
	}
	if svc.lastInput.Limit != 1 {
		t.Errorf("expected limit 1 forwarded to service, got %d", svc.lastInput.Limit)
	}
}

func TestYieldHarvestHandler_CursorForwarded(t *testing.T) {
	userID := uuid.New()
	cursor := "dGVzdC1jdXJzb3I="
	svc := &fakeYieldHarvestService{
		output: service.ListYieldHarvestsOutput{Items: nil, NextCursor: ""},
	}
	srv := newYieldHarvestServer(t, userID, svc)
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/yields/harvests?cursor=%s", srv.URL, cursor))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if svc.lastInput.Cursor != cursor {
		t.Errorf("expected cursor %q forwarded, got %q", cursor, svc.lastInput.Cursor)
	}
}

func TestYieldHarvestHandler_InvalidLimit(t *testing.T) {
	userID := uuid.New()
	svc := &fakeYieldHarvestService{}
	srv := newYieldHarvestServer(t, userID, svc)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/yields/harvests?limit=abc")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}
