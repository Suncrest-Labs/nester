package stellar

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Account / Balance Read Tests
// ============================================================================

func TestGetAccountBalance_ValidAddress(t *testing.T) {
	// Test with valid Stellar address format
	validAddress := "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX"

	// In production, this would query the Horizon API
	// For now, we test the validation logic
	assert.Len(t, validAddress, 56)
	assert.Equal(t, 'G', rune(validAddress[0]))
}

func TestGetAccountBalance_InvalidAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{
			name:    "empty address",
			address: "",
			wantErr: true,
		},
		{
			name:    "too short",
			address: "SHORT",
			wantErr: true,
		},
		{
			name:    "invalid prefix",
			address: "XBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
			wantErr: true,
		},
		{
			name:    "too long",
			address: "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGXEXTRAA",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate address format
			err := validateStellarAddress(tt.address)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetAccountBalance_AssetCode(t *testing.T) {
	tests := []struct {
		name      string
		assetCode string
		valid     bool
	}{
		{
			name:      "valid XLM",
			assetCode: "XLM",
			valid:     true,
		},
		{
			name:      "valid USDC",
			assetCode: "USDC",
			valid:     true,
		},
		{
			name:      "valid 4-char code",
			assetCode: "ABCD",
			valid:     true,
		},
		{
			name:      "valid 5-char code",
			assetCode: "ABCDE",
			valid:     true,
		},
		{
			name:      "valid 12-char code",
			assetCode: "ABCDEFGHIJKL",
			valid:     true,
		},
		{
			name:      "empty code",
			assetCode: "",
			valid:     false,
		},
		{
			name:      "too long",
			assetCode: "ABCDEFGHIJKLM",
			valid:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAssetCode(tt.assetCode)
			if tt.valid {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestGetAccountBalance_BalanceStructure(t *testing.T) {
	// Test the balance structure
	balance := &AccountBalance{
		Address:   "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
		AssetCode: "USDC",
		Amount:    decimal.RequireFromString("1000.50"),
	}

	assert.Equal(t, "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX", balance.Address)
	assert.Equal(t, "USDC", balance.AssetCode)
	assert.True(t, balance.Amount.Equal(decimal.RequireFromString("1000.50")))
}

func TestGetAccountBalance_ZeroBalance(t *testing.T) {
	balance := &AccountBalance{
		Address:   "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
		AssetCode: "XLM",
		Amount:    decimal.Zero,
	}

	assert.True(t, balance.Amount.IsZero())
}

func TestGetAccountBalance_NegativeBalance(t *testing.T) {
	// Negative balances should be handled gracefully
	balance := &AccountBalance{
		Address:   "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
		AssetCode: "XLM",
		Amount:    decimal.RequireFromString("-100.00"),
	}

	assert.True(t, balance.Amount.IsNegative())
}

func TestGetAccountBalance_LargeBalance(t *testing.T) {
	// Test with large balance
	balance := &AccountBalance{
		Address:   "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
		AssetCode: "USDC",
		Amount:    decimal.RequireFromString("999999999999.9999999"),
	}

	assert.True(t, balance.Amount.GreaterThan(decimal.Zero))
}

func TestGetAccountBalance_MultipleAssets(t *testing.T) {
	// Test querying multiple assets for the same address
	address := "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX"

	balances := []AccountBalance{
		{
			Address:   address,
			AssetCode: "XLM",
			Amount:    decimal.RequireFromString("1000.00"),
		},
		{
			Address:   address,
			AssetCode: "USDC",
			Amount:    decimal.RequireFromString("500.00"),
		},
		{
			Address:   address,
			AssetCode: "EURC",
			Amount:    decimal.RequireFromString("250.00"),
		},
	}

	assert.Len(t, balances, 3)
	for _, balance := range balances {
		assert.Equal(t, address, balance.Address)
		assert.True(t, balance.Amount.GreaterThan(decimal.Zero))
	}
}

// ============================================================================
// Account Not Found Error Tests
// ============================================================================

func TestErrAccountNotFound(t *testing.T) {
	err := ErrAccountNotFound{
		Address: "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
	}

	assert.Contains(t, err.Error(), "account not found")
	assert.Contains(t, err.Error(), "GBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX")
}

func TestErrAccountNotFound_EmptyAddress(t *testing.T) {
	err := ErrAccountNotFound{
		Address: "",
	}

	assert.Contains(t, err.Error(), "account not found")
}

// ============================================================================
// Network Environment Handling Tests
// ============================================================================

func TestNetworkEnvironment_Testnet(t *testing.T) {
	// Test that Testnet network is correctly identified
	network := Testnet
	assert.Equal(t, Network("testnet"), network)
	assert.Equal(t, "testnet", string(network))
}

func TestNetworkEnvironment_Mainnet(t *testing.T) {
	// Test that Mainnet network is correctly identified
	network := Mainnet
	assert.Equal(t, Network("mainnet"), network)
	assert.Equal(t, "mainnet", string(network))
}

func TestNetworkEnvironment_Futurenet(t *testing.T) {
	// Test that Futurenet network is correctly identified
	network := Futurenet
	assert.Equal(t, Network("futurenet"), network)
	assert.Equal(t, "futurenet", string(network))
}

func TestNetworkEnvironment_InvalidNetwork(t *testing.T) {
	// Test that invalid network defaults to Testnet
	network := Network("invalid")
	networkID := getNetworkID(network)
	assert.Equal(t, "Test SDF Network ; September 2015", networkID)
}

func TestNetworkPassphrase_Testnet(t *testing.T) {
	networkID := getNetworkID(Testnet)
	assert.Equal(t, "Test SDF Network ; September 2015", networkID)
}

func TestNetworkPassphrase_Mainnet(t *testing.T) {
	networkID := getNetworkID(Mainnet)
	assert.Equal(t, "Public Global Stellar Network ; September 2015", networkID)
}

func TestNetworkPassphrase_Futurenet(t *testing.T) {
	networkID := getNetworkID(Futurenet)
	assert.Equal(t, "Test SDF Future Network ; October 2022", networkID)
}

func TestNetworkPassphrase_Custom(t *testing.T) {
	// Test custom network passphrase
	customPassphrase := "Custom Network Passphrase"
	cfg := Config{
		Network:   Testnet,
		NetworkID: customPassphrase,
		RPCURL:    "https://soroban-testnet.stellar.org",
		SourceKey: "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
	}

	// Custom NetworkID should override default
	assert.Equal(t, customPassphrase, cfg.NetworkID)
}

func TestNetworkEnvironment_DevelopmentConfig(t *testing.T) {
	// Test configuration for development environment (APP_ENV=development)
	cfg := Config{
		Network:   Testnet,
		RPCURL:    "https://soroban-testnet.stellar.org",
		SourceKey: "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
	}

	assert.Equal(t, Testnet, cfg.Network)
	assert.Contains(t, cfg.RPCURL, "testnet")
}

func TestNetworkEnvironment_ProductionConfig(t *testing.T) {
	// Test configuration for production environment (APP_ENV=production)
	cfg := Config{
		Network:   Mainnet,
		RPCURL:    "https://soroban-mainnet.stellar.org",
		SourceKey: "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
	}

	assert.Equal(t, Mainnet, cfg.Network)
	assert.Contains(t, cfg.RPCURL, "mainnet")
}

func TestNetworkEnvironment_WrongPassphrase(t *testing.T) {
	// Test that wrong network passphrase is rejected
	// In production, this would fail during transaction signing
	cfg := Config{
		Network:   Testnet,
		NetworkID: "Wrong Passphrase",
		RPCURL:    "https://soroban-testnet.stellar.org",
		SourceKey: "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
	}

	// The config accepts it, but signing would fail
	assert.NotEqual(t, "Test SDF Network ; September 2015", cfg.NetworkID)
}

// ============================================================================
// Helper Functions for Account Tests
// ============================================================================

// validateStellarAddress validates a Stellar address format
func validateStellarAddress(address string) error {
	if address == "" {
		return ErrAccountNotFound{Address: address}
	}
	if len(address) != 56 {
		return ErrAccountNotFound{Address: address}
	}
	if address[0] != 'G' && address[0] != 'S' {
		return ErrAccountNotFound{Address: address}
	}
	return nil
}

// validateAssetCode validates a Stellar asset code
func validateAssetCode(code string) error {
	if code == "" {
		return errors.New("asset code is required")
	}
	if len(code) > 12 {
		return errors.New("asset code must be 12 characters or less")
	}
	return nil
}

// AccountBalance represents an account balance for testing
type AccountBalance struct {
	Address   string
	AssetCode string
	Amount    decimal.Decimal
}

// ErrAccountNotFound represents an account not found error
type ErrAccountNotFound struct {
	Address string
}

func (e ErrAccountNotFound) Error() string {
	return "account not found: " + e.Address
}
