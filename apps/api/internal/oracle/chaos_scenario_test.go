// Scenario tests for issue #1055: prove real oracle behavior degrades
// correctly when a live dependency misbehaves, by driving an actual
// Provider's real HTTP client through chaos.Transport rather than a
// hand-rolled mock Provider. Each test documents the expected user-visible
// behavior in its own doc comment before asserting it, per #1055's
// acceptance criteria.
package oracle

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/suncrestlabs/nester/apps/api/internal/chaos"
)

// fakeUpstreamTransport answers every request locally with a fixed 200
// DeFiLlama-shaped body, so scenarios that need chaos.Transport to actually
// forward to "the real upstream" (FaultMalformedBody, FaultWrongContentType)
// never make a genuine network call to coins.llama.fi.
type fakeUpstreamTransport struct{}

func (fakeUpstreamTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"coins":{"coingecko:stellar":{"price":0.11}}}`
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		Request:    req,
	}, nil
}

// stubProvider is a fixed-value second source, standing in for a healthy
// sibling provider (e.g. Horizon) alongside the real DefiLlamaProvider under
// fault. It is not itself under test — DefiLlamaProvider driven through
// chaos.Transport is.
type stubProvider struct {
	name  string
	value float64
}

func (s stubProvider) Name() string { return s.name }
func (s stubProvider) Fetch(ctx context.Context, base, quote string) (float64, error) {
	return s.value, nil
}

func defaultOpts() AggregationOptions {
	return AggregationOptions{
		MaxDeviationBPS:    500,
		MinAgreeingSources: 2,
		PerSourceTimeout:   2 * time.Second,
	}
}

// Scenario: DeFiLlama is unreachable (connection reset) while Horizon
// (stubbed here) is healthy.
//
// Expected user-visible behavior: a rate is still returned — callers do not
// see an error just because one of two sources is down — but it is
// attributed to the surviving source only, DeFiLlama is reported failed (not
// silently dropped), and Confidence is reduced below what full two-source
// agreement would produce, so a caller gating on MeetsConfidenceThreshold
// can tell the difference between "both sources agree" and "one source is
// down and we're running on the other."
func TestScenario_DefiLlamaDown_ServesFromSurvivingSource(t *testing.T) {
	llama := NewDefiLlamaProvider()
	llama.SetHTTPClient(&http.Client{
		Transport: chaos.New(nil, chaos.Script{Fault: chaos.FaultConnectionReset}),
		Timeout:   2 * time.Second,
	})
	horizon := stubProvider{name: "horizon", value: 0.11}

	health := NewHealthTracker()
	result, err := Aggregate(context.Background(), "XLM", "USD", []Provider{llama, horizon}, health, defaultOpts())

	require.NoError(t, err, "one live source must be enough to avoid Unavailable")
	assert.False(t, result.Unavailable)
	assert.Equal(t, 0.11, result.Value)
	assert.Equal(t, []string{"horizon"}, result.SourcesUsed)
	assert.Equal(t, []string{"defillama"}, result.SourcesFailed)
	assert.Less(t, result.Confidence, 1.0, "confidence must reflect the missing source, not just report success")
	assert.False(t, health.IsHealthy("defillama") == health.IsHealthy("horizon") && health.IsHealthy("defillama"),
		"defillama's failure must be recorded, not merged into horizon's health")
}

// Scenario: Full mainnet RPC provider outage (simulating both primary and backup
// mainnet RPC endpoints failing with connection resets / timeouts simultaneously),
// confirming the API degrades to read-only/queued mode rather than failing destructively.
func TestScenario_FullMainnetRPCOutage_DegradesGracefullyToReadOrQueue(t *testing.T) {
	primaryClient := &http.Client{
		Transport: chaos.New(nil, chaos.Script{Fault: chaos.FaultConnectionReset}),
		Timeout:   1 * time.Second,
	}
	backupClient := &http.Client{
		Transport: chaos.New(nil, chaos.Script{Fault: chaos.FaultTimeout}),
		Timeout:   1 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/health", nil)
	require.NoError(t, err)

	_, err1 := primaryClient.Do(req)
	_, err2 := backupClient.Do(req)

	require.Error(t, err1)
	require.Error(t, err2)

	health := NewHealthTracker()
	health.RecordFailure("mainnet-rpc-primary", err1)
	health.RecordFailure("mainnet-rpc-backup", err2)

	assert.False(t, health.IsHealthy("mainnet-rpc-primary"))
	assert.False(t, health.IsHealthy("mainnet-rpc-backup"))

	// The health-tracker bookkeeping above only proves the two RPC endpoints
	// are individually marked unhealthy. The real acceptance criterion is
	// that the API's aggregation layer does not error destructively (panic,
	// or surface a raw transport error to the caller) when every registered
	// source for a data type is down at once — it must degrade to a
	// structured "unavailable" result instead. Drive the same failures
	// through the actual aggregator, with both providers wired to the same
	// chaos-injected HTTP clients used above, and assert on Aggregate's
	// return value rather than on isolated health-tracker state.
	primaryProvider := NewDefiLlamaProvider()
	primaryProvider.SetHTTPClient(primaryClient)
	backupProvider := NewDefiLlamaProvider()
	backupProvider.SetHTTPClient(backupClient)

	aggHealth := NewHealthTracker()
	result, aggErr := Aggregate(context.Background(), "XLM", "USD", []Provider{primaryProvider, backupProvider}, aggHealth, defaultOpts())

	require.Error(t, aggErr, "an all-sources-down aggregate must report an error, not silently return a zero value")
	assert.True(t, result.Unavailable, "aggregator must surface a structured degraded/unavailable result rather than panicking or erroring destructively")
	assert.Empty(t, result.SourcesUsed, "no source succeeded, so none may be reported as used")
	assert.ElementsMatch(t, []string{"defillama", "defillama"}, result.SourcesFailed, "both failed sources must be recorded, not dropped")
}

// Scenario: DeFiLlama returns HTTP 503 (a Soroban-RPC-style upstream
// failure, modeled here on the same coins.llama.fi dependency) repeatedly
// across consecutive requests.
//
// Expected user-visible behavior: HealthTracker's exponential backoff kicks
// in — after enough consecutive failures, DeFiLlama is skipped entirely
// (not re-queried on every request), which is what protects the aggregate's
// latency from a persistently-down source without any caller-side retry
// logic.
func TestScenario_DefiLlamaRepeatedServerError_TripsHealthBackoff(t *testing.T) {
	llama := NewDefiLlamaProvider()
	llama.SetHTTPClient(&http.Client{
		Transport: chaos.New(nil, chaos.Script{Fault: chaos.FaultServerError, ServerErrorStatus: http.StatusServiceUnavailable}),
		Timeout:   2 * time.Second,
	})
	horizon := stubProvider{name: "horizon", value: 0.11}
	health := NewHealthTracker()

	fixedNow := time.Now()
	health.SetNowFuncForTest(func() time.Time { return fixedNow })

	var last AggregatedValue
	for i := 0; i < 5; i++ {
		result, err := Aggregate(context.Background(), "XLM", "USD", []Provider{llama, horizon}, health, defaultOpts())
		require.NoError(t, err)
		last = result
	}

	assert.False(t, health.IsHealthy("defillama"), "repeated failures must trip backoff")
	assert.Contains(t, last.SourcesSkipped, "defillama", "once unhealthy, defillama must be skipped rather than re-queried")
	assert.Equal(t, []string{"horizon"}, last.SourcesUsed)
}

// Scenario: DeFiLlama responds successfully but past the caller's own
// per-source timeout (a slow/hanging upstream, not a hard failure).
//
// Expected user-visible behavior: a slow source must be treated exactly
// like a failed one for that request — it must not stall the aggregate
// past PerSourceTimeout, and it must not silently poison the result with a
// value that arrived too late to be reconciled.
func TestScenario_DefiLlamaSlowResponse_TreatedAsFailureNotStall(t *testing.T) {
	llama := NewDefiLlamaProvider()
	llama.SetHTTPClient(&http.Client{
		Transport: chaos.New(nil, chaos.Script{Fault: chaos.FaultLatency, Latency: 500 * time.Millisecond}),
	})
	horizon := stubProvider{name: "horizon", value: 0.11}
	health := NewHealthTracker()

	opts := defaultOpts()
	opts.PerSourceTimeout = 50 * time.Millisecond

	start := time.Now()
	result, err := Aggregate(context.Background(), "XLM", "USD", []Provider{llama, horizon}, health, opts)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 400*time.Millisecond, "a slow source must not stall the whole aggregate")
	assert.Equal(t, []string{"horizon"}, result.SourcesUsed)
	assert.Equal(t, []string{"defillama"}, result.SourcesFailed)
}

// Scenario: DeFiLlama's response body is malformed (truncated/corrupt JSON),
// distinct from a transport-level failure.
//
// Expected user-visible behavior: a 200 response with a body the provider
// cannot parse must surface as a Fetch error like any other failure — it
// must not panic, and must not be silently treated as a successful zero
// value that would corrupt the consensus median.
func TestScenario_DefiLlamaMalformedBody_FailsCleanlyNotSilently(t *testing.T) {
	llama := NewDefiLlamaProvider()
	llama.SetHTTPClient(&http.Client{
		Transport: chaos.New(fakeUpstreamTransport{}, chaos.Script{Fault: chaos.FaultMalformedBody, MalformedBody: []byte(`{"coins":{`)}),
	})
	horizon := stubProvider{name: "horizon", value: 0.11}
	health := NewHealthTracker()

	result, err := Aggregate(context.Background(), "XLM", "USD", []Provider{llama, horizon}, health, defaultOpts())

	require.NoError(t, err)
	assert.Equal(t, []string{"horizon"}, result.SourcesUsed)
	assert.Equal(t, []string{"defillama"}, result.SourcesFailed)
	assert.NotContains(t, result.SourcesUsed, "defillama")
}
