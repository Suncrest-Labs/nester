package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
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
	RiskScore float64  `json:"risk_score"`
	// RiskTier is the human-facing bucket of RiskScore: "low", "medium", or
	// "high". Lets clients filter by risk appetite without re-deriving the
	// thresholds. See RiskTierForScore.
	RiskTier string `json:"risk_tier"`
}

// Risk tiers, bucketed from a [0.0, 1.0] risk score viewed as a 0–100 scale:
// Low (0–33), Medium (34–66), High (67–100).
const (
	RiskTierLow    = "low"
	RiskTierMedium = "medium"
	RiskTierHigh   = "high"
)

// IsValidRiskTier reports whether s names one of the known risk tiers.
func IsValidRiskTier(s string) bool {
	switch s {
	case RiskTierLow, RiskTierMedium, RiskTierHigh:
		return true
	default:
		return false
	}
}

// RiskTierForScore buckets a [0.0, 1.0] risk score into a tier. The score is
// read as a 0–100 percentage: Low (0–33), Medium (34–66), High (67–100).
func RiskTierForScore(score float64) string {
	pct := math.Round(score * 100)
	switch {
	case pct <= 33:
		return RiskTierLow
	case pct <= 66:
		return RiskTierMedium
	default:
		return RiskTierHigh
	}
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
	minTVLUsd     float64
}

const defaultYieldCacheTTL = 5 * time.Minute
const maxStaleDataAge = 30 * time.Minute

