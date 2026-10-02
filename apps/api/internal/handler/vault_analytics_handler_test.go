package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

type fakeAnalyticsQuerier struct {
	analytics service.VaultAnalytics
	err       error
}

func (f *fakeAnalyticsQuerier) Compute(_ context.Context, vaultID uuid.UUID, period string) (service.VaultAnalytics, error) {
	if f.err != nil {
		return service.VaultAnalytics{}, f.err
	}
	out := f.analytics
	out.VaultID = vaultID
	out.Period = period
	return out, nil
}

type fakeDriftQuerier struct {
	state service.DriftState
	err   error
}

func (f *fakeDriftQuerier) GetDriftState(_ context.Context, _ uuid.UUID) (service.DriftState, error) {
	if f.err != nil {
		return service.DriftState{}, f.err
	}
	return f.state, nil
}

func newAnalyticsTestServer(svc VaultAnalyticsQuerier, drift DriftStateQuerier) *httptest.Server {
	mux := http.NewServeMux()
	NewVaultAnalyticsHandler(svc, drift).Register(mux)
	return httptest.NewServer(mux)
}

func TestVaultAnalyticsHandler_AttachesDriftStateWhenWired(t *testing.T) {
	drift := &fakeDriftQuerier{state: service.DriftState{
		CurrentTopProtocol: "protocol-a",
		OptimalProtocol:    "protocol-b",
		DriftBPS:           900,
		ThresholdBPS:       200,
		ExceedsThreshold:   true,
		Reason:             "rebalance",
	}}
	server := newAnalyticsTestServer(&fakeAnalyticsQuerier{}, drift)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + uuid.New().String() + "/analytics")
	if err != nil {
		t.Fatalf("GET analytics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data service.VaultAnalytics `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Drift == nil {
		t.Fatal("expected drift field to be populated")
	}
	if body.Data.Drift.OptimalProtocol != "protocol-b" || !body.Data.Drift.ExceedsThreshold {
		t.Errorf("unexpected drift state in response: %+v", body.Data.Drift)
	}
}

func TestVaultAnalyticsHandler_OmitsDriftWhenNotWired(t *testing.T) {
	// drift is nil, matching how the handler can be constructed without the
	// APY drift detector (e.g. in tests, or an unwired deployment) per
	// NewVaultAnalyticsHandler's doc comment.
	server := newAnalyticsTestServer(&fakeAnalyticsQuerier{}, nil)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + uuid.New().String() + "/analytics")
	if err != nil {
		t.Fatalf("GET analytics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body.Data, &raw); err != nil {
		t.Fatalf("decode data as map: %v", err)
	}
	if _, present := raw["drift"]; present {
		t.Error("expected \"drift\" to be omitted entirely (omitempty) when no detector is wired")
	}
}

func TestVaultAnalyticsHandler_SucceedsWhenDriftLookupFails(t *testing.T) {
	// A drift-state failure must not fail the whole analytics response —
	// the historical metrics half is still valid and useful on its own.
	drift := &fakeDriftQuerier{err: assertHandlerErr("vault repo down")}
	server := newAnalyticsTestServer(&fakeAnalyticsQuerier{}, drift)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + uuid.New().String() + "/analytics")
	if err != nil {
		t.Fatalf("GET analytics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 even when drift lookup fails, got %d", resp.StatusCode)
	}
}

func TestVaultAnalyticsHandler_RejectsInvalidVaultID(t *testing.T) {
	server := newAnalyticsTestServer(&fakeAnalyticsQuerier{}, nil)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/vaults/not-a-uuid/analytics")
	if err != nil {
		t.Fatalf("GET analytics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed vault id, got %d", resp.StatusCode)
	}
}

func TestVaultAnalyticsHandler_RejectsInvalidPeriod(t *testing.T) {
	server := newAnalyticsTestServer(&fakeAnalyticsQuerier{}, nil)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/vaults/" + uuid.New().String() + "/analytics?period=lifetime")
	if err != nil {
		t.Fatalf("GET analytics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for an invalid period, got %d", resp.StatusCode)
	}
}

type assertHandlerErr string

func (e assertHandlerErr) Error() string { return string(e) }
