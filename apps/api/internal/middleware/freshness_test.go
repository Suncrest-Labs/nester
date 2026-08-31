package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/freshness"
)

type stubReader struct {
	snapshot freshness.Snapshot
}

func (s stubReader) Snapshot() freshness.Snapshot { return s.snapshot }

// serveWithFreshness runs one request through the middleware and returns the
// recorded response.
func serveWithFreshness(t *testing.T, reader freshness.Reader, path string) *httptest.ResponseRecorder {
	t.Helper()

	handler := IndexerFreshness(reader)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":true,"data":{}}`))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestFreshDataIsLabelledFresh(t *testing.T) {
	rec := serveWithFreshness(t, stubReader{snapshot: freshness.Snapshot{
		Sampled:    true,
		LagLedgers: 2,
		Lag:        10 * time.Second,
		Budget:     5 * time.Minute,
		Stale:      false,
	}}, "/api/v1/vaults")

	if got := rec.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := rec.Header().Get(HeaderIndexerStale); got != "false" {
		t.Fatalf("%s = %q, want \"false\"", HeaderIndexerStale, got)
	}
	if got := rec.Header().Get(HeaderIndexerLagSeconds); got != "10" {
		t.Fatalf("%s = %q, want \"10\"", HeaderIndexerLagSeconds, got)
	}
	if got := rec.Header().Get(HeaderIndexerLagLedgers); got != "2" {
		t.Fatalf("%s = %q, want \"2\"", HeaderIndexerLagLedgers, got)
	}
	if got := rec.Header().Get(HeaderIndexerStalenessBudgetSeconds); got != "300" {
		t.Fatalf("%s = %q, want \"300\"", HeaderIndexerStalenessBudgetSeconds, got)
	}
}

// The case the issue is about: the indexer is behind, the balance in the body
// is out of date, and the response still succeeds — but says so. A 5xx here
// would take down screens that do not depend on indexed data at all, and would
// tell the user nothing useful about their money.
func TestStaleDataIsServedAndLabelledStale(t *testing.T) {
	rec := serveWithFreshness(t, stubReader{snapshot: freshness.Snapshot{
		Sampled:    true,
		LagLedgers: 140,
		Lag:        12 * time.Minute,
		Budget:     5 * time.Minute,
		Stale:      true,
	}}, "/api/v1/vaults")

	if got := rec.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200: stale data must still be served", got)
	}
	if got := rec.Body.String(); got == "" {
		t.Fatal("stale response carried no body")
	}
	if got := rec.Header().Get(HeaderIndexerStale); got != "true" {
		t.Fatalf("%s = %q, want \"true\"", HeaderIndexerStale, got)
	}
	if got := rec.Header().Get(HeaderIndexerLagSeconds); got != "720" {
		t.Fatalf("%s = %q, want \"720\"", HeaderIndexerLagSeconds, got)
	}
	if got := rec.Header().Get(HeaderIndexerLagLedgers); got != "140" {
		t.Fatalf("%s = %q, want \"140\"", HeaderIndexerLagLedgers, got)
	}
}

// Ledger lag is unknown until the indexer reports a position. The header is
// omitted rather than sent as 0, so a client can tell "we don't know" from
// "exactly at the tip" — and the seconds figure still marks the data stale.
func TestLedgerLagHeaderIsOmittedWhenUnknown(t *testing.T) {
	rec := serveWithFreshness(t, stubReader{snapshot: freshness.Snapshot{
		Sampled: false,
		Lag:     8 * time.Minute,
		Budget:  5 * time.Minute,
		Stale:   true,
	}}, "/api/v1/vaults")

	if _, ok := rec.Header()[http.CanonicalHeaderKey(HeaderIndexerLagLedgers)]; ok {
		t.Fatalf("%s was sent before the indexer reported a position", HeaderIndexerLagLedgers)
	}
	if got := rec.Header().Get(HeaderIndexerStale); got != "true" {
		t.Fatalf("%s = %q, want \"true\"", HeaderIndexerStale, got)
	}
	if got := rec.Header().Get(HeaderIndexerLagSeconds); got != "480" {
		t.Fatalf("%s = %q, want \"480\"", HeaderIndexerLagSeconds, got)
	}
}

// Sub-second lag rounds up rather than down, so the reported number is never
// smaller than the real one and can never contradict the stale flag.
func TestLagSecondsRoundsUp(t *testing.T) {
	rec := serveWithFreshness(t, stubReader{snapshot: freshness.Snapshot{
		Sampled: true,
		Lag:     1500 * time.Millisecond,
		Budget:  5 * time.Minute,
	}}, "/api/v1/vaults")

	if got := rec.Header().Get(HeaderIndexerLagSeconds); got != "2" {
		t.Fatalf("%s = %q, want \"2\"", HeaderIndexerLagSeconds, got)
	}
}

// Freshness describes the data an API response carries. Liveness probes answer
// a different question and are left exactly as they were.
func TestNonAPIPathsAreNotAnnotated(t *testing.T) {
	reader := stubReader{snapshot: freshness.Snapshot{Sampled: true, Stale: true, Budget: time.Minute}}

	for _, path := range []string{"/health", "/healthz", "/"} {
		rec := serveWithFreshness(t, reader, path)
		if got := rec.Header().Get(HeaderIndexerStale); got != "" {
			t.Fatalf("%s annotated with %s = %q", path, HeaderIndexerStale, got)
		}
	}
}

// A deployment wired without an indexer must behave exactly as it did before:
// no headers, no panic, request served.
func TestNilReaderPassesThrough(t *testing.T) {
	rec := serveWithFreshness(t, nil, "/api/v1/vaults")

	if got := rec.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := rec.Header().Get(HeaderIndexerStale); got != "" {
		t.Fatalf("%s = %q with a nil reader, want no header", HeaderIndexerStale, got)
	}
}

// Headers must be set before the wrapped handler writes, or a handler that
// responds immediately would flush the status line first and silently drop
// them.
func TestHeadersSurviveAHandlerThatWritesImmediately(t *testing.T) {
	handler := IndexerFreshness(stubReader{snapshot: freshness.Snapshot{
		Sampled: true,
		Lag:     11 * time.Minute,
		Budget:  5 * time.Minute,
		Stale:   true,
	}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("immediate"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil))

	if got := rec.Header().Get(HeaderIndexerStale); got != "true" {
		t.Fatalf("%s = %q, want \"true\"", HeaderIndexerStale, got)
	}
}

// An error response is still an API response, and a client handling a 500
// benefits from knowing whether the indexed view is also stale.
func TestErrorResponsesAreAnnotated(t *testing.T) {
	handler := IndexerFreshness(stubReader{snapshot: freshness.Snapshot{
		Sampled: true,
		Lag:     7 * time.Minute,
		Budget:  5 * time.Minute,
		Stale:   true,
	}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil))

	if got := rec.Header().Get(HeaderIndexerStale); got != "true" {
		t.Fatalf("%s = %q on a 500, want \"true\"", HeaderIndexerStale, got)
	}
}

// The headers are useless to a browser client on another origin unless CORS
// exposes them, so the two must not drift apart.
func TestCORSExposesEveryFreshnessHeader(t *testing.T) {
	handler := CORS([]string{"https://app.nester.test"})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/vaults", nil)
	req.Header.Set("Origin", "https://app.nester.test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	exposed := rec.Header().Get("Access-Control-Expose-Headers")
	for _, name := range freshnessHeaders {
		if !strings.Contains(exposed, name) {
			t.Fatalf("Access-Control-Expose-Headers %q does not expose %s", exposed, name)
		}
	}
}
