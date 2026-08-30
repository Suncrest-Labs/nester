package stellar

import (
	"context"
	"fmt"
	"net/http"
)

// rpcEventFetcher is the production EventFetcher: a thin adapter over the
// Soroban JSON-RPC endpoint the indexer has always polled.
//
// It holds no state beyond its caller so a single instance is safe to reuse
// across polls. Both of its methods are reads, so both are retried under the
// shared policy (nester#1086) — which matters most here, because a transient
// failure in the indexer's poll loop previously stalled indexing until the
// next tick.
type rpcEventFetcher struct {
	rpc *rpcClient
}

// NewRPCEventFetcher returns an EventFetcher backed by a Soroban JSON-RPC
// endpoint.
func NewRPCEventFetcher(client *http.Client, rpcURL string) EventFetcher {
	return NewRPCEventFetcherWithOptions(client, rpcURL, RPCOptions{})
}

// NewRPCEventFetcherWithOptions is NewRPCEventFetcher with the shared retry
// policy and metrics observer supplied by startup.
func NewRPCEventFetcherWithOptions(client *http.Client, rpcURL string, opts RPCOptions) EventFetcher {
	// Untraced: the indexer polls every few seconds forever, and a span per
	// poll would bury the request-scoped traces an operator actually reads.
	return &rpcEventFetcher{rpc: newRPCClient(rpcURL, client, opts, false)}
}

// FetchEvents implements EventFetcher using the getEvents RPC method.
func (f *rpcEventFetcher) FetchEvents(
	ctx context.Context,
	contractIDs []string,
	startLedger uint64,
) ([]indexedEvent, uint64, error) {
	return fetchSorobanEvents(ctx, f.rpc, contractIDs, startLedger)
}

// LatestLedger implements EventFetcher using the getLatestLedger RPC method.
//
// This is the source of truth for cold-start initialisation: without it the
// indexer had no way to derive a valid startLedger on a fresh database and
// fell back to 0, which the RPC rejects (B-02).
func (f *rpcEventFetcher) LatestLedger(ctx context.Context) (uint64, error) {
	var rpcResp struct {
		Result struct {
			Sequence uint64 `json:"sequence"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := f.rpc.call(ctx, "getLatestLedger", nil, &rpcResp); err != nil {
		return 0, err
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf("getLatestLedger rpc error: %s", rpcResp.Error.Message)
	}

	return rpcResp.Result.Sequence, nil
}
