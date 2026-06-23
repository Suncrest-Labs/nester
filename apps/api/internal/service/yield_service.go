package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// YieldPool represents a single DeFiLlama yield pool entry.
type YieldPool struct {
	Pool      string   `json:"pool"`
	Project   string   `json:"project"`
	Symbol    string   `json:"symbol"`
	APY       float64  `json:"apy"`
	APYBase   float64  `json:"apyBase"`
	APYReward float64  `json:"apyReward"`
	TVLUsd    float64  `json:"tvlUsd"`
	APYPct7d  *float64 `json:"apyPct7d"`
	Chain     string   `json:"chain"`
	RiskScore float64  `json:"riskScore"`
}

type yieldCacheEntry struct {
	pools     []YieldPool
	expiresAt time.Time
	fetchedAt time.Time
}

// YieldMeta contains metadata about the yield data (staleness, age, etc.)
type YieldMeta struct {
	Stale     bool   `json:"stale"`
	FetchedAt string `json:"fetched_at,omitempty"`
}

// YieldOpportunitiesResponse wraps pools with metadata about data freshness
type YieldOpportunitiesResponse struct {
	Pools []YieldPool `json:"data"`
	Meta  YieldMeta   `json:"meta"`
}

// YieldService aggregates DeFiLlama yield pool data for a given chain.
type YieldService struct {
	httpClient    *http.Client
	defiLlamaURL  string
	cacheMu       sync.Mutex
	cache         map[string]yieldCacheEntry
	cacheTTL      time.Duration
}

const defaultYieldCacheTTL = 5 * time.Minute
const maxStaleDataAge = 30 * time.Minute

func NewYieldService(defiLlamaURL string) *YieldService {
	if defiLlamaURL == "" {
		defiLlamaURL = "https://yields.llama.fi"
	}
	return &YieldService{
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		defiLlamaURL: defiLlamaURL,
		cache:        make(map[string]yieldCacheEntry),
		cacheTTL:     defaultYieldCacheTTL,
	}
}

type defiLlamaPoolsResponse struct {
	Data []struct {
		Pool      string   `json:"pool"`
		Project   string   `json:"project"`
		Symbol    string   `json:"symbol"`
		APY       *float64 `json:"apy"`
		APYBase   *float64 `json:"apyBase"`
		APYReward *float64 `json:"apyReward"`
		TVLUsd    *float64 `json:"tvlUsd"`
		APYPct7d  *float64 `json:"apyPct7d"`
		Chain     string   `json:"chain"`
	} `json:"data"`
}

// GetYieldOpportunities fetches pools for the given chain from DeFiLlama,
// scores them by risk-adjusted APY, and returns the top `limit` results.
// Falls back to stale cache (up to 30 minutes old) if upstream is unavailable.
func (s *YieldService) GetYieldOpportunities(ctx context.Context, chain string, limit int) (*YieldOpportunitiesResponse, error) {
	cacheKey := fmt.Sprintf("%s:%d", chain, limit)
	
	// Try fresh cache first
	if cached := s.fromCache(cacheKey); cached != nil {
		return &YieldOpportunitiesResponse{
			Pools: cached,
			Meta: YieldMeta{
				Stale: false,
			},
		}, nil
	}

	// Try to fetch from upstream
	pools, fetchErr := s.fetchFromUpstream(ctx, chain, limit)
	if fetchErr == nil {
		// Success: cache and return
		s.toCache(cacheKey, pools)
		return &YieldOpportunitiesResponse{
			Pools: pools,
			Meta: YieldMeta{
				Stale: false,
			},
		}, nil
	}

	// Upstream failed: try stale cache
	if stale := s.fromStaleCache(cacheKey); stale != nil {
		return &YieldOpportunitiesResponse{
			Pools: stale,
			Meta: YieldMeta{
				Stale:     true,
				FetchedAt: s.getStaleFetchedAt(cacheKey),
			},
		}, nil
	}

	// No data available at all
	return nil, fetchErr
}

// fetchFromUpstream retrieves and processes yield data from DeFiLlama
func (s *YieldService) fetchFromUpstream(ctx context.Context, chain string, limit int) ([]YieldPool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.defiLlamaURL+"/pools", nil)
	if err != nil {
		return nil, fmt.Errorf("build defillama request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("defillama request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("defillama returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read defillama response: %w", err)
	}

	var raw defiLlamaPoolsResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse defillama response: %w", err)
	}

	pools := make([]YieldPool, 0, 64)
	for _, p := range raw.Data {
		if p.Chain != chain {
			continue
		}
		pool := YieldPool{
			Pool:    p.Pool,
			Project: p.Project,
			Symbol:  p.Symbol,
			Chain:   p.Chain,
		}
		if p.APY != nil {
			pool.APY = *p.APY
		}
		if p.APYBase != nil {
			pool.APYBase = *p.APYBase
		}
		if p.APYReward != nil {
			pool.APYReward = *p.APYReward
		}
		if p.TVLUsd != nil {
			pool.TVLUsd = *p.TVLUsd
		}
		pool.APYPct7d = p.APYPct7d
		pool.RiskScore = riskScore(pool)
		pools = append(pools, pool)
	}

	// Sort by risk-adjusted APY descending.
	sort.Slice(pools, func(i, j int) bool {
		return riskAdjustedAPY(pools[i]) > riskAdjustedAPY(pools[j])
	})

	if limit > 0 && len(pools) > limit {
		pools = pools[:limit]
	}

	return pools, nil
}

// riskScore assigns a 0-100 risk score (lower = safer).
// Uses TVL as a proxy for protocol maturity.
func riskScore(p YieldPool) float64 {
	if p.TVLUsd >= 10_000_000 {
		return 20
	}
	if p.TVLUsd >= 1_000_000 {
		return 40
	}
	if p.TVLUsd >= 100_000 {
		return 60
	}
	return 80
}

// riskAdjustedAPY penalises high-risk, low-TVL pools.
func riskAdjustedAPY(p YieldPool) float64 {
	if p.APY <= 0 {
		return 0
	}
	penalty := (p.RiskScore / 100) * 0.5
	return p.APY * (1 - penalty)
}

// fromCache returns cached pools if they exist and are not expired
func (s *YieldService) fromCache(key string) []YieldPool {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.pools
}

// fromStaleCache returns cached pools if they exist (regardless of expiry)
// and are not older than maxStaleDataAge
func (s *YieldService) fromStaleCache(key string) []YieldPool {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return nil
	}
	// Check if stale data is too old (older than 30 minutes)
	if time.Since(entry.fetchedAt) > maxStaleDataAge {
		return nil
	}
	return entry.pools
}

// getStaleFetchedAt returns the RFC3339 formatted timestamp of when the cache was last fetched
func (s *YieldService) getStaleFetchedAt(key string) string {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return ""
	}
	return entry.fetchedAt.Format(time.RFC3339)
}

// toCache stores pools in cache with current timestamps
func (s *YieldService) toCache(key string, pools []YieldPool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache[key] = yieldCacheEntry{
		pools:     pools,
		expiresAt: time.Now().Add(s.cacheTTL),
		fetchedAt: time.Now(),
	}
}
