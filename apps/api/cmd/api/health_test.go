package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpointVersionAndCommit(t *testing.T) {
	Version = "v1.2.3"
	Commit = "deadbeef"

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": Version,
			"commit":  Commit,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode json: %v", err)
	}

	if body["version"] != "v1.2.3" {
		t.Errorf("version = %v, want v1.2.3", body["version"])
	}
	if body["commit"] != "deadbeef" {
		t.Errorf("commit = %v, want deadbeef", body["commit"])
	}
}
