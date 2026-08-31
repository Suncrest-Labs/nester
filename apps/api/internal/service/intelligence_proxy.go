package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

var errIntelligenceNotConfigured = errors.New("intelligence service not configured")

// IntelligenceProxy forwards authenticated requests to the Python intelligence service.
type IntelligenceProxy struct {
	baseURL string
	// upstream is the parsed, configuration-derived origin. Outbound requests
	// are built against it so their scheme and host can never be influenced by
	// the inbound request.
	upstream   *url.URL
	httpClient *http.Client
}

func NewIntelligenceProxy(baseURL string, timeout time.Duration) *IntelligenceProxy {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	trimmed := strings.TrimRight(baseURL, "/")
	p := &IntelligenceProxy{
		baseURL: trimmed,
		httpClient: &http.Client{
			Timeout: timeout,
			// The intelligence service is a fixed internal upstream. Following
			// a redirect would let it — or anything able to impersonate it —
			// steer this client at an arbitrary host while carrying the
			// caller's Authorization header, which is a credential-leak and
			// SSRF pivot in one. There is no legitimate redirect on this path.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	// The upstream origin is resolved once, at construction, from
	// configuration. Every outbound request is then rebuilt against it rather
	// than against anything derived from the inbound request, which is what
	// makes the destination independent of caller-controlled input.
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
		p.upstream = parsed
	}
	return p
}

// SetHTTPClient replaces the HTTP client used for outbound calls. It exists so
// startup can install a metrics-instrumented transport; a nil client is
// ignored so callers need not branch.
func (p *IntelligenceProxy) SetHTTPClient(client *http.Client) {
	if client != nil {
		p.httpClient = client
	}
}

// Forward proxies the incoming request to upstreamPath (may include query string).
func (p *IntelligenceProxy) Forward(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	if p.baseURL == "" {
		http.Error(w, `{"success":false,"error":{"message":"intelligence service not configured"}}`, http.StatusServiceUnavailable)
		return
	}

	req, err := p.buildUpstreamRequest(r, upstreamPath)
	if err != nil {
		http.Error(w, `{"success":false,"error":{"message":"failed to build upstream request"}}`, http.StatusInternalServerError)
		return
	}

	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if uid := r.Header.Get("X-User-Id"); uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	// Propagate the correlation ID so the intelligence service can bind it to
	// its own structured logs and echo it back in responses.
	// The Logging middleware stores the canonical ID in context; prefer that
	// over the raw inbound header so we always forward the real value.
	if rid := logpkg.RequestIDFromContext(r.Context()); rid != "" {
		req.Header.Set("X-Request-ID", rid)
	} else if rid = r.Header.Get("X-Request-ID"); rid != "" {
		req.Header.Set("X-Request-ID", rid)
	}
	resp, err := p.httpClient.Do(req) // #nosec G704 -- the request URL scheme and host come exclusively from p.upstream, parsed once from configuration; buildUpstreamRequest re-verifies the assembled target against that origin and refuses any deviation, and the client does not follow redirects. Proven by intelligence_proxy_ssrf_test.go (nester#1035).
	if err != nil {
		if errorsIsTimeout(err) {
			http.Error(w, `{"success":false,"error":{"message":"intelligence service timed out"}}`, http.StatusGatewayTimeout)
			return
		}
		http.Error(w, `{"success":false,"error":{"message":"intelligence service unavailable"}}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// ForwardJSON proxies the incoming request to upstreamPath the same way
// Forward does, but returns the upstream status code and raw body instead of
// writing directly to the response — for handlers that need to decode the
// upstream JSON and re-wrap it in the standard {success,data}/{success,error}
// envelope rather than passing Python's raw body straight through.
func (p *IntelligenceProxy) ForwardJSON(r *http.Request, upstreamPath string) (int, []byte, error) {
	if p.baseURL == "" {
		return 0, nil, errIntelligenceNotConfigured
	}

	req, err := p.buildUpstreamRequest(r, upstreamPath)
	if err != nil {
		return 0, nil, err
	}

	resp, err := p.httpClient.Do(req) // #nosec G704 -- the request URL scheme and host come exclusively from p.upstream, parsed once from configuration; buildUpstreamRequest re-verifies the assembled target against that origin and refuses any deviation, and the client does not follow redirects. Proven by intelligence_proxy_ssrf_test.go (nester#1035).
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, respBody, nil
}

// buildUpstreamRequest constructs the outbound request to the intelligence
// service.
//
// SSRF containment (nester#1035, gosec G704): the destination is built from
// p.upstream — parsed once from configuration — and never from the inbound
// request. Only the query string is carried across, and it is re-encoded rather
// than concatenated, so a caller cannot inject a host, a scheme, or additional
// path segments into the target. upstreamPath is a compile-time constant chosen
// by the calling handler; it is validated here as defence in depth so that a
// future caller passing something dynamic fails loudly instead of silently
// widening the request surface.
func (p *IntelligenceProxy) buildUpstreamRequest(r *http.Request, upstreamPath string) (*http.Request, error) {
	if p.upstream == nil {
		return nil, errIntelligenceNotConfigured
	}
	if !strings.HasPrefix(upstreamPath, "/") || strings.Contains(upstreamPath, "//") {
		// A path that does not start with a single slash could resolve to a
		// different origin once joined ("//evil.example" is a protocol-relative
		// URL, not a path).
		return nil, fmt.Errorf("invalid upstream path %q", upstreamPath)
	}

	// Copy the configured origin, then set the path. Assigning to Path rather
	// than concatenating strings means url.URL performs the encoding, so path
	// traversal or an embedded query in upstreamPath cannot alter the target.
	target := *p.upstream
	target.Path = strings.TrimRight(p.upstream.Path, "/") + upstreamPath
	target.RawPath = ""

	// The inbound query is forwarded, but re-encoded through url.Values so it
	// is parsed as a query and cannot smuggle a fragment or additional URL
	// structure into the target.
	if r.URL.RawQuery != "" {
		parsedQuery, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return nil, fmt.Errorf("invalid query string: %w", err)
		}
		target.RawQuery = parsedQuery.Encode()
	} else {
		target.RawQuery = ""
	}
	// Defence in depth: confirm the assembled target still points at the
	// configured origin. If any of the steps above were ever changed in a way
	// that allowed the host to move, this check fails closed.
	if target.Scheme != p.upstream.Scheme || target.Host != p.upstream.Host {
		return nil, errors.New("refusing to proxy to a host other than the configured intelligence service")
	}

	var body io.Reader
	if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
		body = r.Body
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), body)
	if err != nil {
		return nil, err
	}

	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	if auth := r.Header.Get("Authorization"); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	if uid := r.Header.Get("X-User-Id"); uid != "" {
		req.Header.Set("X-User-Id", uid)
	}
	return req, nil
}

func errorsIsTimeout(err error) bool {
	if err == nil {
		return false
	}
	if err == context.DeadlineExceeded {
		return true
	}
	type timeout interface{ Timeout() bool }
	if t, ok := err.(timeout); ok && t.Timeout() {
		return true
	}
	return false
}
