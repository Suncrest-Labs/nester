package stellar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// rpcErrorServer answers every JSON-RPC call with the given error message,
// the way a Soroban RPC node does when it rejects a getEvents request.
func rpcErrorServer(t *testing.T, message string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]any{"code": -32602, "message": message},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A cursor older than the RPC's retention window can never be served: the node
// has pruned those ledgers. The indexer must surface that as a typed, specific
// error so operators see "run backfill" rather than a generic fetch failure
// that repeats forever.
func TestFetchEvents_CursorBeforeRetentionIsTyped(t *testing.T) {
	srv := rpcErrorServer(t, "startLedger must be within the ledger range: 5000 - 9000")
	rpc := newRPCClient(srv.URL, srv.Client(), RPCOptions{}, false)

	_, _, err := fetchSorobanEvents(context.Background(), rpc, []string{"CVAULT"}, 100)

	var gap *CursorBehindRetentionError
	if !errors.As(err, &gap) {
		t.Fatalf("expected *CursorBehindRetentionError, got %T: %v", err, err)
	}
	if gap.Cursor != 100 || gap.Oldest != 5000 {
		t.Errorf("got cursor=%d oldest=%d, want cursor=100 oldest=5000", gap.Cursor, gap.Oldest)
	}
	if !strings.Contains(err.Error(), "backfill") {
		t.Errorf("error should point operators at the backfill command, got: %v", err)
	}
}

func TestFetchEvents_OtherRPCErrorsStayGeneric(t *testing.T) {
	cases := map[string]string{
		"unrelated error":           "internal error",
		"cursor ahead of the range": "startLedger must be within the ledger range: 5000 - 9000",
		"malformed range":           "startLedger must be within the ledger range: soon",
	}
	starts := map[string]uint64{
		"unrelated error":           100,
		"cursor ahead of the range": 9500,
		"malformed range":           100,
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			srv := rpcErrorServer(t, msg)
			rpc := newRPCClient(srv.URL, srv.Client(), RPCOptions{}, false)

			_, _, err := fetchSorobanEvents(context.Background(), rpc, []string{"CVAULT"}, starts[name])

			if err == nil {
				t.Fatal("expected an error")
			}
			var gap *CursorBehindRetentionError
			if errors.As(err, &gap) {
				t.Errorf("must not classify as a retention gap: %v", err)
			}
		})
	}
}
