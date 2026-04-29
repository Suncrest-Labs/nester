package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DefiLlamaProvider fetches the XLM/USD price from the DeFiLlama coins API.
// It requires no API key and serves as the fallback when Horizon is unavailable.
type DefiLlamaProvider struct {
	client *http.Client
}

// NewDefiLlamaProvider returns a DefiLlamaProvider with a default HTTP client.
func NewDefiLlamaProvider() *DefiLlamaProvider {
	return &DefiLlamaProvider{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *DefiLlamaProvider) Name() string { return "defillama" }

func (p *DefiLlamaProvider) Fetch(ctx context.Context, base, quote string) (float64, error) {
	if base != "XLM" || quote != "USD" {
		return 0, ErrUnsupportedPair
	}

	const url = "https://coins.llama.fi/prices/current/coingecko:stellar"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("defillama: build request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("defillama: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("defillama: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Coins map[string]struct {
			Price float64 `json:"price"`
		} `json:"coins"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("defillama: decode: %w", err)
	}

	coin, ok := body.Coins["coingecko:stellar"]
	if !ok || coin.Price <= 0 {
		return 0, fmt.Errorf("defillama: XLM price not available")
	}

	return coin.Price, nil
}
