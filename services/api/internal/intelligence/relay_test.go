package intelligence

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (fn roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestRelayChatForwardsAuthenticatedWalletContext(t *testing.T) {
	var called atomic.Int32
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called.Add(1)

		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}

		if req.URL.String() != "http://prometheus.test/intelligence/chat" {
			t.Fatalf("unexpected upstream url: %s", req.URL.String())
		}

		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("expected json content type, got %s", got)
		}

		if got := req.Header.Get("Authorization"); got != "Bearer relay-key" {
			t.Fatalf("expected authorization header, got %s", got)
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		raw := string(body)
		if !strings.Contains(raw, `"message":"Should I rebalance?"`) {
			t.Fatalf("forwarded body missing message: %s", raw)
		}
		if !strings.Contains(raw, `"wallet_address":"GAUTHENTICATED"`) {
			t.Fatalf("forwarded body missing authenticated wallet: %s", raw)
		}
		if strings.Contains(raw, "GCLIENTSUPPLIED") {
			t.Fatalf("forwarded body leaked client wallet: %s", raw)
		}

		return jsonResponse(http.StatusOK, `{"response":"stay balanced","confidence":0.82}`), nil
	}), RelayConfig{
		BaseURL: "http://prometheus.test",
		APIKey:  "relay-key",
		Timeout: time.Second,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"Should I rebalance?","wallet_address":"GCLIENTSUPPLIED"}`))
	req = req.WithContext(WithViewer(req.Context(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if called.Load() != 1 {
		t.Fatalf("expected one upstream call, got %d", called.Load())
	}

	if body := strings.TrimSpace(w.Body.String()); body != `{"response":"stay balanced","confidence":0.82}` {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestRelayChatReturnsGatewayTimeoutWhenUpstreamTimesOut(t *testing.T) {
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	}), RelayConfig{
		BaseURL: "http://prometheus.test",
		Timeout: 15 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(WithViewer(req.Context(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "timed out") {
		t.Fatalf("expected timeout error body, got %s", w.Body.String())
	}
}

func TestRelayChatProxiesSuccessfulResponse(t *testing.T) {
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusCreated, `{"response":"ok","sources":["market","portfolio"]}`), nil
	}), RelayConfig{
		BaseURL: "http://prometheus.test",
		Timeout: time.Second,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(WithViewer(req.Context(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}

	if body := strings.TrimSpace(w.Body.String()); body != `{"response":"ok","sources":["market","portfolio"]}` {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestRelayChatReturnsUpstreamClientError(t *testing.T) {
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadRequest, `{"error":"message is required"}`), nil
	}), RelayConfig{
		BaseURL: "http://prometheus.test",
		Timeout: time.Second,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(WithViewer(req.Context(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	if body := strings.TrimSpace(w.Body.String()); body != `{"error":"message is required"}` {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestRelayChatReturnsBadGatewayForUpstreamServerError(t *testing.T) {
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusInternalServerError, `{"error":"boom"}`), nil
	}), RelayConfig{
		BaseURL: "http://prometheus.test",
		Timeout: time.Second,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(WithViewer(req.Context(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
}

func TestRelayChatReturnsBadGatewayForMalformedJSON(t *testing.T) {
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusOK, `{"response":`), nil
	}), RelayConfig{
		BaseURL: "http://prometheus.test",
		Timeout: time.Second,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(WithViewer(req.Context(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}

	if !strings.Contains(w.Body.String(), "invalid JSON") {
		t.Fatalf("expected invalid json error body, got %s", w.Body.String())
	}
}

func TestRelayChatRejectsUnauthenticatedRequests(t *testing.T) {
	var called atomic.Int32
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called.Add(1)
		return jsonResponse(http.StatusOK, `{"response":"ok"}`), nil
	}), RelayConfig{
		BaseURL: "http://prometheus.test",
		Timeout: time.Second,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}

	if called.Load() != 0 {
		t.Fatalf("expected no upstream calls, got %d", called.Load())
	}
}

func TestRelayChatOpensCircuitAfterRepeatedFailures(t *testing.T) {
	var called atomic.Int32
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called.Add(1)
		return nil, errors.New("dial tcp: unreachable")
	}), RelayConfig{
		BaseURL:          "http://prometheus.test",
		Timeout:          time.Second,
		FailureThreshold: 2,
		FailureWindow:    time.Minute,
		OpenDuration:     time.Minute,
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
		req = req.WithContext(WithViewer(req.Context(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
		w := httptest.NewRecorder()
		handler.RelayChat(w, req)

		if w.Code != http.StatusBadGateway {
			t.Fatalf("expected 502 on failure, got %d", w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(WithViewer(req.Context(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 with open circuit, got %d", w.Code)
	}

	if called.Load() != 2 {
		t.Fatalf("expected no third upstream call, got %d total", called.Load())
	}
}

func TestRelayChatReturnsGatewayTimeoutForTransportTimeoutError(t *testing.T) {
	handler := NewRelayHandler(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, timeoutError{}
	}), RelayConfig{
		BaseURL: "http://prometheus.test",
		Timeout: time.Second,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(`{"message":"hello"}`))
	req = req.WithContext(WithViewer(context.Background(), Viewer{UserID: "user-1", WalletAddress: "GAUTHENTICATED"}))
	w := httptest.NewRecorder()

	handler.RelayChat(w, req)

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", w.Code)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
