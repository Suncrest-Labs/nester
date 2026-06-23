package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetYieldOpportunities_Fresh(t *testing.T) {
	// Arrange: Create a mock server that returns valid yield data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"data": [
				{
					"pool": "pool1",
					"project": "proj1",
					"symbol": "USDC",
					"apy": 5.5,
					"apyBase": 5.0,
					"apyReward": 0.5,
					"tvlUsd": 1000000,
					"chain": "Stellar"
				}
			]
		}`))
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)

	// Act: Call GetYieldOpportunities
	resp, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 10)

	// Assert: Should get fresh data with stale=false
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if len(resp.Pools) != 1 {
		t.Fatalf("expected 1 pool, got %d", len(resp.Pools))
	}
	if resp.Meta.Stale {
		t.Fatal("expected stale=false for fresh data")
	}
	if resp.Pools[0].Pool != "pool1" {
		t.Fatalf("expected pool name 'pool1', got %s", resp.Pools[0].Pool)
	}
}

func TestGetYieldOpportunities_CachedResponse(t *testing.T) {
	// Arrange: Create a service with a preloaded cache
	svc := NewYieldService("http://not-called")
	cacheKey := "Stellar:10"
	pools := []YieldPool{
		{
			Pool:    "cached_pool",
			Project: "proj",
			Symbol:  "USDC",
			APY:     5.0,
		},
	}
	svc.cache[cacheKey] = yieldCacheEntry{
		pools:     pools,
		expiresAt: time.Now().Add(5 * time.Minute),
		fetchedAt: time.Now(),
	}

	// Act: Call GetYieldOpportunities (should hit cache, not network)
	resp, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 10)

	// Assert: Should get cached data with stale=false (because cache is still fresh)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Meta.Stale {
		t.Fatal("expected stale=false for fresh cached data")
	}
	if resp.Pools[0].Pool != "cached_pool" {
		t.Fatalf("expected cached pool, got %s", resp.Pools[0].Pool)
	}
}

func TestGetYieldOpportunities_StaleCacheOnError(t *testing.T) {
	// Arrange: Create a mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Service Unavailable"))
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)
	
	// Preload stale cache
	cacheKey := "Stellar:10"
	pools := []YieldPool{
		{
			Pool:    "stale_pool",
			Project: "proj",
			Symbol:  "USDC",
			APY:     4.5,
		},
	}
	fetchedAt := time.Now().Add(-10 * time.Minute) // 10 minutes old
	svc.cache[cacheKey] = yieldCacheEntry{
		pools:     pools,
		expiresAt: time.Now().Add(-5 * time.Minute), // already expired
		fetchedAt: fetchedAt,
	}

	// Act: Call GetYieldOpportunities (should fall back to stale cache)
	resp, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 10)

	// Assert: Should get stale data with stale=true
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if !resp.Meta.Stale {
		t.Fatal("expected stale=true when serving stale cache")
	}
	if resp.Pools[0].Pool != "stale_pool" {
		t.Fatalf("expected stale pool, got %s", resp.Pools[0].Pool)
	}
	if resp.Meta.FetchedAt == "" {
		t.Fatal("expected FetchedAt timestamp to be set")
	}
	// Verify the timestamp format
	if _, err := time.Parse(time.RFC3339, resp.Meta.FetchedAt); err != nil {
		t.Fatalf("FetchedAt should be RFC3339 formatted, got %s: %v", resp.Meta.FetchedAt, err)
	}
}

func TestGetYieldOpportunities_ErrorWhenStaleTooOld(t *testing.T) {
	// Arrange: Create a mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)
	
	// Preload very stale cache (older than 30 minutes)
	cacheKey := "Stellar:10"
	pools := []YieldPool{
		{
			Pool: "very_old_pool",
		},
	}
	fetchedAt := time.Now().Add(-40 * time.Minute) // 40 minutes old - too old!
	svc.cache[cacheKey] = yieldCacheEntry{
		pools:     pools,
		expiresAt: time.Now().Add(-35 * time.Minute),
		fetchedAt: fetchedAt,
	}

	// Act: Call GetYieldOpportunities (should fail, not use stale cache)
	resp, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 10)

	// Assert: Should return error because stale cache is too old
	if err == nil {
		t.Fatal("expected error when stale cache is too old")
	}
	if resp != nil {
		t.Fatal("expected nil response when error occurs")
	}
	// Verify error message indicates upstream error
	if !strings.Contains(err.Error(), "returned status") && !strings.Contains(err.Error(), "Internal") {
		t.Fatalf("expected upstream error message, got: %v", err)
	}
}

func TestGetYieldOpportunities_ErrorWhenNoCacheAvailable(t *testing.T) {
	// Arrange: Create a mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Service Unavailable"))
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)
	// No cache preloaded

	// Act: Call GetYieldOpportunities (should fail with no cache to fall back to)
	resp, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 10)

	// Assert: Should return error because no cache available
	if err == nil {
		t.Fatal("expected error when no cache available")
	}
	if resp != nil {
		t.Fatal("expected nil response when error occurs")
	}
}

func TestGetYieldOpportunities_CacheExpiredFetchNew(t *testing.T) {
	// Arrange: Create a mock server that returns valid data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"data": [
				{
					"pool": "new_pool",
					"project": "proj",
					"symbol": "USDC",
					"apy": 6.0,
					"tvlUsd": 2000000,
					"chain": "Stellar"
				}
			]
		}`))
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)
	
	// Preload expired cache
	cacheKey := "Stellar:10"
	oldPools := []YieldPool{
		{
			Pool: "old_pool",
		},
	}
	svc.cache[cacheKey] = yieldCacheEntry{
		pools:     oldPools,
		expiresAt: time.Now().Add(-1 * time.Minute), // already expired
		fetchedAt: time.Now().Add(-5 * time.Minute),
	}

	// Act: Call GetYieldOpportunities
	resp, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 10)

	// Assert: Should fetch new data, not use expired cache
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Meta.Stale {
		t.Fatal("expected stale=false when fresh data fetched")
	}
	if resp.Pools[0].Pool != "new_pool" {
		t.Fatalf("expected new pool, got %s", resp.Pools[0].Pool)
	}
}

