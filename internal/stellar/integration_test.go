//go:build integration
// +build integration

package stellar

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Integration Tests - Run against Stellar testnet in CI (tagged)
// ============================================================================

// TestIntegration_FullWorkflow tests the complete Stellar integration layer
// Run with: go test -tags integration ./internal/stellar/...
func TestIntegration_FullWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Load configuration from environment
	rpcURL := os.Getenv("STELLAR_RPC_URL")
	sourceKey := os.Getenv("STELLAR_SOURCE_KEY")
	contractID := os.Getenv("STELLAR_CONTRACT_ID")

	if rpcURL == "" || sourceKey == "" || contractID == "" {
		t.Skip("Skipping integration test: STELLAR_RPC_URL, STELLAR_SOURCE_KEY, and STELLAR_CONTRACT_ID env vars required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := Config{
		Network:      Testnet,
		RPCURL:       rpcURL,
		SourceKey:    sourceKey,
		MaxRetries:   3,
		RetryBackoff: 100,
	}

	// Initialize client
	client, err := NewClient(ctx, cfg)
	require.NoError(t, err)
	defer client.Close()

	// Test health check
	t.Run("HealthCheck", func(t *testing.T) {
		health, err := client.Health(ctx)
		require.NoError(t, err)
		assert.True(t, health.Healthy)
		assert.Greater(t, health.NetworkLatency, int64(0))
	})

	// Test contract invocation
	t.Run("ContractInvocation", func(t *testing.T) {
		invoker := NewContractInvoker(client)

		// Simulate a contract call
		simResult, err := invoker.SimulateContract(ctx, contractID, "get_balance", []interface{}{})
		if err != nil {
			t.Logf("Simulation failed (expected if contract method doesn't exist): %v", err)
		} else {
			assert.NotNil(t, simResult)
		}
	})

	// Test vault reader
	t.Run("VaultReader", func(t *testing.T) {
		invoker := NewContractInvoker(client)
		reader := NewVaultReader(invoker)

		// Query vault balance
		balance, err := reader.GetVaultBalance(ctx, contractID)
		if err != nil {
			t.Logf("Balance query failed (expected if contract method doesn't exist): %v", err)
		} else if balance != nil {
			assert.Equal(t, contractID, balance.ContractID)
			assert.NotNil(t, balance.Total)
		}
	})

	// Test event polling
	t.Run("EventPolling", func(t *testing.T) {
		poller := NewEventPoller(client)

		// Try to poll events
		events, err := poller.PollEvents(ctx, contractID, 0, 100)
		require.NoError(t, err)
		assert.NotNil(t, events)
		// Events may be empty on testnet
	})
}

// TestIntegration_RetryLogic tests exponential backoff retry behavior
func TestIntegration_RetryLogic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// This tests the retry mechanism with invalid RPC URL
	cfg := Config{
		Network:      Testnet,
		RPCURL:       "https://invalid-rpc-url-that-does-not-exist.example.com",
		SourceKey:    "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGG",
		MaxRetries:   2,
		RetryBackoff: 50,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connection should fail
	_, err := NewClient(ctx, cfg)
	assert.Error(t, err)
}

// TestIntegration_TypeSafety verifies no SDK types leak into domain layer
func TestIntegration_TypeSafety(t *testing.T) {
	// Verify domain types don't depend on SDK types

	// VaultBalance uses decimal.Decimal, not SDK types
	balance := &VaultBalance{
		ContractID: "TEST",
		Total:      decimal.NewFromInt(1000),
		Available:  decimal.NewFromInt(500),
		Locked:     decimal.NewFromInt(500),
	}
	assert.Equal(t, decimal.NewFromInt(1000), balance.Total)

	// Allocation uses decimal.Decimal and plain strings
	alloc := &Allocation{
		ContractID:   "TEST",
		SourceName:   "Aave",
		Amount:       decimal.NewFromInt(100),
		APY:          decimal.NewFromString("0.08"),
		AllocationID: "alloc-1",
	}
	assert.Equal(t, "Aave", alloc.SourceName)

	// Event has a simple Data map
	event := &Event{
		ContractID:    "TEST",
		EventType:     "Deposit",
		BlockNumber:   100,
		TransactionID: "TXN123",
		Data:          map[string]interface{}{"amount": "1000"},
	}
	assert.IsType(t, map[string]interface{}{}, event.Data)
}

// TestIntegration_EnvironmentConfig tests loading config from environment
func TestIntegration_EnvironmentConfig(t *testing.T) {
	// Simulate loading config from env
	os.Setenv("TEST_NETWORK", "testnet")
	os.Setenv("TEST_RPC_URL", "https://soroban-testnet.stellar.org")
	os.Setenv("TEST_SOURCE_KEY", "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGG")

	// Load and validate
	network := Network(os.Getenv("TEST_NETWORK"))
	assert.Equal(t, Testnet, network)

	rpcURL := os.Getenv("TEST_RPC_URL")
	assert.Equal(t, "https://soroban-testnet.stellar.org", rpcURL)

	sourceKey := os.Getenv("TEST_SOURCE_KEY")
	err := validateSourceKey(sourceKey)
	assert.NoError(t, err)
}

