package stellar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AccountBalance reports an account's native XLM balance by querying Horizon's
// /accounts/{id} endpoint directly (no SDK dependency, matching the plain
// net/http + JSON decode pattern used by PingHorizon).
func AccountBalance(ctx context.Context, client *http.Client, horizonURL, accountID string) (float64, error) {
	url := strings.TrimRight(horizonURL, "/") + "/accounts/" + accountID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, fmt.Errorf("horizon accounts %s: status %d: %s", accountID, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Balances []struct {
			Balance     string `json:"balance"`
			AssetType   string `json:"asset_type"`
			AssetCode   string `json:"asset_code,omitempty"`
			AssetIssuer string `json:"asset_issuer,omitempty"`
		} `json:"balances"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode account %s: %w", accountID, err)
	}

	for _, b := range payload.Balances {
		if b.AssetType == "native" {
			balance, err := strconv.ParseFloat(b.Balance, 64)
			if err != nil {
				return 0, fmt.Errorf("parse native balance for %s: %w", accountID, err)
			}
			return balance, nil
		}
	}
	return 0, fmt.Errorf("no native balance entry for account %s", accountID)
}

// AccountReserveSampler periodically polls an account's native XLM balance
// from Horizon and caches the last good reading so a metrics collector can
// read it at scrape time without blocking on a network call.
type AccountReserveSampler struct {
	client      *http.Client
	horizonURL  string
	accountID   string
	safeReserve float64
	logger      *slog.Logger

	mu      sync.Mutex
	balance float64
	have    bool
}

// NewAccountReserveSampler builds a sampler for accountID's native balance
// against horizonURL, using safeReserveXLM as the alert threshold reported
// alongside the balance.
func NewAccountReserveSampler(client *http.Client, horizonURL, accountID string, safeReserveXLM float64, logger *slog.Logger) *AccountReserveSampler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AccountReserveSampler{
		client:      client,
		horizonURL:  horizonURL,
		accountID:   accountID,
		safeReserve: safeReserveXLM,
		logger:      logger,
	}
}

// Run polls the account balance on the given interval until ctx is done. It
// ticks once immediately so the first scrape after startup has data.
func (s *AccountReserveSampler) Run(ctx context.Context, interval time.Duration) {
	s.poll(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.poll(ctx)
		}
	}
}

func (s *AccountReserveSampler) poll(ctx context.Context) {
	balance, err := AccountBalance(ctx, s.client, s.horizonURL, s.accountID)
	if err != nil {
		s.logger.Error("stellar account reserve: failed to fetch balance", "account", s.accountID, "error", err)
		return
	}
	s.mu.Lock()
	s.balance = balance
	s.have = true
	s.mu.Unlock()
}

// Sample implements metrics.StellarAccountBalanceSource: it returns the last
// successfully polled balance and the configured safe reserve threshold, and
// reports emit=false until the first successful poll completes.
func (s *AccountReserveSampler) Sample() (balance float64, safeReserve float64, emit bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.have {
		return 0, s.safeReserve, false
	}
	return s.balance, s.safeReserve, true
}
