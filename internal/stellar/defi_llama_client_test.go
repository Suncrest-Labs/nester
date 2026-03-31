package stellar

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeFiLlamaClientAPYByPool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"status":"success",
			"data":[
				{"pool":"pool-aave","apy":5.25,"timestamp":"2026-03-29T10:00:00Z"},
				{"pool":"pool-blend","apy":8.10,"timestamp":"2026-03-29T10:01:00Z"}
			]
		}`))
	}))
	defer server.Close()

	client := NewDeFiLlamaClient(server.Client(), server.URL)
	got, err := client.APYByPool(context.Background(), []string{"pool-aave", "pool-blend"})
	if err != nil {
		t.Fatalf("APYByPool() error = %v", err)
	}

	if got["pool-aave"].APYBPS != 525 {
		t.Fatalf("pool-aave APYBPS = %d, want 525", got["pool-aave"].APYBPS)
	}
	if got["pool-blend"].APYBPS != 810 {
		t.Fatalf("pool-blend APYBPS = %d, want 810", got["pool-blend"].APYBPS)
	}
	wantAaveTS := time.Date(2026, 3, 29, 10, 0, 0, 0, time.UTC)
	if !got["pool-aave"].UpdatedAt.Equal(wantAaveTS) {
		t.Fatalf("pool-aave UpdatedAt = %s, want %s", got["pool-aave"].UpdatedAt, wantAaveTS)
	}
}

func TestDeFiLlamaClientAPYByPoolMissingPool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":[{"pool":"pool-aave","apy":5.25}]}`))
	}))
	defer server.Close()

	client := NewDeFiLlamaClient(server.Client(), server.URL)
	_, err := client.APYByPool(context.Background(), []string{"pool-missing"})
	if err == nil {
		t.Fatal("expected missing-pool error, got nil")
	}
}