func TestGetYieldOpportunities_FiltersChainCorrectly(t *testing.T) {
	// Arrange: Create a mock server that returns pools from multiple chains
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"data": [
				{
					"pool": "stellar_pool",
					"project": "proj1",
					"symbol": "USDC",
					"apy": 5.0,
					"tvlUsd": 1000000,
					"chain": "Stellar"
				},
				{
					"pool": "ethereum_pool",
					"project": "proj2",
					"symbol": "USDC",
					"apy": 4.0,
					"tvlUsd": 2000000,
					"chain": "Ethereum"
				}
			]
		}`))
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)

	// Act: Get pools for Stellar chain
	resp, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 10)

	// Assert: Should only get Stellar pools
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Pools) != 1 {
		t.Fatalf("expected 1 Stellar pool, got %d", len(resp.Pools))
	}
	if resp.Pools[0].Pool != "stellar_pool" {
		t.Fatalf("expected stellar_pool, got %s", resp.Pools[0].Pool)
	}
}

func TestGetYieldOpportunities_RespectLimitParameter(t *testing.T) {
	// Arrange: Create a mock server that returns multiple pools
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"data": [
				{"pool": "pool1", "chain": "Stellar", "apy": 5.0, "tvlUsd": 1000000},
				{"pool": "pool2", "chain": "Stellar", "apy": 4.0, "tvlUsd": 900000},
				{"pool": "pool3", "chain": "Stellar", "apy": 3.0, "tvlUsd": 800000},
				{"pool": "pool4", "chain": "Stellar", "apy": 2.0, "tvlUsd": 700000}
			]
		}`))
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)

	// Act: Request only 2 pools
	resp, err := svc.GetYieldOpportunities(context.Background(), "Stellar", 2)

	// Assert: Should only return top 2 by risk-adjusted APY
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(resp.Pools) != 2 {
		t.Fatalf("expected 2 pools with limit=2, got %d", len(resp.Pools))
	}
}

func TestFromStaleCache_WithinMaxAge(t *testing.T) {
	svc := NewYieldService("http://not-called")
	
	cacheKey := "test:key"
	pools := []YieldPool{{Pool: "test_pool"}}
	fetchedAt := time.Now().Add(-10 * time.Minute) // 10 minutes old
	
	svc.cache[cacheKey] = yieldCacheEntry{
		pools:     pools,
		expiresAt: time.Now().Add(-5 * time.Minute),
		fetchedAt: fetchedAt,
	}

	// Act
	result := svc.fromStaleCache(cacheKey)

	// Assert: Should return pools since age < 30 minutes
	if result == nil {
		t.Fatal("expected to get stale cache within max age")
	}
	if len(result) != 1 || result[0].Pool != "test_pool" {
		t.Fatal("expected correct pool from stale cache")
	}
}

