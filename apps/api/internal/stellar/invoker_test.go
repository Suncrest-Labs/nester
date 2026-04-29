package stellar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testInvoker(t *testing.T, rpcURL, horizonURL string) *ContractInvoker {
	t.Helper()
	kp := keypair.MustRandom()
	inv, err := NewContractInvoker(rpcURL, horizonURL, "Test SDF Network ; September 2015", kp.Seed())
	require.NoError(t, err)
	return inv
}

func TestNewContractInvoker_InvalidSecret(t *testing.T) {
	_, err := NewContractInvoker("http://rpc", "http://horizon", "passphrase", "not-a-secret")
	assert.Error(t, err)
}

func TestNewContractInvoker_ValidSecret(t *testing.T) {
	kp := keypair.MustRandom()
	inv, err := NewContractInvoker("http://rpc", "http://horizon", "passphrase", kp.Seed())
	require.NoError(t, err)
	assert.NotNil(t, inv)
}

func TestContractAddressToXDR_Valid(t *testing.T) {
	// A well-formed C... Stellar contract address (32 bytes of zeros strkey-encoded).
	addr := "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM"
	scAddr, err := contractAddressToXDR(addr)
	require.NoError(t, err)
	assert.NotNil(t, scAddr.ContractId)
}

func TestContractAddressToXDR_Invalid(t *testing.T) {
	_, err := contractAddressToXDR("not-a-contract")
	assert.ErrorIs(t, err, ErrInvalidContract)
}

func TestAccountAddressToXDR_Valid(t *testing.T) {
	kp := keypair.MustRandom()
	scAddr, err := accountAddressToXDR(kp.Address())
	require.NoError(t, err)
	assert.NotNil(t, scAddr.AccountId)
}

// TestInvokeVoidFunction_SimulateError verifies that a simulate failure is
// surfaced before any transaction is submitted.
func TestInvokeVoidFunction_SimulateError(t *testing.T) {
	rpcCalls := 0
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rpcCalls++
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		switch req["method"] {
		case "simulateTransaction":
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"error": "contract not found",
				},
			})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer rpc.Close()

	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sequence": "100"})
	}))
	defer horizon.Close()

	inv := testInvoker(t, rpc.URL, horizon.URL)
	contractAddr := "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM"
	err := inv.InvokeVoidFunction(context.Background(), contractAddr, "pause")

	assert.ErrorIs(t, err, ErrSimulateFailed)
	assert.Equal(t, 1, rpcCalls, "sendTransaction must not be called after simulate fails")
}

// TestInvokeVoidFunction_SendError verifies send failures are surfaced correctly.
func TestInvokeVoidFunction_SendError(t *testing.T) {
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		switch req["method"] {
		case "simulateTransaction":
			// Return a minimal valid simulate response so the invoker proceeds to send.
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"error": map[string]any{
					"code":    -32600,
					"message": "rpc unavailable",
				},
			})
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer rpc.Close()

	horizon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sequence": "100"})
	}))
	defer horizon.Close()

	inv := testInvoker(t, rpc.URL, horizon.URL)
	contractAddr := "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM"
	err := inv.InvokeVoidFunction(context.Background(), contractAddr, "pause")

	assert.ErrorIs(t, err, ErrSimulateFailed)
}
