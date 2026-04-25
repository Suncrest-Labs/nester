package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/bank"
)

const paystackBaseURL = "https://api.paystack.co"

// PaystackResolver implements bank.BankResolver using the Paystack API.
type PaystackResolver struct {
	apiKey     string
	httpClient *http.Client
}

// NewPaystackResolver creates a resolver backed by the Paystack API.
func NewPaystackResolver(apiKey string) *PaystackResolver {
	return &PaystackResolver{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ListBanks fetches the Nigerian bank list from Paystack.
func (p *PaystackResolver) ListBanks(ctx context.Context, country string) ([]bank.Bank, error) {
	url := fmt.Sprintf("%s/bank?country=%s&perPage=200", paystackBaseURL, country)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("paystack list banks: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("paystack list banks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("paystack list banks: unexpected status %d", resp.StatusCode)
	}

	var payload struct {
		Status bool `json:"status"`
		Data   []struct {
			Name string `json:"name"`
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("paystack list banks: decode: %w", err)
	}
	if !payload.Status {
		return nil, fmt.Errorf("paystack list banks: provider returned status=false")
	}

	banks := make([]bank.Bank, 0, len(payload.Data))
	for _, b := range payload.Data {
		banks = append(banks, bank.Bank{
			Name:    b.Name,
			Code:    b.Code,
			Country: country,
		})
	}
	return banks, nil
}

// ResolveAccount resolves an account name via Paystack's /bank/resolve endpoint.
func (p *PaystackResolver) ResolveAccount(ctx context.Context, accountNumber, bankCode string) (*bank.AccountInfo, error) {
	url := fmt.Sprintf("%s/bank/resolve?account_number=%s&bank_code=%s",
		paystackBaseURL, accountNumber, bankCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("paystack resolve: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("paystack resolve: %w", err)
	}
	defer resp.Body.Close()

	// Paystack returns 422 when the account is not found.
	if resp.StatusCode == http.StatusUnprocessableEntity || resp.StatusCode == http.StatusNotFound {
		return nil, bank.ErrAccountNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("paystack resolve: unexpected status %d", resp.StatusCode)
	}

	var payload struct {
		Status bool `json:"status"`
		Data   struct {
			AccountNumber string `json:"account_number"`
			AccountName   string `json:"account_name"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("paystack resolve: decode: %w", err)
	}
	if !payload.Status {
		return nil, bank.ErrAccountNotFound
	}

	// Look up the bank name from the bank list — Paystack resolve doesn't
	// return it directly so we embed what we know.
	return &bank.AccountInfo{
		AccountNumber: payload.Data.AccountNumber,
		AccountName:   payload.Data.AccountName,
		BankCode:      bankCode,
		BankName:      "", // enriched by BankService from cached bank list
	}, nil
}