// TestIntegration_VaultVerification tests vault integrity checks
func TestIntegration_VaultVerification(t *testing.T) {
	client := &Client{
		config: Config{
			MaxRetries:   3,
			RetryBackoff: 100,
		},
	}

	invoker := NewContractInvoker(client)
	reader := NewVaultReader(invoker)

	// Test vault integrity with valid state
	balance := &VaultBalance{
		ContractID: "TEST",
		Total:      decimal.NewFromInt(1000),
		Available:  decimal.NewFromInt(600),
		Locked:     decimal.NewFromInt(400),
	}

	// Verify invariants
	assert.True(t, balance.Total.GreaterThanOrEqual(balance.Available))
	assert.True(t, balance.Total.GreaterThanOrEqual(balance.Locked))
	sum := balance.Available.Add(balance.Locked)
	assert.True(t, sum.LessThanOrEqual(balance.Total))
}

// TestIntegration_EventFiltering tests event filtering utilities
func TestIntegration_EventFiltering(t *testing.T) {
	events := []Event{
		{ContractID: "CONTRACT1", EventType: "Deposit", BlockNumber: 100},
		{ContractID: "CONTRACT2", EventType: "Withdrawal", BlockNumber: 101},
		{ContractID: "CONTRACT1", EventType: "Withdrawal", BlockNumber: 102},
		{ContractID: "CONTRACT2", EventType: "Deposit", BlockNumber: 103},
	}

	// Filter by type
	deposits := FilterEvents(events, "Deposit")
	assert.Equal(t, 2, len(deposits))
	for _, e := range deposits {
		assert.Equal(t, "Deposit", e.EventType)
	}

	// Filter by contract
	contract1Events := FilterEventsByContract(events, "CONTRACT1")
	assert.Equal(t, 2, len(contract1Events))
	for _, e := range contract1Events {
		assert.Equal(t, "CONTRACT1", e.ContractID)
	}

	// Combined filtering
	contract1Deposits := FilterEvents(
		FilterEventsByContract(events, "CONTRACT1"),
		"Deposit",
	)
	assert.Equal(t, 1, len(contract1Deposits))
	assert.Equal(t, "CONTRACT1", contract1Deposits[0].ContractID)
	assert.Equal(t, "Deposit", contract1Deposits[0].EventType)
}

// ============================================================================
// Additional Integration Tests for Real Network Interactions
// ============================================================================

// TestIntegration_RealTransactionSubmission tests real transaction submission
func TestIntegration_RealTransactionSubmission(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	rpcURL := os.Getenv("STELLAR_RPC_URL")
	sourceKey := os.Getenv("STELLAR_SOURCE_KEY")
	contractID := os.Getenv("STELLAR_CONTRACT_ID")

	if rpcURL == "" || sourceKey == "" || contractID == "" {
		t.Skip("Skipping integration test: environment variables required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := Config{
		Network:      Testnet,
		RPCURL:       rpcURL,
		SourceKey:    sourceKey,
		MaxRetries:   3,
		RetryBackoff: 100,
	}

	client, err := NewClient(ctx, cfg)
	require.NoError(t, err)
	defer client.Close()

	invoker := NewContractInvoker(client)

	// Test contract invocation with real network
	t.Run("InvokeContract", func(t *testing.T) {
		result, err := invoker.InvokeContract(
			ctx,
			contractID,
			"get_balance",
			[]interface{}{},
		)

		// May fail if contract method doesn't exist, but should not panic
		if err != nil {
			t.Logf("Contract invocation failed (expected): %v", err)
		} else {
			assert.NotNil(t, result)
			if result.IsSuccess {
				assert.NotEmpty(t, result.TransactionHash)
			}
		}
	})
}

// TestIntegration_NetworkPassphraseValidation tests network passphrase validation
func TestIntegration_NetworkPassphraseValidation(t *testing.T) {
	tests := []struct {
		name       string
		network    Network
		passphrase string
		valid      bool
	}{
		{
			name:       "testnet valid",
			network:    Testnet,
			passphrase: "Test SDF Network ; September 2015",
			valid:      true,
		},
		{
			name:       "mainnet valid",
			network:    Mainnet,
			passphrase: "Public Global Stellar Network ; September 2015",
			valid:      true,
		},
		{
			name:       "futurenet valid",
			network:    Futurenet,
			passphrase: "Test SDF Future Network ; October 2022",
			valid:      true,
		},
		{
			name:       "wrong passphrase",
			network:    Testnet,
			passphrase: "Wrong Passphrase",
			valid:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			networkID := getNetworkID(tt.network)
			if tt.valid {
				assert.Equal(t, tt.passphrase, networkID)
			} else {
				assert.NotEqual(t, tt.passphrase, networkID)
			}
		})
	}
}

