package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Suncrest-Labs/nester/internal/config"
	"github.com/Suncrest-Labs/nester/internal/domain/intelligence"
	"github.com/stretchr/testify/assert"
)

func TestPrometheusClient_Caching(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		recs := []intelligence.Recommendation{{Title: "Test"}}
		_ = json.NewEncoder(w).Encode(recs)
	}))
	defer server.Close()

	cfg := config.PrometheusConfig{
		BaseURL: server.URL,
		Timeout: 1 * time.Second,
	}
	client := NewPrometheusClient(cfg)

	recs, err := client.GetVaultRecommendations(context.Background(), "v1")
	assert.NoError(t, err)
	assert.Len(t, recs, 1)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))

	recs, err = client.GetVaultRecommendations(context.Background(), "v1")
	assert.NoError(t, err)
	assert.Len(t, recs, 1)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

func TestPrometheusClient_PathEscaping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/vaults/v%2F1/recommendations", r.URL.EscapedPath())
		recs := []intelligence.Recommendation{{Title: "Escaped"}}
		_ = json.NewEncoder(w).Encode(recs)
	}))
	defer server.Close()

	cfg := config.PrometheusConfig{
		BaseURL: server.URL,
		Timeout: 1 * time.Second,
	}
	client := NewPrometheusClient(cfg)

	_, err := client.GetVaultRecommendations(context.Background(), "v/1")
	assert.NoError(t, err)
}

func TestPrometheusClient_CircuitBreaker(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.PrometheusConfig{
		BaseURL: server.URL,
		Timeout: 100 * time.Millisecond,
	}
	client := NewPrometheusClient(cfg)

	for i := 0; i < 5; i++ {
		_, err := client.GetVaultRecommendations(context.Background(), "v1")
		assert.Error(t, err)
	}

	prevCalls := atomic.LoadInt32(&callCount)
	_, err := client.GetVaultRecommendations(context.Background(), "v1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit open")
	assert.Equal(t, prevCalls, atomic.LoadInt32(&callCount))
}

func TestPrometheusClient_Retry(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		recs := []intelligence.Recommendation{{Title: "Success After Retry"}}
		_ = json.NewEncoder(w).Encode(recs)
	}))
	defer server.Close()

	cfg := config.PrometheusConfig{
		BaseURL: server.URL,
		Timeout: 1 * time.Second,
	}
	client := NewPrometheusClient(cfg)

	recs, err := client.GetVaultRecommendations(context.Background(), "v1")
	assert.NoError(t, err)
	assert.Len(t, recs, 1)
	assert.Equal(t, "Success After Retry", recs[0].Title)
	assert.Equal(t, int32(3), atomic.LoadInt32(&callCount))
}

func TestPrometheusClient_URLSanitization(t *testing.T) {
	var requestedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("[]"))
	}))
	defer server.Close()

	cfg := config.PrometheusConfig{
		BaseURL: server.URL,
		Timeout: 1 * time.Second,
	}
	client := NewPrometheusClient(cfg)

	// Test with a vaultID that contains characters requiring escaping
	vaultID := "vault/123?query=true"
	_, err := client.GetVaultRecommendations(context.Background(), vaultID)
	assert.NoError(t, err)

	// Check if the vaultID was properly escaped in the URL
	// The expected URL should contain the escaped version of vaultID
	expectedPath := "/api/v1/vaults/" + url.PathEscape(vaultID) + "/recommendations"
	assert.Contains(t, requestedURL, expectedPath)
}
