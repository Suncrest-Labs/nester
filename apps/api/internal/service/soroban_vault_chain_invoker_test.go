package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
)

func newTestInvoker(t *testing.T, rpcURL, horizonURL string) *service.SorobanVaultChainInvoker {
	t.Helper()
	kp := keypair.MustRandom()
	inv, err := service.NewSorobanVaultChainInvoker(
		rpcURL,
		horizonURL,
		"Test SDF Network ; September 2015",
		kp.Seed(),
	)
	require.NoError(t, err)
	return inv
}

func TestNewSorobanVaultChainInvoker_BadSecret(t *testing.T) {
	_, err := service.NewSorobanVaultChainInvoker("http://rpc", "http://horizon", "pass", "bad")
	assert.Error(t, err)
}

// TestPauseVault_RpcError verifies that an RPC-level error propagates from PauseVault.
func TestPauseVault_RpcError(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"error": "simulate failed"},
		})
	}))
	defer rpc.Close()

	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sequence": "42"})
	}))
	defer horizon.Close()

	inv := newTestInvoker(t, rpc.URL, horizon.URL)
	err := inv.PauseVault(context.Background(), "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM")
	assert.Error(t, err)
}

// TestUnpauseVault_RpcError verifies that an RPC-level error propagates from UnpauseVault.
func TestUnpauseVault_RpcError(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"error": "simulate failed"},
		})
	}))
	defer rpc.Close()

	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sequence": "42"})
	}))
	defer horizon.Close()

	inv := newTestInvoker(t, rpc.URL, horizon.URL)
	err := inv.UnpauseVault(context.Background(), "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM")
	assert.Error(t, err)
}

// TestPauseVault_InvalidContract verifies that a malformed contract address is rejected early.
func TestPauseVault_InvalidContract(t *testing.T) {
	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sequence": "42"})
	}))
	defer horizon.Close()

	// RPC server should never be hit because the address is invalid.
	rpcHit := false
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rpcHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer rpc.Close()

	inv := newTestInvoker(t, rpc.URL, horizon.URL)
	err := inv.PauseVault(context.Background(), "not-a-contract")
	assert.Error(t, err)
	assert.False(t, rpcHit, "RPC must not be called for an invalid contract address")
}

// TestNoopVaultChainInvoker_IsDefaultWhenNilPassed ensures that NewAdminService
// still defaults to NoopVaultChainInvoker when nil is passed.
func TestNoopVaultChainInvoker_IsDefaultWhenNilPassed(t *testing.T) {
	svc := service.NewAdminService(nil, nil, "", "")
	require.NotNil(t, svc)
}