func NewYieldService(defiLlamaURL string) *YieldService {
	if defiLlamaURL == "" {
		defiLlamaURL = "https://yields.llama.fi"
	}
	minTVL := 100_000.0
	if v := os.Getenv("YIELD_MIN_TVL_USD"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			minTVL = parsed
		}
	}
	return &YieldService{
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		defiLlamaURL: defiLlamaURL,
		cache:        make(map[string]yieldCacheEntry),
		cacheTTL:     defaultYieldCacheTTL,
		minTVLUsd:    minTVL,
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
	if limit > 100 { limit = 100 }
	chain = normalizeChain(chain)
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

// WarmCache pre-populates the Stellar yield cache so the first request after a
// cold start doesn't pay the full DeFiLlama round-trip (#667). It returns the
// number of pools cached. Failures are non-fatal: callers log and rely on the
// lazy-load path.
func (s *YieldService) WarmCache(ctx context.Context) (int, error) {
	resp, err := s.GetYieldOpportunities(ctx, "Stellar", 100)
	if err != nil {
		return 0, err
	}
	return len(resp.Pools), nil
}

// GetYieldOpportunitiesByTier returns up to `limit` opportunities on `chain`
// whose risk score falls in `tier` ("low"|"medium"|"high"). An empty tier means
// "all tiers" and behaves exactly like GetYieldOpportunities.
//
// When a tier is given, it filters the full scored set for the chain (fetched
// with limit=0) and only then applies the limit, so callers still receive up to
// `limit` opportunities of the requested tier rather than whatever survived a
// pre-filter truncation. Risk scoring — and therefore this filter — runs after
// the DeFiLlama TVL threshold check in fetchFromUpstream.
func (s *YieldService) GetYieldOpportunitiesByTier(ctx context.Context, chain string, limit int, tier string) (*YieldOpportunitiesResponse, error) {
	tier = strings.ToLower(strings.TrimSpace(tier))
	if tier == "" {
		return s.GetYieldOpportunities(ctx, chain, limit)
	}

	resp, err := s.GetYieldOpportunities(ctx, chain, 0)
	if err != nil {
		return nil, err
	}

	filtered := make([]YieldPool, 0, len(resp.Pools))
	for _, p := range resp.Pools {
		if RiskTierForScore(p.RiskScore) == tier {
			filtered = append(filtered, p)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return &YieldOpportunitiesResponse{Pools: filtered, Meta: resp.Meta}, nil
}

// ProtocolSummary is an aggregated, comparable view of one protocol's pools.
type ProtocolSummary struct {
	Protocol  string  `json:"protocol"`
	Found     bool    `json:"found"`
	BestAPY   float64 `json:"best_apy"`
	BaseAPY   float64 `json:"base_apy"`
	RewardAPY float64 `json:"reward_apy"`
	TVLUsd    float64 `json:"tvl_usd"`
	APYPct7d  float64 `json:"apy_pct_7d"`
	RiskScore float64 `json:"risk_score"`
	PoolCount int     `json:"pool_count"`
	TopPool   string  `json:"top_pool_symbol"`
}

const (
	minCompareProtocols = 2
	maxCompareProtocols = 5
)

// ErrInvalidProtocolCount is returned when fewer than 2 or more than 5
// protocols are requested for comparison.
var ErrInvalidProtocolCount = errors.New("protocols must be between 2 and 5")

// CompareProtocols returns a side-by-side summary for each requested protocol.
// It aggregates from the shared DeFiLlama pool cache (a single fetch for the
// whole comparison — no per-protocol HTTP calls). Protocol names are
// case-insensitive; unknown protocols are returned with Found=false rather
// than failing the request.
func (s *YieldService) CompareProtocols(ctx context.Context, protocols []string) ([]ProtocolSummary, error) {
	if len(protocols) < minCompareProtocols || len(protocols) > maxCompareProtocols {
		return nil, ErrInvalidProtocolCount
	}

	// limit=0 → all pools for the chain, served from the shared cache.
	resp, err := s.GetYieldOpportunities(ctx, "Stellar", 0)
	if err != nil {
		return nil, err
	}

	byProtocol := make(map[string][]YieldPool)
	for _, p := range resp.Pools {
		key := strings.ToLower(p.Project)
		byProtocol[key] = append(byProtocol[key], p)
	}

	summaries := make([]ProtocolSummary, 0, len(protocols))
	for _, name := range protocols {
		key := strings.ToLower(strings.TrimSpace(name))
		pools := byProtocol[key]
		if len(pools) == 0 {
			summaries = append(summaries, ProtocolSummary{Protocol: name, Found: false})
			continue
		}
		summaries = append(summaries, summarizeProtocol(name, pools))
	}
	return summaries, nil
}

// summarizeProtocol reduces a protocol's pools to a single comparison row.
// BestAPY (and the base/reward/7d/risk/top-pool fields tied to it) come from
// the highest-APY pool; TVLUsd is summed across all of the protocol's pools.
func summarizeProtocol(name string, pools []YieldPool) ProtocolSummary {
	best := pools[0]
	var tvl float64
	for _, p := range pools {
		tvl += p.TVLUsd
		if p.APY > best.APY {
			best = p
		}
	}
	var apy7d float64
	if best.APYPct7d != nil {
		apy7d = *best.APYPct7d
	}
	return ProtocolSummary{
		Protocol:  name,
		Found:     true,
		BestAPY:   best.APY,
		BaseAPY:   best.APYBase,
		RewardAPY: best.APYReward,
		TVLUsd:    tvl,
		APYPct7d:  apy7d,
		RiskScore: best.RiskScore,
		PoolCount: len(pools),
		TopPool:   best.Symbol,
	}
}

// ProtocolTVL returns the current aggregate TVL (USD) for a protocol by summing
// all pools matching the slug. It satisfies the scheduler.ProtocolTVLFetcher interface.
func (s *YieldService) ProtocolTVL(ctx context.Context, protocolSlug string) (float64, error) {
	resp, err := s.GetYieldOpportunities(ctx, "Stellar", 0)
	if err != nil {
		return 0, err
	}
	key := strings.ToLower(strings.TrimSpace(protocolSlug))
	var total float64
	for _, p := range resp.Pools {
		if strings.ToLower(p.Project) == key {
			total += p.TVLUsd
		}
	}
	return total, nil
}

// ProtocolDetail is an enriched summary of a single protocol with a 24h TVL change field.
type ProtocolDetail struct {
	ProtocolSummary
	TVLChange24hPct *float64 `json:"tvl_change_24h"`
}

// GetProtocol returns detail for a single protocol slug, without requiring comparison of
// multiple protocols (unlike CompareProtocols which needs 2–5).
func (s *YieldService) GetProtocol(ctx context.Context, slug string) (*ProtocolDetail, error) {
	resp, err := s.GetYieldOpportunities(ctx, "Stellar", 0)
	if err != nil {
		return nil, err
	}
	key := strings.ToLower(strings.TrimSpace(slug))
	var pools []YieldPool
	for _, p := range resp.Pools {
		if strings.ToLower(p.Project) == key {
			pools = append(pools, p)
		}
	}
	if len(pools) == 0 {
		return nil, fmt.Errorf("protocol %q not found", slug)
	}
	summary := summarizeProtocol(slug, pools)
	return &ProtocolDetail{ProtocolSummary: summary}, nil
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
	totalFetched := len(raw.Data)
	afterChainFilter := 0
	afterTVLFilter := 0
	for _, p := range raw.Data {
		if !strings.EqualFold(p.Chain, chain) {
			continue
		}
		afterChainFilter++
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
		if pool.TVLUsd < s.minTVLUsd {
			continue
		}
		afterTVLFilter++
		pool.APYPct7d = p.APYPct7d

		var apy7dSwing float64
		if p.APYPct7d != nil {
			apy7dSwing = *p.APYPct7d
		}
		var rewardRatio float64
		if pool.APY > 0 {
			rewardRatio = pool.APYReward / pool.APY
		}
		pool.RiskScore = ComputeRiskScore(pool.TVLUsd, apy7dSwing, rewardRatio)
		pool.RiskTier = RiskTierForScore(pool.RiskScore)
		pools = append(pools, pool)
	}
	slog.Debug("yield pools filtered",
		"total_fetched", totalFetched,
		"after_chain_filter", afterChainFilter,
		"after_tvl_filter", afterTVLFilter,
	)

	// Sort by risk-adjusted APY descending.
	sort.Slice(pools, func(i, j int) bool {
		return riskAdjustedAPY(pools[i]) > riskAdjustedAPY(pools[j])
	})

	if limit > 0 && len(pools) > limit {
		pools = pools[:limit]
	}

	return pools, nil
}

// ComputeRiskScore returns a risk score in [0.0, 1.0] from multiple signals.
// Each signal contributes additively; the result is clamped so it never
// exceeds 1.0 or drops below 0.0.
//
//   - tvlUsd < 100_000 adds 0.4 (low-liquidity penalty)
//   - |apy7dSwing| > 20 adds 0.3 (high APY volatility penalty)
//   - rewardRatio > 0.8 adds 0.2 (heavy reward-token dependency penalty)
func ComputeRiskScore(tvlUsd, apy7dSwing, rewardRatio float64) float64 {
	var score float64
	if tvlUsd < 100_000 {
		score += 0.4
	}
	if math.Abs(apy7dSwing) > 20 {
		score += 0.3
	}
	if rewardRatio > 0.8 {
		score += 0.2
	}
	// Round to 1 decimal place to eliminate IEEE 754 accumulation errors
	// (e.g. 0.4+0.3+0.2 = 0.8999... without rounding).
	score = math.Round(score*10) / 10
	if score > 1.0 {
		return 1.0
	}
	if score < 0.0 {
		return 0.0
	}
	return score
}

// riskScore computes a normalised [0.0, 1.0] risk score for a pool by
// delegating to ComputeRiskScore with the pool's TVL, 7-day APY swing, and
// reward-to-total-APY ratio.
func riskScore(p YieldPool) float64 {
	var apy7dSwing float64
	if p.APYPct7d != nil {
		apy7dSwing = *p.APYPct7d
	}
	var rewardRatio float64
	if p.APY > 0 {
		rewardRatio = p.APYReward / p.APY
	}
	return ComputeRiskScore(p.TVLUsd, apy7dSwing, rewardRatio)
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

// normalizeChain trims whitespace and lower-cases the chain parameter
// to match DeFiLlama's format for comparison purposes.
func normalizeChain(chain string) string {
	return strings.ToLower(strings.TrimSpace(chain))
}
