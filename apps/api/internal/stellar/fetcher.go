package stellar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// rpcEventFetcher is the production EventFetcher: a thin adapter over the
// Soroban JSON-RPC endpoint the indexer has always polled.
//
// It holds no state beyond its client and URL so a single instance is safe to
// reuse across polls.
type rpcEventFetcher struct {
	client *http.Client
	url    string
}

// NewRPCEventFetcher returns an EventFetcher backed by a Soroban JSON-RPC
// endpoint.
func NewRPCEventFetcher(client *http.Client, rpcURL string) EventFetcher {
	return &rpcEventFetcher{client: client, url: rpcURL}
}

// FetchEvents implements EventFetcher using the getEvents RPC method.
func (f *rpcEventFetcher) FetchEvents(
	ctx context.Context,
	contractIDs []string,
	startLedger uint64,
) ([]indexedEvent, uint64, error) {
	return fetchSorobanEvents(ctx, f.client, f.url, contractIDs, startLedger)
}

// LatestLedger implements EventFetcher using the getLatestLedger RPC method.
//
// This is the source of truth for cold-start initialisation: without it the
// indexer had no way to derive a valid startLedger on a fresh database and
// fell back to 0, which the RPC rejects (B-02).
func (f *rpcEventFetcher) LatestLedger(ctx context.Context) (uint64, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "nester-indexer",
		"method":  "getLatestLedger",
	})
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("getLatestLedger returned %d: %s", resp.StatusCode, string(payload))
	}

	var rpcResp struct {
		Result struct {
			Sequence uint64 `json:"sequence"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&rpcResp); err != nil {
		return 0, err
	}
	if rpcResp.Error != nil {
		return 0, fmt.Errorf("getLatestLedger rpc error: %s", rpcResp.Error.Message)
	}

	return rpcResp.Result.Sequence, nil
}
