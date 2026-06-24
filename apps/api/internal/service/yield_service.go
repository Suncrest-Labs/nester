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
func (s *YieldService) GetYieldOpportunities(ctx context.Context, chain string, limit int) ([]YieldPool, error) {
	cacheKey := fmt.Sprintf("%s:%d", chain, limit)
	if cached := s.fromCache(cacheKey); cached != nil {
		return cached, nil
	}

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

		var apy7dSwing float64
		if p.APYPct7d != nil {
			apy7dSwing = *p.APYPct7d
		}
		var rewardRatio float64
		if pool.APY > 0 {
			rewardRatio = pool.APYReward / pool.APY
		}
		pool.RiskScore = computeRiskScore(pool.TVLUsd, apy7dSwing, rewardRatio)
		pools = append(pools, pool)
	}

	// Sort by risk-adjusted APY descending.
	sort.Slice(pools, func(i, j int) bool {
		return riskAdjustedAPY(pools[i]) > riskAdjustedAPY(pools[j])
	})

	if limit > 0 && len(pools) > limit {
		pools = pools[:limit]
	}

	s.toCache(cacheKey, pools)
	return pools, nil
}

// computeRiskScore derives a deterministic risk score in [0.0, 1.0] from three
// DeFiLlama signals:
//   - tvl < $100k → +0.4 (low liquidity, higher protocol risk)
//   - |apy7dSwing| > 20% → +0.3 (high volatility)
//   - rewardRatio > 0.8 → +0.2 (incentive-heavy pools are less sustainable)
//
// The result is clamped to [0.0, 1.0]. Lower score = safer pool.
func computeRiskScore(tvl, apy7dSwing, rewardRatio float64) float64 {
	var score float64
	if tvl < 100_000 {
		score += 0.4
	}
	if apy7dSwing > 20 || apy7dSwing < -20 {
		score += 0.3
	}
	if rewardRatio > 0.8 {
		score += 0.2
	}
	if score > 1.0 {
		return 1.0
	}
	return score
}

// riskAdjustedAPY penalises high-risk, low-TVL pools.
// RiskScore is in [0.0, 1.0]; a score of 1.0 halves the effective APY.
func riskAdjustedAPY(p YieldPool) float64 {
	if p.APY <= 0 {
		return 0
	}
	penalty := p.RiskScore * 0.5
	return p.APY * (1 - penalty)
}

func (s *YieldService) fromCache(key string) []YieldPool {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	entry, ok := s.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.pools
}

func (s *YieldService) toCache(key string, pools []YieldPool) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache[key] = yieldCacheEntry{pools: pools, expiresAt: time.Now().Add(s.cacheTTL)}
}