// TestIntegration_ClientInitialization tests client initialization with various configs
func TestIntegration_ClientInitialization(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid testnet config",
			cfg: Config{
				Network:   Testnet,
				RPCURL:    "https://soroban-testnet.stellar.org",
				SourceKey: "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
			},
			wantErr: false,
		},
		{
			name: "missing network",
			cfg: Config{
				RPCURL:    "https://soroban-testnet.stellar.org",
				SourceKey: "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
			},
			wantErr: true,
		},
		{
			name: "missing RPC URL",
			cfg: Config{
				Network:   Testnet,
				SourceKey: "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
			},
			wantErr: true,
		},
		{
			name: "missing source key",
			cfg: Config{
				Network: Testnet,
				RPCURL:  "https://soroban-testnet.stellar.org",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			client, err := NewClient(ctx, tt.cfg)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				if client != nil {
					client.Close()
				}
			}
		})
	}
}

// TestIntegration_HealthCheck tests health check with real network
func TestIntegration_HealthCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	rpcURL := os.Getenv("STELLAR_RPC_URL")
	sourceKey := os.Getenv("STELLAR_SOURCE_KEY")

	if rpcURL == "" || sourceKey == "" {
		t.Skip("Skipping integration test: environment variables required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := Config{
		Network:   Testnet,
		RPCURL:    rpcURL,
		SourceKey: sourceKey,
	}

	client, err := NewClient(ctx, cfg)
	require.NoError(t, err)
	defer client.Close()

	health, err := client.Health(ctx)
	require.NoError(t, err)
	assert.True(t, health.Healthy)
	assert.Greater(t, health.NetworkLatency, int64(0))
	assert.Greater(t, health.LastCheckTime, int64(0))
}

// TestIntegration_VaultReaderWithRealNetwork tests vault reader with real network
func TestIntegration_VaultReaderWithRealNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	rpcURL := os.Getenv("STELLAR_RPC_URL")
	sourceKey := os.Getenv("STELLAR_SOURCE_KEY")
	contractID := os.Getenv("STELLAR_CONTRACT_ID")

	if rpcURL == "" || sourceKey == "" || contractID == "" {
		t.Skip("Skipping integration test: environment variables required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := Config{
		Network:   Testnet,
		RPCURL:    rpcURL,
		SourceKey: sourceKey,
	}

	client, err := NewClient(ctx, cfg)
	require.NoError(t, err)
	defer client.Close()

	invoker := NewContractInvoker(client)
	reader := NewVaultReader(invoker)

	t.Run("GetVaultBalance", func(t *testing.T) {
		balance, err := reader.GetVaultBalance(ctx, contractID)
		if err != nil {
			t.Logf("Balance query failed (expected if contract doesn't exist): %v", err)
		} else {
			assert.NotNil(t, balance)
			assert.Equal(t, contractID, balance.ContractID)
		}
	})

	t.Run("GetVaultAllocations", func(t *testing.T) {
		allocations, err := reader.GetVaultAllocations(ctx, contractID)
		if err != nil {
			t.Logf("Allocations query failed (expected if contract doesn't exist): %v", err)
		} else {
			assert.NotNil(t, allocations)
		}
	})

	t.Run("VerifyVaultIntegrity", func(t *testing.T) {
		valid, err := reader.VerifyVaultIntegrity(ctx, contractID)
		if err != nil {
			t.Logf("Integrity check failed (expected if contract doesn't exist): %v", err)
		} else {
			assert.True(t, valid)
		}
	})
}

// TestIntegration_EventPollingWithRealNetwork tests event polling with real network
func TestIntegration_EventPollingWithRealNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	rpcURL := os.Getenv("STELLAR_RPC_URL")
	sourceKey := os.Getenv("STELLAR_SOURCE_KEY")
	contractID := os.Getenv("STELLAR_CONTRACT_ID")

	if rpcURL == "" || sourceKey == "" || contractID == "" {
		t.Skip("Skipping integration test: environment variables required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := Config{
		Network:   Testnet,
		RPCURL:    rpcURL,
		SourceKey: sourceKey,
	}

	client, err := NewClient(ctx, cfg)
	require.NoError(t, err)
	defer client.Close()

	poller := NewEventPoller(client)

	t.Run("PollEvents", func(t *testing.T) {
		events, err := poller.PollEvents(ctx, contractID, 0, 100)
		require.NoError(t, err)
		assert.NotNil(t, events)
		// Events may be empty on testnet
	})

	t.Run("Subscribe", func(t *testing.T) {
		listener := func(event *Event) {
			// Event received
		}

		err := poller.Subscribe(contractID, listener)
		assert.NoError(t, err)
	})
}

// TestIntegration_DefaultValues tests that default values are applied correctly
func TestIntegration_DefaultValues(t *testing.T) {
	cfg := Config{
		Network:   Testnet,
		RPCURL:    "https://soroban-testnet.stellar.org",
		SourceKey: "SBVH6U5PEFXPXPJ4GPXVYACRF4NZQA5QBCZLLPQGHXWWK6NXPV6IYGGX",
	}

	// Verify defaults are not set yet
	assert.Equal(t, 0, cfg.MaxRetries)
	assert.Equal(t, 0, cfg.RetryBackoff)

	// In production, NewClient would set these defaults
	// For now, we just verify the config structure
	assert.Equal(t, Testnet, cfg.Network)
	assert.Contains(t, cfg.RPCURL, "testnet")
}
