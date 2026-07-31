package service

import (
	"context"
	"errors"
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
	baseURL    string
	httpClient *http.Client
}

func NewIntelligenceProxy(baseURL string, timeout time.Duration) *IntelligenceProxy {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &IntelligenceProxy{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: timeout,
		},
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
	resp, err := p.httpClient.Do(req)
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

	resp, err := p.httpClient.Do(req)
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

func (p *IntelligenceProxy) buildUpstreamRequest(r *http.Request, upstreamPath string) (*http.Request, error) {
	target, err := url.Parse(p.baseURL + upstreamPath)
	if err != nil {
		return nil, err
	}
	if r.URL.RawQuery != "" {
		target.RawQuery = r.URL.RawQuery
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
