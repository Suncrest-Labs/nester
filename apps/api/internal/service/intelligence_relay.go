package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultFailureThreshold = 5
	defaultFailureWindow    = 30 * time.Second
	defaultOpenDuration     = 60 * time.Second
	defaultTimeout          = 5 * time.Second
)

type viewerContextKey struct{}

type Viewer struct {
	UserID        string
	WalletAddress string
}

type RelayConfig struct {
	BaseURL          string
	APIKey           string
	Timeout          time.Duration
	FailureThreshold int
	FailureWindow    time.Duration
	OpenDuration     time.Duration
}

type ChatRequest struct {
	Message        string `json:"message"`
	WalletAddress  string `json:"wallet_address,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
}

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type RelayHandler struct {
	doer HTTPDoer
	cfg  RelayConfig
	now  func() time.Time

	mu            sync.Mutex
	failures      []time.Time
	circuitOpened time.Time
}

func WithViewer(ctx context.Context, viewer Viewer) context.Context {
	return context.WithValue(ctx, viewerContextKey{}, viewer)
}

func ViewerFromContext(ctx context.Context) (Viewer, bool) {
	viewer, ok := ctx.Value(viewerContextKey{}).(Viewer)
	if !ok {
		return Viewer{}, false
	}
	return viewer, true
}

func NewRelayHandler(doer HTTPDoer, cfg RelayConfig) *RelayHandler {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = defaultFailureThreshold
	}
	if cfg.FailureWindow <= 0 {
		cfg.FailureWindow = defaultFailureWindow
	}
	if cfg.OpenDuration <= 0 {
		cfg.OpenDuration = defaultOpenDuration
	}

	return &RelayHandler{
		doer: doer,
		cfg:  cfg,
		now:  time.Now,
	}
}

func (h *RelayHandler) RelayChat(w http.ResponseWriter, r *http.Request) {
	viewer, ok := ViewerFromContext(r.Context())
	if !ok || strings.TrimSpace(viewer.WalletAddress) == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if strings.TrimSpace(req.Message) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message is required"})
		return
	}

	if h.circuitOpen() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "intelligence relay temporarily unavailable"})
		return
	}

	forwarded := ChatRequest{
		Message:        req.Message,
		WalletAddress:  viewer.WalletAddress,
		ConversationID: req.ConversationID,
	}

	payload, err := json.Marshal(forwarded)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to prepare relay request"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.Timeout)
	defer cancel()

	upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.cfg.BaseURL, "/")+"/intelligence/chat", bytes.NewReader(payload))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to prepare relay request"})
		return
	}

	upstreamReq.Header.Set("Content-Type", "application/json")
	upstreamReq.Header.Set("Accept", "application/json")
	if h.cfg.APIKey != "" {
		upstreamReq.Header.Set("Authorization", "Bearer "+h.cfg.APIKey)
	}

	resp, err := h.doer.Do(upstreamReq)
	if err != nil {
		h.recordFailure()
		if isTimeoutError(err) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "intelligence service timed out"})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "intelligence service unavailable"})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		h.recordFailure()
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to read intelligence response"})
		return
	}

	if !json.Valid(body) {
		h.recordFailure()
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "intelligence service returned invalid JSON"})
		return
	}

	switch {
	case resp.StatusCode >= 500:
		h.recordFailure()
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "intelligence service failed"})
		return
	case resp.StatusCode >= 400:
		h.resetFailures()
		proxyJSON(w, resp.StatusCode, body)
		return
	default:
		h.resetFailures()
		proxyJSON(w, resp.StatusCode, body)
	}
}

func (h *RelayHandler) circuitOpen() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.now().Before(h.circuitOpened)
}

func (h *RelayHandler) recordFailure() {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	cutoff := now.Add(-h.cfg.FailureWindow)
	filtered := h.failures[:0]
	for _, failure := range h.failures {
		if failure.After(cutoff) {
			filtered = append(filtered, failure)
		}
	}

	filtered = append(filtered, now)
	h.failures = filtered

	if len(h.failures) >= h.cfg.FailureThreshold {
		h.circuitOpened = now.Add(h.cfg.OpenDuration)
		h.failures = nil
	}
}

func (h *RelayHandler) resetFailures() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures = nil
	h.circuitOpened = time.Time{}
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func proxyJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
