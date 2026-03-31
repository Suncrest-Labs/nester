package stellar

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

const defaultDeFiLlamaBaseURL = "https://yields.llama.fi"

type APYSnapshot struct {
	APYBPS    uint32
	UpdatedAt time.Time
}

type DeFiLlamaClient struct {
	baseURL    string
	httpClient *http.Client
	now        func() time.Time
}

type DeFiLlamaPoolsResponse struct {
	Status string          `json:"status"`
	Data   []DeFiLlamaPool `json:"data"`
}

type DeFiLlamaPool struct {
	Pool      string  `json:"pool"`
	APY       float64 `json:"apy"`
	Timestamp string  `json:"timestamp"`
}

func NewDeFiLlamaClient(httpClient *http.Client, baseURL string) *DeFiLlamaClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultDeFiLlamaBaseURL
	}

	return &DeFiLlamaClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		now:        time.Now,
	}
}

func (c *DeFiLlamaClient) GetPools(ctx context.Context) ([]DeFiLlamaPool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/pools", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("defillama returned status %d", resp.StatusCode)
	}

	var payload DeFiLlamaPoolsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode defillama pools payload: %w", err)
	}

	return payload.Data, nil
}

func (c *DeFiLlamaClient) APYByPool(ctx context.Context, poolIDs []string) (map[string]APYSnapshot, error) {
	pools, err := c.GetPools(ctx)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]DeFiLlamaPool, len(pools))
	for _, pool := range pools {
		byID[pool.Pool] = pool
	}

	out := make(map[string]APYSnapshot, len(poolIDs))
	for _, poolID := range poolIDs {
		pool, ok := byID[poolID]
		if !ok {
			return nil, fmt.Errorf("pool %q not found in defillama response", poolID)
		}

		updatedAt := c.now().UTC()
		if strings.TrimSpace(pool.Timestamp) != "" {
			if ts, parseErr := time.Parse(time.RFC3339, pool.Timestamp); parseErr == nil {
				updatedAt = ts.UTC()
			}
		}

		apyBPS := uint32(math.Round(pool.APY * 100))
		out[poolID] = APYSnapshot{
			APYBPS:    apyBPS,
			UpdatedAt: updatedAt,
		}
	}

	return out, nil
}
