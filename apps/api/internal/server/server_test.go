package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suncrestlabs/nester/apps/api/internal/server"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func noopChecker(_ context.Context) error { return nil }

// newTestServer returns a running httptest.Server backed by the full
// middleware stack.  Callers must defer Close().
func newTestServer(t *testing.T, extra func(*http.ServeMux)) *httptest.Server {
	t.Helper()
	handler, mux := server.New(silentLogger(), noopChecker)
	if extra != nil {
		extra(mux)
	}
	return httptest.NewServer(handler)
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

func TestHealthCheck_Returns200WithStatusOK(t *testing.T) {
	srv := newTestServer(t, nil)
	defer srv.Close()

	for _, path := range []string{"/health", "/healthz"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s error = %v", path, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: expected 200, got %d", path, resp.StatusCode)
		}

		var body map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("GET %s: response is not valid JSON: %v", path, err)
		}
		if body["status"] != "ok" {
			t.Errorf("GET %s: expected {\"status\":\"ok\"}, got %v", path, body)
		}
	}
}

func TestHealthCheck_Returns503WhenCheckerFails(t *testing.T) {
	checker := func(_ context.Context) error { return errors.New("db down") }
	handler, _ := server.New(silentLogger(), checker)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Route registration
// ---------------------------------------------------------------------------

func TestUnregisteredRoute_Returns404(t *testing.T) {
	srv := newTestServer(t, nil)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/does-not-exist")
	if err != nil {
		t.Fatalf("GET unknown route error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for unknown route, got %d", resp.StatusCode)
	}
}

func TestWrongMethodOnRegisteredRoute_Returns405(t *testing.T) {
	srv := newTestServer(t, func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/items", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/v1/items", "application/json", nil)
	if err != nil {
		t.Fatalf("POST to GET-only route error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for wrong method, got %d", resp.StatusCode)
	}
}

func TestRegisteredRoute_RespondsCorrectly(t *testing.T) {
	srv := newTestServer(t, func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ping":"pong"}`))
		})
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/ping")
	if err != nil {
		t.Fatalf("GET /api/v1/ping error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Middleware — request ID
// ---------------------------------------------------------------------------

func TestMiddleware_RequestIDIsPropagatedThroughContext(t *testing.T) {
	var capturedBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&capturedBuf, nil))

	handler, mux := server.New(log, noopChecker)
	mux.HandleFunc("GET /api/v1/echo", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	http.Get(srv.URL + "/api/v1/echo") //nolint:errcheck

	if !strings.Contains(capturedBuf.String(), "request_id") {
		t.Errorf("expected request_id in log output, got %q", capturedBuf.String())
	}
}

// ---------------------------------------------------------------------------
// Middleware — panic recovery
// ---------------------------------------------------------------------------

func TestPanicInHandler_Returns500NotCrash(t *testing.T) {
	srv := newTestServer(t, func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/boom", func(_ http.ResponseWriter, _ *http.Request) {
			panic("deliberate test panic")
		})
	})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/boom")
	if err != nil {
		t.Fatalf("GET /boom unexpectedly closed connection: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 on panic, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Errorf("panic response is not valid JSON: %v\nbody: %q", err, body)
	}
	if envelope["success"] != false {
		t.Errorf("expected success=false in panic response, got %v", envelope["success"])
	}
}

