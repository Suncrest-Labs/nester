package config

import (
	"strings"
	"testing"
)

// TestStellarNetworkEnv verifies that NetworkEnv classifies the three known
// passphrase values and falls back to "custom" for anything else.
func TestStellarNetworkEnv(t *testing.T) {
	cases := []struct {
		passphrase string
		want       string
	}{
		{StellarMainnetPassphrase, "mainnet"},
		{StellarTestnetPassphrase, "testnet"},
		{"Standalone Network ; February 2017", "custom"},
		{"", "custom"},
	}
	for _, tc := range cases {
		s := StellarConfig{networkPassphrase: tc.passphrase}
		if got := s.NetworkEnv(); got != tc.want {
			t.Errorf("NetworkEnv(%q) = %q, want %q", tc.passphrase, got, tc.want)
		}
	}
}

// TestStellarNetworkConsistency_MainnetPassphraseWithTestnetRPC ensures that
// combining the mainnet passphrase with a testnet RPC URL is rejected at
// startup (nester#1396).
func TestStellarNetworkConsistency_MainnetPassphraseWithTestnetRPC(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", StellarMainnetPassphrase)
	t.Setenv("STELLAR_RPC_URL", "https://soroban-testnet.stellar.org")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon.example.com")
	t.Setenv("YIELD_REGISTRY_CONTRACT", "CAAAA")
	t.Setenv("STELLAR_ALLOCATION_STRATEGY_ADDRESS", "CBBBB")
	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to reject mainnet passphrase + testnet RPC URL")
	}
	if !strings.Contains(err.Error(), "STELLAR_RPC_URL") {
		t.Errorf("expected error to mention STELLAR_RPC_URL, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "testnet") {
		t.Errorf("expected error to mention testnet, got: %v", err)
	}
}

// TestStellarNetworkConsistency_MainnetPassphraseWithTestnetHorizon ensures
// the same rejection for a testnet Horizon URL.
func TestStellarNetworkConsistency_MainnetPassphraseWithTestnetHorizon(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", StellarMainnetPassphrase)
	t.Setenv("STELLAR_RPC_URL", "https://soroban.example.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon-testnet.stellar.org")
	t.Setenv("YIELD_REGISTRY_CONTRACT", "CAAAA")
	t.Setenv("STELLAR_ALLOCATION_STRATEGY_ADDRESS", "CBBBB")
	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to reject mainnet passphrase + testnet Horizon URL")
	}
	if !strings.Contains(err.Error(), "STELLAR_HORIZON_URL") {
		t.Errorf("expected error to mention STELLAR_HORIZON_URL, got: %v", err)
	}
}

// TestStellarNetworkConsistency_MainnetRequiresContracts verifies that the
// mainnet passphrase requires both contract addresses to be set.
func TestStellarNetworkConsistency_MainnetRequiresContracts(t *testing.T) {
	t.Run("missing yield registry", func(t *testing.T) {
		baseEnv(t)
		requiredEnv(t)
		t.Setenv("STELLAR_NETWORK_PASSPHRASE", StellarMainnetPassphrase)
		t.Setenv("STELLAR_RPC_URL", "https://soroban.example.com")
		t.Setenv("STELLAR_HORIZON_URL", "https://horizon.example.com")
		t.Setenv("YIELD_REGISTRY_CONTRACT", "")
		t.Setenv("STELLAR_ALLOCATION_STRATEGY_ADDRESS", "CBBBB")
		chdir(t, t.TempDir())

		_, err := Load()
		if err == nil {
			t.Fatal("expected Load() to reject missing YIELD_REGISTRY_CONTRACT on mainnet")
		}
		if !strings.Contains(err.Error(), "YIELD_REGISTRY_CONTRACT") {
			t.Errorf("expected error to mention YIELD_REGISTRY_CONTRACT, got: %v", err)
		}
	})

	t.Run("missing allocation strategy", func(t *testing.T) {
		baseEnv(t)
		requiredEnv(t)
		t.Setenv("STELLAR_NETWORK_PASSPHRASE", StellarMainnetPassphrase)
		t.Setenv("STELLAR_RPC_URL", "https://soroban.example.com")
		t.Setenv("STELLAR_HORIZON_URL", "https://horizon.example.com")
		t.Setenv("YIELD_REGISTRY_CONTRACT", "CAAAA")
		t.Setenv("STELLAR_ALLOCATION_STRATEGY_ADDRESS", "")
		chdir(t, t.TempDir())

		_, err := Load()
		if err == nil {
			t.Fatal("expected Load() to reject missing STELLAR_ALLOCATION_STRATEGY_ADDRESS on mainnet")
		}
		if !strings.Contains(err.Error(), "STELLAR_ALLOCATION_STRATEGY_ADDRESS") {
			t.Errorf("expected error to mention STELLAR_ALLOCATION_STRATEGY_ADDRESS, got: %v", err)
		}
	})
}

// TestStellarNetworkConsistency_ValidMainnet verifies that a well-formed
// mainnet configuration loads without error.
func TestStellarNetworkConsistency_ValidMainnet(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", StellarMainnetPassphrase)
	t.Setenv("STELLAR_RPC_URL", "https://soroban.example.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon.example.com")
	t.Setenv("YIELD_REGISTRY_CONTRACT", "CAAAA")
	t.Setenv("STELLAR_ALLOCATION_STRATEGY_ADDRESS", "CBBBB")
	chdir(t, t.TempDir())

	_, err := Load()
	if err != nil {
		t.Fatalf("expected valid mainnet config to load, got: %v", err)
	}
}

// TestStellarNetworkConsistency_TestnetPassphraseWithMainnetHorizon ensures
// that a testnet passphrase pointed at the Stellar mainnet Horizon endpoint
// is rejected.
func TestStellarNetworkConsistency_TestnetPassphraseWithMainnetHorizon(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", StellarTestnetPassphrase)
	t.Setenv("STELLAR_RPC_URL", "https://soroban.example.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon.stellar.org")
	chdir(t, t.TempDir())

	_, err := Load()
	if err == nil {
		t.Fatal("expected Load() to reject testnet passphrase + mainnet Horizon URL")
	}
	if !strings.Contains(err.Error(), "STELLAR_HORIZON_URL") {
		t.Errorf("expected error to mention STELLAR_HORIZON_URL, got: %v", err)
	}
}

// TestStellarNetworkConsistency_CustomPassphraseSkipsCheck verifies that a
// custom (non-SDF) network passphrase does not trigger the URL consistency
// check, allowing private/standalone networks.
func TestStellarNetworkConsistency_CustomPassphraseSkipsCheck(t *testing.T) {
	baseEnv(t)
	requiredEnv(t)
	t.Setenv("STELLAR_NETWORK_PASSPHRASE", "My Private Network ; January 2024")
	t.Setenv("STELLAR_RPC_URL", "https://soroban.example.com")
	t.Setenv("STELLAR_HORIZON_URL", "https://horizon.example.com")
	chdir(t, t.TempDir())

	_, err := Load()
	if err != nil {
		t.Fatalf("custom passphrase should not trigger network consistency check, got: %v", err)
	}
}
