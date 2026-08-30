package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// The build stamp has to come from the handler the service actually serves.
// An earlier version of this test declared its own inline /health handler and
// asserted against that, which passed regardless of what the server did.
func TestDetailedHealthReportsVersionAndCommit(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(stub.Close)

	ready := &atomic.Bool{}
	ready.Store(true)

	deps := healthDeps{
		ready:        ready,
		pingDB:       func(context.Context) error { return nil },
		poolStats:    func() poolStats { return poolStats{MaxConns: 25} },
		probeTimeout: 2 * time.Second,
		httpClient:   &http.Client{Timeout: 2 * time.Second},
		horizonURL:   stub.URL,
		rpcURL:       stub.URL,
		environment:  "test",
		buildVersion: "v1.2.3",
		buildCommit:  "deadbeef",
		startedAt:    time.Now().Add(-time.Minute),
	}

	mux := http.NewServeMux()
	registerHealthRoutes(mux, deps)

	req := httptest.NewRequest(http.MethodGet, "/health/detailed", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var body detailedHealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode json: %v", err)
	}

	if body.Version != "v1.2.3" {
		t.Errorf("version = %q, want v1.2.3", body.Version)
	}
	if body.Commit != "deadbeef" {
		t.Errorf("commit = %q, want deadbeef", body.Commit)
	}
}

// buildCommit must never report an empty string: an operator reading the health
// payload has to be able to tell "unknown build" from "no field at all".
func TestBuildCommitNeverEmpty(t *testing.T) {
	original := commit
	t.Cleanup(func() { commit = original })

	commit = "abc123"
	if got := buildCommit(); got != "abc123" {
		t.Errorf("buildCommit() = %q, want the ldflags value abc123", got)
	}

	commit = ""
	if got := buildCommit(); got == "" {
		t.Error("buildCommit() returned empty; want a VCS revision or \"unknown\"")
	}
}
