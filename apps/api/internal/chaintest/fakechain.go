// Package chaintest provides a deterministic stand-in for the chain boundary:
// a fake Soroban JSON-RPC node and a fake Horizon instance that can be made to
// fail in each of the ways a real one does.
//
// It exists so the money path can be driven through every chain failure mode
// without a network, without a live testnet, and without a sleep. Every fault
// below resolves from a signal — a cancelled request context, a closed
// connection, a written status code — never from elapsed wall-clock time, so a
// slow machine changes how long a test takes but never whether it passes.
//
// This package is test support. It is imported only from _test.go files; it
// links into no binary.
package chaintest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// Fault is an injected chain-boundary failure.
type Fault int

const (
	// FaultNone answers normally.
	FaultNone Fault = iota

	// FaultTimeout never responds. The caller's context deadline is what ends
	// the request, which is exactly how a hung node presents: the client gives
	// up without ever learning the outcome.
	FaultTimeout

	// FaultServerError returns HTTP 500 with a JSON body. The body matters:
	// a client that decodes the payload without checking the status code sees
	// a syntactically valid response with every field zeroed, which is the
	// shape that turns an outage into a false success.
	FaultServerError

	// FaultMalformed returns HTTP 200 and a truncated JSON body — a proxy that
	// cut the response short, or a node that emitted a partial payload.
	FaultMalformed

	// FaultSlow responds correctly, but only after Release is called. Callers
	// set a deadline shorter than that, so the client has already given up by
	// the time the answer arrives: a late success must not be observable.
	FaultSlow

	// FaultLostResponse processes the request and then drops the connection
	// without writing a response. This is the dangerous one: the chain may
	// have accepted the transaction, and the caller cannot tell. Anything the
	// request would have done to the chain is recorded before the drop, so a
	// later lookup can find it — the "lost response, eventual success" case.
	FaultLostResponse
)

func (f Fault) String() string {
	switch f {
	case FaultNone:
		return "none"
	case FaultTimeout:
		return "timeout"
	case FaultServerError:
		return "http_5xx"
	case FaultMalformed:
		return "malformed_response"
	case FaultSlow:
		return "slow_response"
	case FaultLostResponse:
		return "lost_response"
	default:
		return fmt.Sprintf("fault(%d)", int(f))
	}
}

// LedgerEntry is what the chain itself holds for a transaction, independent of
// whether the caller ever managed to read it.
type LedgerEntry struct {
	Successful bool
	ClosedAt   time.Time
	ResultXDR  string
}

// Chain is a fake Soroban RPC node plus a fake Horizon instance sharing one
// view of the ledger. Construct with New and Close when done.
type Chain struct {
	soroban *httptest.Server
	horizon *httptest.Server

	mu           sync.Mutex
	sorobanFault Fault
	horizonFault Fault
	ledger       map[string]LedgerEntry
	calls        map[string]int

	releaseOnce sync.Once
	release     chan struct{}

	// SorobanURL and HorizonURL address the two fake endpoints.
	SorobanURL string
	HorizonURL string

	// SubmitHash is the transaction hash sendTransaction reports.
	SubmitHash string

	// OperatorSequence is the account sequence Horizon reports for any
	// account, so transaction building has something to build against.
	OperatorSequence string
}

// New starts the fake chain. The caller closes it (t.Cleanup(chain.Close)).
func New() *Chain {
	c := &Chain{
		ledger:           make(map[string]LedgerEntry),
		calls:            make(map[string]int),
		release:          make(chan struct{}),
		SubmitHash:       "fakechaintxhash0000000000000000000000000000000000000000000000000",
		OperatorSequence: "100",
	}
	c.soroban = httptest.NewServer(http.HandlerFunc(c.serveSoroban))
	c.horizon = httptest.NewServer(http.HandlerFunc(c.serveHorizon))
	c.SorobanURL = c.soroban.URL
	c.HorizonURL = c.horizon.URL
	return c
}

// Close shuts down both servers and releases any handler still parked on a
// FaultSlow, so a test that never called Release cannot leak a goroutine.
func (c *Chain) Close() {
	c.Release()
	c.soroban.Close()
	c.horizon.Close()
}

// SetSorobanFault sets the fault applied to every Soroban JSON-RPC call.
func (c *Chain) SetSorobanFault(f Fault) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sorobanFault = f
}

// SetHorizonFault sets the fault applied to every Horizon request.
func (c *Chain) SetHorizonFault(f Fault) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.horizonFault = f
}

// Land records that the chain closed a transaction in a ledger. It is what the
// chain knows, not what any caller has been told — use it to model "the
// submission actually succeeded even though the response was lost".
func (c *Chain) Land(hash string, successful bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := LedgerEntry{Successful: successful, ClosedAt: time.Now().UTC()}
	if !successful {
		entry.ResultXDR = "AAAAAAAAAAD////9AAAAAA=="
	}
	c.ledger[hash] = entry
}

