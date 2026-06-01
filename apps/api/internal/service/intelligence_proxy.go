package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

	target, err := url.Parse(p.baseURL + upstreamPath)
	if err != nil {
		http.Error(w, `{"success":false,"error":{"message":"invalid upstream path"}}`, http.StatusInternalServerError)
		return
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
