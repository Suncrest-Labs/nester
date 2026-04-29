package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/Suncrest-Labs/nester/internal/config"
	"github.com/Suncrest-Labs/nester/internal/domain/intelligence"
)

const (
	MaxFailures         = 5
	FailureWindow       = 30 * time.Second
	CircuitOpenDuration = 60 * time.Second
	VaultCacheTTL       = 2 * time.Minute
	MarketCacheTTL      = 5 * time.Minute
	PortfolioCacheTTL   = 5 * time.Minute
)

type cacheEntry struct {
	data      any
	expiresAt time.Time
}

type PrometheusClient struct {
	cfg        config.PrometheusConfig
	httpClient *http.Client

	cache   map[string]cacheEntry
	cacheMu sync.RWMutex

	failures         []time.Time
	circuitOpenUntil time.Time
	breakerMu        sync.Mutex
}

func NewPrometheusClient(cfg config.PrometheusConfig) *PrometheusClient {
	return &PrometheusClient{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		cache: make(map[string]cacheEntry),
	}
}

func (c *PrometheusClient) GetVaultRecommendations(ctx context.Context, vaultID string) ([]intelligence.Recommendation, error) {
	key := fmt.Sprintf("vault:%s", vaultID)
	if val, ok := c.getFromCache(key); ok {
		return val.([]intelligence.Recommendation), nil
	}

	if !c.canCall() {
		return nil, fmt.Errorf("prometheus service unavailable (circuit open)")
	}

	endpoint := fmt.Sprintf(
		"%s/api/v1/vaults/%s/recommendations",
		c.cfg.BaseURL,
		url.PathEscape(vaultID),
	)
	var recs []intelligence.Recommendation
	err := c.doRequest(ctx, endpoint, &recs)
	if err != nil {
		log.Printf("ERROR: prometheus_client request failed for vault %s: %v", vaultID, err)
		c.recordFailure()
		return nil, fmt.Errorf("failed to get vault recommendations: %w", err)
	}

	c.setCache(key, recs, VaultCacheTTL)
	return recs, nil
}

func (c *PrometheusClient) GetMarketSentiment(ctx context.Context) (*intelligence.SentimentReport, error) {
	key := "market:sentiment"
	if val, ok := c.getFromCache(key); ok {
		return val.(*intelligence.SentimentReport), nil
	}

	if !c.canCall() {
		return nil, fmt.Errorf("prometheus service unavailable (circuit open)")
	}

	endpoint := fmt.Sprintf("%s/api/v1/intelligence/market", c.cfg.BaseURL)
	var report intelligence.SentimentReport
	err := c.doRequest(ctx, endpoint, &report)
	if err != nil {
		log.Printf("ERROR: prometheus_client request failed for market sentiment: %v", err)
		c.recordFailure()
		return nil, fmt.Errorf("failed to get market sentiment: %w", err)
	}

	c.setCache(key, &report, MarketCacheTTL)
	return &report, nil
}

func (c *PrometheusClient) GetPortfolioInsights(ctx context.Context, userID string) (*intelligence.PortfolioInsights, error) {
	key := fmt.Sprintf("user:%s:insights", userID)
	if val, ok := c.getFromCache(key); ok {
		return val.(*intelligence.PortfolioInsights), nil
	}

	if !c.canCall() {
		return nil, fmt.Errorf("prometheus service unavailable (circuit open)")
	}

	endpoint := fmt.Sprintf(
		"%s/api/v1/users/%s/insights",
		c.cfg.BaseURL,
		url.PathEscape(userID),
	)
	var insights intelligence.PortfolioInsights
	err := c.doRequest(ctx, endpoint, &insights)
	if err != nil {
		log.Printf("ERROR: prometheus_client request failed for user %s insights: %v", userID, err)
		c.recordFailure()
		return nil, fmt.Errorf("failed to get portfolio insights: %w", err)
	}

	c.setCache(key, &insights, PortfolioCacheTTL)
	return &insights, nil
}

func (c *PrometheusClient) doRequest(ctx context.Context, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}

	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.cfg.APIKey))
	}

	var resp *http.Response
	for i := 0; i < 3; i++ {
		resp, err = c.httpClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			return json.NewDecoder(resp.Body).Decode(target)
		}
		if i < 2 {
			time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
		}
	}

	if err != nil {
		return err
	}
	if resp != nil {
		resp.Body.Close()
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return fmt.Errorf("request failed")
}

func (c *PrometheusClient) getFromCache(key string) (any, bool) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()

	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.data, true
}

func (c *PrometheusClient) setCache(key string, data any, ttl time.Duration) {
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()

	c.cache[key] = cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(ttl),
	}
}

func (c *PrometheusClient) canCall() bool {
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()

	return !time.Now().Before(c.circuitOpenUntil)
}

func (c *PrometheusClient) recordFailure() {
	c.breakerMu.Lock()
	defer c.breakerMu.Unlock()

	now := time.Now()
	c.failures = append(c.failures, now)

	windowStart := now.Add(-FailureWindow)
	validFailures := make([]time.Time, 0, len(c.failures))
	for _, failure := range c.failures {
		if failure.After(windowStart) {
			validFailures = append(validFailures, failure)
		}
	}
	c.failures = validFailures

	if len(c.failures) >= MaxFailures {
		c.circuitOpenUntil = now.Add(CircuitOpenDuration)
		c.failures = nil
	}
}
