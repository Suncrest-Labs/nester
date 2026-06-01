package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Sep24Resolver is a proof-of-concept for interacting with a Stellar SEP-24 anchor.
type Sep24Resolver struct {
	anchorURL  string
	httpClient *http.Client
	jwtToken   string // Simplified for POC
}

func NewSep24Resolver(anchorURL, jwtToken string) *Sep24Resolver {
	return &Sep24Resolver{
		anchorURL: anchorURL,
		jwtToken:  jwtToken,
		// Use the default transport which enforces TLS certificate validation.
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// InitiateInteractiveWithdrawal initiates a SEP-24 interactive withdrawal
// and returns the transaction ID and the URL the user needs to visit to complete the flow.
func (r *Sep24Resolver) InitiateInteractiveWithdrawal(ctx context.Context, asset string, amount decimal.Decimal) (string, string, error) {
	endpoint := fmt.Sprintf("%s/transactions/withdraw/interactive", strings.TrimRight(r.anchorURL, "/"))

	payload := map[string]interface{}{
		"asset_code": asset,
		"amount":     amount.String(),
	}
	bodyData, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyData))
	if err != nil {
		return "", "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.jwtToken))

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(b))
	}

	var result struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", err
	}

	return result.ID, result.URL, nil
}

// PollTransactionStatus polls the anchor for the current transaction status.
func (r *Sep24Resolver) PollTransactionStatus(ctx context.Context, id string) (string, error) {
	endpoint := fmt.Sprintf("%s/transaction?id=%s", strings.TrimRight(r.anchorURL, "/"), url.QueryEscape(id))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", r.jwtToken))

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result struct {
		Transaction struct {
			Status string `json:"status"`
		} `json:"transaction"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Transaction.Status, nil
}
