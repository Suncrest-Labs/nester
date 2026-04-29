package stellar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// HealthResult captures the outcome of a single dependency probe.
type HealthResult struct {
	OK            bool   `json:"ok"`
	Endpoint      string `json:"endpoint,omitempty"`
	Error         string `json:"error,omitempty"`
	LatencyMillis int64  `json:"latency_ms,omitempty"`
	LatestLedger  uint64 `json:"latest_ledger,omitempty"`
}

// PingHorizon issues a GET against the Horizon root endpoint and reports
// reachability plus the current ledger sequence. It does not retry; callers
// supply their own timeout via ctx.
func PingHorizon(ctx context.Context, client *http.Client, horizonURL string) HealthResult {
	url := strings.TrimRight(horizonURL, "/") + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return HealthResult{Endpoint: horizonURL, Error: err.Error()}
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return HealthResult{Endpoint: horizonURL, Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return HealthResult{
			Endpoint: horizonURL,
			Error:    fmt.Sprintf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		}
	}

	var payload struct {
		HistoryLatestLedger uint64 `json:"history_latest_ledger"`
		CoreLatestLedger    uint64 `json:"core_latest_ledger"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload); err != nil {
		// Reachability is established by the 2xx response; failure to parse
		// the body shouldn't fail the health check.
		return HealthResult{OK: true, Endpoint: horizonURL}
	}

	ledger := payload.HistoryLatestLedger
	if ledger == 0 {
		ledger = payload.CoreLatestLedger
	}
	return HealthResult{OK: true, Endpoint: horizonURL, LatestLedger: ledger}
}

// PingSorobanRPC issues a getHealth JSON-RPC call to a Soroban RPC node and
// reports reachability.
func PingSorobanRPC(ctx context.Context, client *http.Client, rpcURL string) HealthResult {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      "nester-health",
		"method":  "getHealth",
	})
	if err != nil {
		return HealthResult{Endpoint: rpcURL, Error: err.Error()}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rpcURL, strings.NewReader(string(body)))
	if err != nil {
		return HealthResult{Endpoint: rpcURL, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return HealthResult{Endpoint: rpcURL, Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return HealthResult{
			Endpoint: rpcURL,
			Error:    fmt.Sprintf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(payload))),
		}
	}

	var rpcResp struct {
		Result struct {
			Status       string `json:"status"`
			LatestLedger uint64 `json:"latestLedger"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&rpcResp); err != nil {
		return HealthResult{Endpoint: rpcURL, Error: fmt.Sprintf("decode: %s", err.Error())}
	}
	if rpcResp.Error != nil {
		return HealthResult{Endpoint: rpcURL, Error: rpcResp.Error.Message}
	}
	if rpcResp.Result.Status != "" && !strings.EqualFold(rpcResp.Result.Status, "healthy") {
		return HealthResult{Endpoint: rpcURL, Error: fmt.Sprintf("rpc status %q", rpcResp.Result.Status)}
	}
	return HealthResult{OK: true, Endpoint: rpcURL, LatestLedger: rpcResp.Result.LatestLedger}
}
