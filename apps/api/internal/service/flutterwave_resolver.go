package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/bank"
)

const flutterwaveBaseURL = "https://api.flutterwave.com/v3"

// FlutterwaveResolver implements bank.BankResolver using the Flutterwave API.
// It is used as the automatic fallback when Paystack is unavailable.
type FlutterwaveResolver struct {
	apiKey     string
	httpClient *http.Client
}

// NewFlutterwaveResolver creates a resolver backed by the Flutterwave API.
func NewFlutterwaveResolver(apiKey string) *FlutterwaveResolver {
	return &FlutterwaveResolver{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ListBanks fetches the bank list from Flutterwave.
func (f *FlutterwaveResolver) ListBanks(ctx context.Context, country string) ([]bank.Bank, error) {
	url := fmt.Sprintf("%s/banks/%s", flutterwaveBaseURL, country)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("flutterwave list banks: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("flutterwave list banks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flutterwave list banks: unexpected status %d", resp.StatusCode)
	}

	var payload struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    []struct {
			Name string `json:"name"`
			Code string `json:"code"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("flutterwave list banks: decode: %w", err)
	}
	if payload.Status != "success" {
		return nil, fmt.Errorf("flutterwave list banks: %s", payload.Message)
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

// ResolveAccount resolves an account name via Flutterwave's /accounts/resolve endpoint.
func (f *FlutterwaveResolver) ResolveAccount(ctx context.Context, accountNumber, bankCode string) (*bank.AccountInfo, error) {
	url := fmt.Sprintf("%s/accounts/resolve?account_number=%s&account_bank=%s",
		flutterwaveBaseURL, accountNumber, bankCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("flutterwave resolve: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.apiKey)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("flutterwave resolve: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnprocessableEntity {
		return nil, bank.ErrAccountNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("flutterwave resolve: unexpected status %d", resp.StatusCode)
	}

	var payload struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    struct {
			AccountNumber string `json:"account_number"`
			AccountName   string `json:"account_name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("flutterwave resolve: decode: %w", err)
	}
	if payload.Status != "success" {
		return nil, bank.ErrAccountNotFound
	}

	return &bank.AccountInfo{
		AccountNumber: payload.Data.AccountNumber,
		AccountName:   payload.Data.AccountName,
		BankCode:      bankCode,
		BankName:      "",
	}, nil
}
