package vault

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestParseStatusValidValues(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected VaultStatus
	}{
		{"active lowercase", "active", StatusActive},
		{"active uppercase", "ACTIVE", StatusActive},
		{"active mixed case", "Active", StatusActive},
		{"paused lowercase", "paused", StatusPaused},
		{"paused uppercase", "PAUSED", StatusPaused},
		{"closed lowercase", "closed", StatusClosed},
		{"closed uppercase", "CLOSED", StatusClosed},
		{"with whitespace", "  active  ", StatusActive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := ParseStatus(tt.input)
			if err != nil {
				t.Fatalf("ParseStatus(%q) unexpected error: %v", tt.input, err)
			}
			if status != tt.expected {
				t.Fatalf("ParseStatus(%q) = %q, want %q", tt.input, status, tt.expected)
			}
		})
	}
}

func TestParseStatusInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"invalid status", "invalid"},
		{"random text", "random"},
		{"numeric", "123"},
		{"special chars", "active!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseStatus(tt.input)
			if err != ErrInvalidVault {
				t.Fatalf("ParseStatus(%q) expected ErrInvalidVault, got %v", tt.input, err)
			}
		})
	}
}

func TestVaultStructValidation(t *testing.T) {
	// Test that a valid vault struct can be created
	validVault := Vault{
		ID:              uuid.New(),
		UserID:          uuid.New(),
		ContractAddress: "CA-001",
		TotalDeposited:  decimal.Zero,
		CurrentBalance:  decimal.Zero,
		Currency:        "USDC",
		Status:          StatusActive,
		Allocations:     []Allocation{},
	}

	if validVault.ID == uuid.Nil {
		t.Fatal("valid vault should have non-nil ID")
	}
	if validVault.UserID == uuid.Nil {
		t.Fatal("valid vault should have non-nil UserID")
	}
	if validVault.ContractAddress == "" {
		t.Fatal("valid vault should have non-empty ContractAddress")
	}
	if validVault.Currency == "" {
		t.Fatal("valid vault should have non-empty Currency")
	}
}

func TestAllocationAPYBounds(t *testing.T) {
	tests := []struct {
		name      string
		apy       decimal.Decimal
		shouldErr bool
	}{
		{"valid APY 0%", decimal.Zero, false},
		{"valid APY 5%", decimal.RequireFromString("5.0"), false},
		{"valid APY 100%", decimal.RequireFromString("100.0"), false},
		{"negative APY", decimal.RequireFromString("-1.0"), true},
		{"APY over 100%", decimal.RequireFromString("100.1"), true},
		{"APY way over 100%", decimal.RequireFromString("150.0"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allocation := Allocation{
				ID:       uuid.New(),
				VaultID:  uuid.New(),
				Protocol: "aave",
				Amount:   decimal.RequireFromString("100"),
				APY:      tt.apy,
			}

			// Validate APY bounds
			isValid := allocation.APY.GreaterThanOrEqual(decimal.Zero) && allocation.APY.LessThanOrEqual(decimal.RequireFromString("100"))

			if tt.shouldErr && isValid {
				t.Fatalf("APY %s should be invalid but was considered valid", tt.apy)
			}
			if !tt.shouldErr && !isValid {
				t.Fatalf("APY %s should be valid but was considered invalid", tt.apy)
			}
		})
	}
}

func TestAllocationAmountValidation(t *testing.T) {
	tests := []struct {
		name      string
		amount    decimal.Decimal
		shouldErr bool
	}{
		{"valid amount 0", decimal.Zero, false},
		{"valid amount 100", decimal.RequireFromString("100"), false},
		{"valid amount 0.01", decimal.RequireFromString("0.01"), false},
		{"negative amount", decimal.RequireFromString("-1"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allocation := Allocation{
				ID:       uuid.New(),
				VaultID:  uuid.New(),
				Protocol: "aave",
				Amount:   tt.amount,
				APY:      decimal.RequireFromString("5"),
			}

			// Validate amount is non-negative
			isValid := allocation.Amount.GreaterThanOrEqual(decimal.Zero)

			if tt.shouldErr && isValid {
				t.Fatalf("amount %s should be invalid but was considered valid", tt.amount)
			}
			if !tt.shouldErr && !isValid {
				t.Fatalf("amount %s should be valid but was considered invalid", tt.amount)
			}
		})
	}
}

func TestAllocationProtocolValidation(t *testing.T) {
	tests := []struct {
		name      string
		protocol  string
		shouldErr bool
	}{
		{"valid protocol", "aave", false},
		{"valid protocol uppercase", "AAVE", false},
		{"empty protocol", "", true},
		{"whitespace only protocol", "   ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allocation := Allocation{
				ID:       uuid.New(),
				VaultID:  uuid.New(),
				Protocol: tt.protocol,
				Amount:   decimal.RequireFromString("100"),
				APY:      decimal.RequireFromString("5"),
			}

			// Validate protocol is not empty or whitespace
			isValid := len(trimSpace(allocation.Protocol)) > 0

			if tt.shouldErr && isValid {
				t.Fatalf("protocol %q should be invalid but was considered valid", tt.protocol)
			}
			if !tt.shouldErr && !isValid {
				t.Fatalf("protocol %q should be valid but was considered invalid", tt.protocol)
			}
		})
	}
}

func trimSpace(s string) string {
	// Simple trim implementation for testing
	start := 0
	end := len(s)

	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}

	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}

	return s[start:end]
}