func TestFromStaleCache_ExceedsMaxAge(t *testing.T) {
	svc := NewYieldService("http://not-called")
	
	cacheKey := "test:key"
	pools := []YieldPool{{Pool: "test_pool"}}
	fetchedAt := time.Now().Add(-40 * time.Minute) // 40 minutes old
	
	svc.cache[cacheKey] = yieldCacheEntry{
		pools:     pools,
		expiresAt: time.Now().Add(-35 * time.Minute),
		fetchedAt: fetchedAt,
	}

	// Act
	result := svc.fromStaleCache(cacheKey)

	// Assert: Should return nil since age > 30 minutes
	if result != nil {
		t.Fatal("expected nil for stale cache exceeding max age")
	}
}

func TestFromStaleCache_NonexistentKey(t *testing.T) {
	svc := NewYieldService("http://not-called")

	// Act
	result := svc.fromStaleCache("nonexistent:key")

	// Assert: Should return nil
	if result != nil {
		t.Fatal("expected nil for nonexistent cache key")
	}
}

func TestGetStaleFetchedAt_ReturnsRFC3339(t *testing.T) {
	svc := NewYieldService("http://not-called")
	
	cacheKey := "test:key"
	pools := []YieldPool{{Pool: "test_pool"}}
	now := time.Now()
	
	svc.cache[cacheKey] = yieldCacheEntry{
		pools:     pools,
		expiresAt: time.Now(),
		fetchedAt: now,
	}

	// Act
	timestamp := svc.getStaleFetchedAt(cacheKey)

	// Assert: Should return RFC3339 formatted timestamp
	if timestamp == "" {
		t.Fatal("expected timestamp string")
	}
	
	parsedTime, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		t.Fatalf("expected RFC3339 format, got %s: %v", timestamp, err)
	}
	
	// Check that parsed time is close to original (within 1 second tolerance)
	diff := parsedTime.Sub(now).Abs()
	if diff > time.Second {
		t.Fatalf("timestamp differs from original by %v", diff)
	}
}

func TestGetStaleFetchedAt_NonexistentKey(t *testing.T) {
	svc := NewYieldService("http://not-called")

	// Act
	timestamp := svc.getStaleFetchedAt("nonexistent:key")

	// Assert: Should return empty string
	if timestamp != "" {
		t.Fatalf("expected empty string for nonexistent key, got %s", timestamp)
	}
}

func TestFetchFromUpstream_InvalidJSON(t *testing.T) {
	// Arrange: Create a mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)

	// Act
	pools, err := svc.fetchFromUpstream(context.Background(), "Stellar", 10)

	// Assert: Should return error
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if pools != nil {
		t.Fatal("expected nil pools on error")
	}
}

func TestFetchFromUpstream_NetworkError(t *testing.T) {
	svc := NewYieldService("http://localhost:9999") // Nonexistent server

	// Act
	pools, err := svc.fetchFromUpstream(context.Background(), "Stellar", 10)

	// Assert: Should return error
	if err == nil {
		t.Fatal("expected error for network failure")
	}
	if pools != nil {
		t.Fatal("expected nil pools on error")
	}
	if !strings.Contains(err.Error(), "defillama request failed") {
		t.Fatalf("expected network error message, got: %v", err)
	}
}

func TestFetchFromUpstream_LargeResponse(t *testing.T) {
	// Arrange: Create a mock server that returns large response data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		
		// Build response with many pools
		response := `{"data": [`
		for i := 1; i <= 100; i++ {
			if i > 1 {
				response += ","
			}
			response += `{"pool": "pool` + string(rune(48+i%10)) + `", "chain": "Stellar", "apy": 5.0, "tvlUsd": 1000000}`
		}
		response += `]}`
		io.WriteString(w, response)
	}))
	defer server.Close()

	svc := NewYieldService(server.URL)

	// Act
	pools, err := svc.fetchFromUpstream(context.Background(), "Stellar", 10)

	// Assert: Should successfully parse large response and limit to 10
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(pools) != 10 {
		t.Fatalf("expected 10 pools with limit=10, got %d", len(pools))
	}
}