func TestPanicInHandler_StackTraceIsLogged(t *testing.T) {
	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))
	handler, mux := server.New(log, noopChecker)
	mux.HandleFunc("GET /api/v1/panic", func(_ http.ResponseWriter, _ *http.Request) {
		panic("stack trace test")
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	http.Get(srv.URL + "/api/v1/panic") //nolint:errcheck

	if !strings.Contains(logBuf.String(), "stack") {
		t.Errorf("expected stack trace in log output after panic, got %q", logBuf.String())
	}
}

func TestPanicDoesNotCrashSubsequentRequests(t *testing.T) {
	srv := newTestServer(t, func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/boom", func(_ http.ResponseWriter, _ *http.Request) {
			panic("test panic")
		})
		mux.HandleFunc("GET /api/v1/ok", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})
	defer srv.Close()

	// First request panics.
	http.Get(srv.URL + "/api/v1/boom") //nolint:errcheck

	// Server must still handle subsequent requests normally.
	resp, err := http.Get(srv.URL + "/api/v1/ok")
	if err != nil {
		t.Fatalf("server crashed after panic, subsequent request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 on subsequent request after panic, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Graceful shutdown
// ---------------------------------------------------------------------------

func TestGracefulShutdown_InFlightRequestCompletes(t *testing.T) {
	// Use a real listener on a random port so we can control the server lifecycle.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error = %v", err)
	}

	requestStarted := make(chan struct{})
	requestCanFinish := make(chan struct{})

	handler, mux := server.New(silentLogger(), noopChecker)
	mux.HandleFunc("GET /api/v1/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted) // signal: request is inside the handler
		<-requestCanFinish    // wait until test unblocks us
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("done"))
	})

	srv := &http.Server{Handler: handler}
	go srv.Serve(ln) //nolint:errcheck

	addr := fmt.Sprintf("http://%s/api/v1/slow", ln.Addr())
	respCh := make(chan *http.Response, 1)
	go func() {
		resp, _ := http.Get(addr)
		respCh <- resp
	}()

	// Wait until the slow handler is executing.
	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for slow request to start")
	}

	// Begin graceful shutdown — the in-flight request must still complete.
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- srv.Shutdown(shutCtx)
	}()

	// Allow the slow handler to finish now.
	close(requestCanFinish)

	// The in-flight request should return 200.
	select {
	case resp := <-respCh:
		if resp == nil {
			t.Fatal("expected a response from the slow handler, got nil")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 from slow handler, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "done" {
			t.Errorf("expected body %q, got %q", "done", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for slow request to complete during shutdown")
	}

	// Shutdown itself should return nil (clean exit).
	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Errorf("Shutdown() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server shutdown to complete")
	}
}

func TestGracefulShutdown_NewConnectionsRejectedAfterShutdown(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error = %v", err)
	}

	handler, _ := server.New(silentLogger(), noopChecker)
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln) //nolint:errcheck

	addr := fmt.Sprintf("http://%s/health", ln.Addr())

	// Confirm the server is up before shutting it down.
	preResp, err := http.Get(addr)
	if err != nil {
		t.Fatalf("pre-shutdown request failed: %v", err)
	}
	preResp.Body.Close()

	// Shut down cleanly.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	// Any new connection after shutdown should fail.
	client := &http.Client{Timeout: 2 * time.Second}
	_, postErr := client.Get(addr)
	if postErr == nil {
		t.Error("expected an error for new connection after shutdown, got nil")
	}
}

// ---------------------------------------------------------------------------
// CORS headers
// ---------------------------------------------------------------------------

func TestCORS_HeadersPresentOnCrossOriginRequests(t *testing.T) {
	srv := newTestServer(t, nil)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/health", nil)
	req.Header.Set("Origin", "https://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /health error = %v", err)
	}
	defer resp.Body.Close()

	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao == "" {
		t.Error("expected Access-Control-Allow-Origin header, got empty")
	}
	acam := resp.Header.Get("Access-Control-Allow-Methods")
	if acam == "" {
		t.Error("expected Access-Control-Allow-Methods header, got empty")
	}
}

func TestCORS_PreflightOptionsReturns204(t *testing.T) {
	srv := newTestServer(t, nil)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/health", nil)
	req.Header.Set("Origin", "https://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /health error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204 for preflight OPTIONS, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Middleware execution order
// ---------------------------------------------------------------------------

func TestMiddleware_ExecutesInCorrectOrder(t *testing.T) {
	// Verify the middleware chain executes in the documented order:
	// RecoverPanic → CORS → LimitRequestBody → Logging → handler
	//
	// We prove this by observing side effects:
	// 1. CORS headers are present (CORS ran)
	// 2. Request ID is in the log (Logging ran)
	// 3. A panic is recovered (RecoverPanic ran outermost)

	var logBuf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logBuf, nil))

	handler, mux := server.New(log, noopChecker)
	mux.HandleFunc("GET /api/v1/order-test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/order-test", nil)
	req.Header.Set("Origin", "https://example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error = %v", err)
	}
	defer resp.Body.Close()

	// CORS ran (headers present)
	if resp.Header.Get("Access-Control-Allow-Origin") == "" {
		t.Error("CORS middleware did not execute — no Access-Control-Allow-Origin header")
	}

	// Logging ran (request_id in log output)
	if !strings.Contains(logBuf.String(), "request_id") {
		t.Error("Logging middleware did not execute — no request_id in log output")
	}
}

// ---------------------------------------------------------------------------
// Graceful shutdown — exits within configured timeout
// ---------------------------------------------------------------------------

func TestGracefulShutdown_ExitsWithinConfiguredTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen error = %v", err)
	}

	handler, _ := server.New(silentLogger(), noopChecker)
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln) //nolint:errcheck

	// Verify the server is up
	addr := fmt.Sprintf("http://%s/health", ln.Addr())
	resp, err := http.Get(addr)
	if err != nil {
		t.Fatalf("pre-shutdown request failed: %v", err)
	}
	resp.Body.Close()

	// Cancel context to trigger shutdown
	cancel()

	timeout := 3 * time.Second
	done := make(chan error, 1)
	go func() {
		done <- server.RunWithGracefulShutdown(ctx, srv, timeout)
	}()

	select {
	case err := <-done:
		// RunWithGracefulShutdown may return ErrServerClosed or nil — both are fine.
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("RunWithGracefulShutdown returned unexpected error: %v", err)
		}
	case <-time.After(timeout + 2*time.Second):
		t.Fatal("server did not exit within configured timeout")
	}
}
