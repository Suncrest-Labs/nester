package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// supportedFiatQuotes are the fiat currencies supported as quote against USD.
var supportedFiatQuotes = map[string]bool{
	"NGN": true,
	"GHS": true,
	"KES": true,
	"USD": true,
}

// FiatProvider fetches USD-based fiat exchange rates from the open.er-api.com
// free tier (no API key required).
type FiatProvider struct {
	client *http.Client
}

// NewFiatProvider returns a FiatProvider with a default HTTP client.
func NewFiatProvider() *FiatProvider {
	return &FiatProvider{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *FiatProvider) Name() string { return "forex" }

func (p *FiatProvider) Fetch(ctx context.Context, base, quote string) (float64, error) {
	if base != "USD" {
		return 0, ErrUnsupportedPair
	}
	if !supportedFiatQuotes[quote] {
		return 0, ErrUnsupportedPair
	}
	if quote == "USD" {
		return 1.0, nil
	}

	const url = "https://open.er-api.com/v6/latest/USD"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("forex: build request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("forex: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("forex: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("forex: decode: %w", err)
	}

	if body.Result != "success" {
		return 0, fmt.Errorf("forex: API returned result=%q", body.Result)
	}

	rate, ok := body.Rates[quote]
	if !ok || rate <= 0 {
		return 0, fmt.Errorf("forex: rate not found for %s", quote)
	}

	return rate, nil
}
