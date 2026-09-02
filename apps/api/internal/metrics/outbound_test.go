package metrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOutboundRequestCountAndDuration(t *testing.T) {
	m := New()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	client := m.InstrumentClient(&http.Client{Timeout: 5 * time.Second}, UpstreamHorizon)

	resp, err := client.Get(upstream.URL + "/order_book?selling_asset_type=native")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if got := counterValue(t, m.Registry(), "nester_outbound_requests_total", map[string]string{
		"upstream":     "horizon",
		"method":       http.MethodGet,
		"status_class": "2xx",
	}); got != 1 {
		t.Fatalf("expected one horizon request counted, got %v", got)
	}

	if got := histogramCount(t, m.Registry(), "nester_outbound_request_duration_seconds", map[string]string{
		"upstream": "horizon",
		"method":   http.MethodGet,
	}); got != 1 {
		t.Fatalf("expected one duration observation, got %d", got)
	}

	// The query string must not survive into any label.
	for _, value := range allLabelValues(t, m.Registry()) {
		if strings.Contains(value, "selling_asset_type") || strings.Contains(value, upstream.URL) {
			t.Fatalf("outbound URL or query leaked into label %q", value)
		}
	}
}

func TestOutboundErrorStatusIsCounted(t *testing.T) {
	m := New()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer upstream.Close()

	client := m.InstrumentClient(&http.Client{Timeout: 5 * time.Second}, UpstreamSorobanRPC)

	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if got := counterValue(t, m.Registry(), "nester_outbound_requests_total", map[string]string{
		"upstream":     "soroban_rpc",
		"status_class": "5xx",
	}); got != 1 {
		t.Fatalf("expected one 5xx counted for soroban_rpc, got %v", got)
	}
}

// TestOutboundTransportErrorIsCounted covers failures that never produce a
// status code, which is the case status_class cannot express.
func TestOutboundTransportErrorIsCounted(t *testing.T) {
	m := New()

	// Bind and immediately release a port so the dial is refused.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadAddr := listener.Addr().String()
	listener.Close()

	client := m.InstrumentClient(&http.Client{Timeout: 2 * time.Second}, UpstreamCoinGecko)

	//nolint:bodyclose // the request fails before a response body exists
	if _, err := client.Get("http://" + deadAddr); err == nil {
		t.Fatal("expected the request to a closed port to fail")
	}

	families, gatherErr := m.Registry().Gather()
	if gatherErr != nil {
		t.Fatalf("gather: %v", gatherErr)
	}

	var total float64
	for _, family := range families {
		if family.GetName() != "nester_outbound_errors_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			if labelsMatch(metric, map[string]string{"upstream": "coingecko"}) {
				total += metric.GetCounter().GetValue()
			}
		}
	}
	if total != 1 {
		t.Fatalf("expected one transport error counted for coingecko, got %v", total)
	}

	// The duration is still observed: a failed call consumed real time and
	// hiding it would understate the latency the caller experienced.
	if got := histogramCount(t, m.Registry(), "nester_outbound_request_duration_seconds", map[string]string{
		"upstream": "coingecko",
	}); got != 1 {
		t.Fatalf("expected the failed request to be timed, got %d", got)
	}
}

// TestOutboundNeverLabelsSecrets is the security assertion for outbound
// instrumentation: credentials travel in headers and URLs, and neither may
// reach a metric.
func TestOutboundNeverLabelsSecrets(t *testing.T) {
	m := New()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	client := m.InstrumentClient(&http.Client{Timeout: 5 * time.Second}, UpstreamDeFiLlama)

	const secret = "sk-ant-super-secret-key"
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/v1/messages?api_key="+secret, strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("X-Api-Key", secret)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	for _, value := range allLabelValues(t, m.Registry()) {
		if strings.Contains(value, secret) || strings.Contains(value, "Bearer") {
			t.Fatalf("credential material reached label value %q", value)
		}
	}
}

func TestUpstreamLabelIsBounded(t *testing.T) {
	m := New()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Every request goes to a different path and host form, but they share
	// one upstream constant, so exactly one upstream label value appears.
	client := m.InstrumentClient(&http.Client{Timeout: 5 * time.Second}, UpstreamDeFiLlama)
	for _, path := range []string{"/pools", "/chart/abc", "/protocol/" + testUUID} {
		resp, err := client.Get(upstream.URL + path)
		if err != nil {
			t.Fatalf("request %s: %v", path, err)
		}
		resp.Body.Close()
	}

	families, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	seen := map[string]struct{}{}
	for _, family := range families {
		if family.GetName() != "nester_outbound_requests_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, pair := range metric.GetLabel() {
				if pair.GetName() == "upstream" {
					seen[pair.GetValue()] = struct{}{}
				}
			}
		}
	}

	if len(seen) != 1 {
		t.Fatalf("expected exactly one upstream label value, got %v", seen)
	}
	if _, ok := seen["defillama"]; !ok {
		t.Fatalf("expected upstream=defillama, got %v", seen)
	}
}

func TestClassifyTransportError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"deadline", context.DeadlineExceeded, errKindTimeout},
		{"canceled", context.Canceled, errKindCanceled},
		{"dns", &net.DNSError{Err: "no such host"}, errKindDNS},
		{"op", &net.OpError{Op: "dial", Err: errors.New("refused")}, errKindConnect},
		{"unknown", errors.New("something else"), errKindOther},
	}

	for _, tc := range cases {
		if got := classifyTransportError(tc.err); got != tc.want {
			t.Errorf("%s: classifyTransportError = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestClassifyTransportErrorNeverReturnsErrorText is the guard that keeps a
// URL-bearing error message out of the kind label.
func TestClassifyTransportErrorNeverReturnsErrorText(t *testing.T) {
	err := errors.New(`Get "https://api.example.com/v1?api_key=SECRET": dial tcp: refused`)

	kind := classifyTransportError(err)
	if strings.Contains(kind, "SECRET") || strings.Contains(kind, "http") {
		t.Fatalf("error text leaked into the kind label: %q", kind)
	}

	allowed := map[string]struct{}{
		errKindTimeout: {}, errKindCanceled: {}, errKindDNS: {},
		errKindConnect: {}, errKindOther: {},
	}
	if _, ok := allowed[kind]; !ok {
		t.Fatalf("kind %q is outside the closed set", kind)
	}
}

func TestInstrumentClientNilIsSafe(t *testing.T) {
	m := New()
	if got := m.InstrumentClient(nil, UpstreamHorizon); got != nil {
		t.Fatalf("expected nil for a nil client, got %v", got)
	}
}

// TestInstrumentClientPreservesExistingTransport proves instrumentation
// wraps rather than replaces a configured transport.
func TestInstrumentClientPreservesExistingTransport(t *testing.T) {
	m := New()

	called := false
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})

	client := m.InstrumentClient(&http.Client{Transport: base}, UpstreamCoinGecko)

	resp, err := client.Get("http://example.invalid/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()

	if !called {
		t.Fatal("the original transport was replaced instead of wrapped")
	}
	if got := counterValue(t, m.Registry(), "nester_outbound_requests_total", map[string]string{
		"upstream": "coingecko",
	}); got != 1 {
		t.Fatalf("expected the wrapped request to be counted, got %v", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
