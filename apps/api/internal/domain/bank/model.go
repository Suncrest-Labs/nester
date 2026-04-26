// Package bank defines the domain types and provider interface for bank
// account name-enquiry (NUBAN resolution) used by the offramp settlement flow.
package bank

import (
	"context"
	"errors"
)

// Sentinel errors returned by the service and handler layers.
var (
	ErrProviderUnavailable  = errors.New("bank resolver: all providers unavailable")
	ErrAccountNotFound      = errors.New("bank resolver: account not found")
	ErrInvalidAccountNumber = errors.New("bank resolver: account number must be exactly 10 digits")
	ErrInvalidBankCode      = errors.New("bank resolver: bank code is required")
	ErrInvalidCountry       = errors.New("bank resolver: unsupported country code")
)

// SupportedCountries is the set of ISO country codes accepted at launch.
var SupportedCountries = map[string]bool{
	"NG": true,
}

// Bank represents a single entry in the bank list.
type Bank struct {
	Name    string `json:"name"`
	Code    string `json:"code"`
	Country string `json:"country"`
}

// AccountInfo is the result of a successful name-enquiry call.
type AccountInfo struct {
	AccountNumber string `json:"account_number"`
	AccountName   string `json:"account_name"`
	BankCode      string `json:"bank_code"`
	BankName      string `json:"bank_name"`
}

// BankResolver is the provider abstraction for listing banks and resolving
// account names. Implementations exist for Paystack (primary) and
// Flutterwave (fallback).
type BankResolver interface {
	// ListBanks returns the supported bank list for the given country code.
	ListBanks(ctx context.Context, country string) ([]Bank, error)

	// ResolveAccount performs a NUBAN name-enquiry.
	// Returns ErrAccountNotFound when the provider confirms no match.
	ResolveAccount(ctx context.Context, accountNumber, bankCode string) (*AccountInfo, error)
}
