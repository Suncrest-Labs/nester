package oracle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// usdcIssuer is Circle's USDC issuer on the Stellar network.
const usdcIssuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"

// StellarProvider fetches the XLM/USDC mid-market rate from Horizon's order book.
type StellarProvider struct {
	horizonURL string
	client     *http.Client
}

// NewStellarProvider returns a StellarProvider using the given Horizon base URL.
func NewStellarProvider(horizonURL string) *StellarProvider {
	return &StellarProvider{
		horizonURL: horizonURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *StellarProvider) Name() string { return "horizon" }

func (p *StellarProvider) Fetch(ctx context.Context, base, quote string) (float64, error) {
	if base != "XLM" || quote != "USD" {
		return 0, ErrUnsupportedPair
	}

	url := fmt.Sprintf(
		"%s/order_book?selling_asset_type=native"+
			"&buying_asset_type=credit_alphanum4"+
			"&buying_asset_code=USDC"+
			"&buying_asset_issuer=%s"+
			"&limit=1",
		p.horizonURL, usdcIssuer,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("stellar: build request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("stellar: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("stellar: unexpected status %d", resp.StatusCode)
	}

	var body struct {
		Bids []struct {
			Price string `json:"price"`
		} `json:"bids"`
		Asks []struct {
			Price string `json:"price"`
		} `json:"asks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("stellar: decode: %w", err)
	}

	if len(body.Bids) == 0 || len(body.Asks) == 0 {
		return 0, fmt.Errorf("stellar: order book is empty")
	}

	bid, err := strconv.ParseFloat(body.Bids[0].Price, 64)
	if err != nil {
		return 0, fmt.Errorf("stellar: parse bid: %w", err)
	}
	ask, err := strconv.ParseFloat(body.Asks[0].Price, 64)
	if err != nil {
		return 0, fmt.Errorf("stellar: parse ask: %w", err)
	}

	// Mid-market: price is USDC per XLM ≈ USD per XLM since USDC is pegged 1:1.
	return (bid + ask) / 2, nil
}
