package stellar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/xdr"
)

// testContractAddress is a syntactically valid contract strkey used across
// existing fixtures.
const testContractAddress = "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM"

// newSimulateServer serves simulateTransaction responses whose returnValue is
// the given i128, encoded exactly as Soroban RPC returns it.
func newSimulateServer(t *testing.T, hi int64, lo uint64) *httptest.Server {
	t.Helper()

	val := xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &xdr.Int128Parts{Hi: xdr.Int64(hi), Lo: xdr.Uint64(lo)},
	}
	encoded, err := xdr.MarshalBase64(val)
	if err != nil {
		t.Fatalf("encode i128 ScVal: %v", err)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"returnValue": encoded},
		})
	}))
}

// The unit contract the whole balance reconciliation rests on (nester#1082):
// TotalAssetsStroops returns the i128 raw, TotalAssets rescales by 1e7, and
// the StroopsBalanceReader adapter exposes the RAW value through the
// TotalAssets method name the reconciliation interface expects. If the
// adapter ever delegated to the display method instead, every vault would
// diverge by a factor of 1e7 — this test is what pins that apart.
func TestTotalAssetsStroopsAndDisplayUnits(t *testing.T) {
	// 38_250_000_000 stroops = 3825 USDC.
	server := newSimulateServer(t, 0, 38_250_000_000)
	defer server.Close()

	reader := NewContractReader(server.URL, "Test SDF Network ; September 2015", "")

	stroops, err := reader.TotalAssetsStroops(context.Background(), testContractAddress)
	if err != nil {
		t.Fatalf("TotalAssetsStroops() error = %v", err)
	}
	if stroops.String() != "38250000000" {
		t.Fatalf("TotalAssetsStroops() = %s, want 38250000000 (raw, unshifted)", stroops)
	}

	display, err := reader.TotalAssets(context.Background(), testContractAddress)
	if err != nil {
		t.Fatalf("TotalAssets() error = %v", err)
	}
	if display.String() != "3825" {
		t.Fatalf("TotalAssets() = %s, want 3825 (display USDC)", display)
	}

	adapted, err := StroopsBalanceReader{Reader: reader}.TotalAssets(context.Background(), testContractAddress)
	if err != nil {
		t.Fatalf("StroopsBalanceReader.TotalAssets() error = %v", err)
	}
	if !adapted.Equal(stroops) {
		t.Fatalf("StroopsBalanceReader.TotalAssets() = %s, want the raw stroops %s", adapted, stroops)
	}
}

// An i128 balance past int64 must error loudly, never return a truncated or
// wrapped value the comparator would then record as a divergence.
func TestTotalAssetsStroopsOverflowErrors(t *testing.T) {
	server := newSimulateServer(t, 1, 0) // hi=1 → 2^64, outside int64
	defer server.Close()

	reader := NewContractReader(server.URL, "Test SDF Network ; September 2015", "")

	if _, err := reader.TotalAssetsStroops(context.Background(), testContractAddress); err == nil {
		t.Fatal("expected an overflow error for an i128 past int64, got nil")
	}
}
