package service

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// These tests are the evidence behind the G704 justification in
// buildUpstreamRequest. gosec's taint analysis flags httpClient.Do because the
// request was built in a function that also reads the inbound request; it
// cannot see that the scheme and host come exclusively from configuration.
// Rather than assert that in a comment alone, these tests demonstrate it: every
// case feeds caller-controlled input that would redirect the request under a
// naive implementation, and asserts the target still points at the configured
// upstream.

const testUpstream = "http://intelligence.internal:8000"

func newTestProxy(t *testing.T) *IntelligenceProxy {
	t.Helper()
	return NewIntelligenceProxy(testUpstream, 5*time.Second)
}

// targetOf builds the upstream request and returns its URL.
func targetOf(t *testing.T, p *IntelligenceProxy, inbound *http.Request, path string) *url.URL {
	t.Helper()
	req, err := p.buildUpstreamRequest(inbound, path)
	if err != nil {
		t.Fatalf("buildUpstreamRequest: %v", err)
	}
	return req.URL
}

func TestUpstreamHostIsAlwaysTheConfiguredService(t *testing.T) {
	p := newTestProxy(t)

	// Each of these is a query string an attacker might supply hoping to
	// redirect the outbound request. None may change the destination host.
	hostile := []string{
		"url=http://169.254.169.254/latest/meta-data/",
		"redirect=http://evil.example/",
		"next=//evil.example",
		"host=evil.example",
		"a=1&b=http%3A%2F%2Fevil.example",
		"x=%00%0d%0aHost:evil.example",
	}

	for _, raw := range hostile {
		t.Run(raw, func(t *testing.T) {
			inbound := httptest.NewRequest(http.MethodGet, "/local?"+raw, nil)
			target := targetOf(t, p, inbound, "/v1/analyze")

			if target.Host != "intelligence.internal:8000" {
				t.Fatalf("outbound host was redirected to %q", target.Host)
			}
			if target.Scheme != "http" {
				t.Fatalf("outbound scheme changed to %q", target.Scheme)
			}
			if !strings.HasPrefix(target.Path, "/v1/analyze") {
				t.Fatalf("outbound path was altered: %q", target.Path)
			}
		})
	}
}

func TestMetadataEndpointCannotBeReachedViaQuery(t *testing.T) {
	// The cloud metadata endpoint is the canonical SSRF target. Even naming it
	// directly in the query cannot move the request there, because the host is
	// never taken from caller input.
	p := newTestProxy(t)
	inbound := httptest.NewRequest(http.MethodGet,
		"/local?target=http://169.254.169.254/latest/meta-data/iam/security-credentials/", nil)

	target := targetOf(t, p, inbound, "/v1/analyze")
	if strings.Contains(target.Host, "169.254.169.254") {
		t.Fatalf("request was directed at the metadata endpoint: %s", target)
	}
}

func TestPathTraversalInUpstreamPathRejected(t *testing.T) {
	// upstreamPath is a caller-chosen constant today. This asserts that a
	// future dynamic value cannot escape the configured origin.
	p := newTestProxy(t)
	inbound := httptest.NewRequest(http.MethodGet, "/local", nil)

	for _, bad := range []string{
		"//evil.example/steal", // protocol-relative: resolves to another origin
		"v1/no-leading-slash",  // missing leading slash
		"//",                   // degenerate
	} {
		t.Run(bad, func(t *testing.T) {
			if _, err := p.buildUpstreamRequest(inbound, bad); err == nil {
				t.Fatalf("upstream path %q was accepted", bad)
			}
		})
	}
}

func TestQueryStringIsPreservedForLegitimateUse(t *testing.T) {
	// The hardening must not break the legitimate case: real query parameters
	// still reach the intelligence service.
	p := newTestProxy(t)
	inbound := httptest.NewRequest(http.MethodGet, "/local?symbol=XLM&window=30d", nil)

	target := targetOf(t, p, inbound, "/v1/forecast")
	q := target.Query()
	if q.Get("symbol") != "XLM" {
		t.Fatalf("query parameter lost: %q", target.RawQuery)
	}
	if q.Get("window") != "30d" {
		t.Fatalf("query parameter lost: %q", target.RawQuery)
	}
}

func TestUnconfiguredProxyRefusesToBuildRequests(t *testing.T) {
	// Fail closed when no upstream is configured, rather than constructing a
	// request against an empty origin.
	p := NewIntelligenceProxy("", time.Second)
	inbound := httptest.NewRequest(http.MethodGet, "/local", nil)

	if _, err := p.buildUpstreamRequest(inbound, "/v1/analyze"); err == nil {
		t.Fatal("an unconfigured proxy built an upstream request")
	}
}

func TestProxyDoesNotFollowRedirects(t *testing.T) {
	// A redirect from the upstream would otherwise steer this client — carrying
	// the caller's Authorization header — at an arbitrary host.
	var redirectTargetHit bool

	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		redirectTargetHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer attacker.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL, http.StatusFound)
	}))
	defer upstream.Close()

	p := NewIntelligenceProxy(upstream.URL, 5*time.Second)
	inbound := httptest.NewRequest(http.MethodGet, "/local", nil)
	req, err := p.buildUpstreamRequest(inbound, "/v1/analyze")
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if redirectTargetHit {
		t.Fatal("the client followed a redirect to a different host")
	}
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected the redirect to be surfaced as 302, got %d", resp.StatusCode)
	}
}