// Release lets any FaultSlow handler finish. Idempotent.
func (c *Chain) Release() {
	c.releaseOnce.Do(func() { close(c.release) })
}

// Calls reports how many times a Soroban method or Horizon path was served,
// including calls whose response was never delivered. That distinction is the
// point: a lost response still happened.
func (c *Chain) Calls(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls[key]
}

func (c *Chain) record(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls[key]++
}

func (c *Chain) fault(soroban bool) Fault {
	c.mu.Lock()
	defer c.mu.Unlock()
	if soroban {
		return c.sorobanFault
	}
	return c.horizonFault
}

func (c *Chain) lookup(hash string) (LedgerEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.ledger[hash]
	return entry, ok
}

// applyFault performs the injected failure. It reports whether the handler
// should go on to write the normal response.
func (c *Chain) applyFault(w http.ResponseWriter, r *http.Request, f Fault) bool {
	switch f {
	case FaultNone:
		return true

	case FaultTimeout:
		// Park until the caller gives up. Nothing is ever written, so the
		// client observes a deadline rather than a reply — no wall-clock
		// assumption on this side.
		<-r.Context().Done()
		return false

	case FaultSlow:
		select {
		case <-c.release:
			// The caller's deadline has already passed by construction; the
			// response is written into a connection nobody is reading.
			return true
		case <-r.Context().Done():
			return false
		}

	case FaultServerError:
		// The body is a well-formed, entirely empty JSON-RPC envelope on
		// purpose. That is the trap: a client that decodes the payload without
		// first checking the status code gets no decode error, no rpc error
		// object, and a zeroed result — which reads as "the call succeeded and
		// returned nothing" rather than as an outage.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{}}`)
		return false

	case FaultMalformed:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Deliberately truncated: valid JSON up to the cut, then nothing.
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"status":`)
		return false

	case FaultLostResponse:
		// ErrAbortHandler closes the connection without a reply and without a
		// server log line. The chain-side effect has already been recorded by
		// the caller of applyFault.
		panic(http.ErrAbortHandler)

	default:
		return true
	}
}

// ── Soroban JSON-RPC ────────────────────────────────────────────────────────

type rpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (c *Chain) serveSoroban(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = json.Unmarshal(body, &req)
	c.record(req.Method)

	fault := c.fault(true)

	// sendTransaction is recorded as landing BEFORE the fault is applied. A
	// submission whose response is lost still reached the node, and the whole
	// point of the reconciliation path is that the chain remembers it even
	// though the caller does not.
	if req.Method == "sendTransaction" && (fault == FaultLostResponse || fault == FaultSlow) {
		c.Land(c.SubmitHash, true)
	}

	if !c.applyFault(w, r, fault) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	switch req.Method {
	case "getLatestLedger":
		c.writeResult(w, map[string]any{"sequence": 1000})
	case "simulateTransaction":
		c.writeResult(w, map[string]any{
			"latestLedger":    1000,
			"minResourceFee":  "1000",
			"transactionData": "",
		})
	case "sendTransaction":
		c.Land(c.SubmitHash, true)
		c.writeResult(w, map[string]any{"status": "PENDING", "hash": c.SubmitHash})
	case "getTransaction":
		var params struct {
			Hash string `json:"hash"`
		}
		_ = json.Unmarshal(req.Params, &params)
		entry, ok := c.lookup(params.Hash)
		switch {
		case !ok:
			c.writeResult(w, map[string]any{"status": "NOT_FOUND"})
		case entry.Successful:
			c.writeResult(w, map[string]any{"status": "SUCCESS"})
		default:
			c.writeResult(w, map[string]any{"status": "FAILED"})
		}
	default:
		c.writeResult(w, map[string]any{})
	}
}

func (c *Chain) writeResult(w http.ResponseWriter, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  result,
	})
}

// ── Horizon ─────────────────────────────────────────────────────────────────

func (c *Chain) serveHorizon(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/transactions/"):
		c.serveHorizonTransaction(w, r)
	case strings.HasPrefix(r.URL.Path, "/accounts/"):
		c.record("horizon:accounts")
		if !c.applyFault(w, r, c.fault(false)) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sequence": c.OperatorSequence})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (c *Chain) serveHorizonTransaction(w http.ResponseWriter, r *http.Request) {
	c.record("horizon:transactions")

	if !c.applyFault(w, r, c.fault(false)) {
		return
	}

	hash := strings.TrimPrefix(r.URL.Path, "/transactions/")
	entry, ok := c.lookup(hash)
	if !ok {
		// Horizon's "not ingested yet" answer, which the money path must read
		// as still-pending rather than as a failure.
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"successful": entry.Successful,
		"created_at": entry.ClosedAt.Format(time.RFC3339),
		"result_xdr": entry.ResultXDR,
	})
}
