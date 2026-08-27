package metrics

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestMetricsNotReachableOnPublicRouter is the security acceptance criterion:
// an unauthenticated request to /metrics on the public interface must not
// return metrics.
//
// The guarantee is structural rather than a matter of rule ordering — the
// public mux never has the pattern registered — so this test asserts the
// property the deployment actually depends on: the public handler does not
// serve exposition data at that path.
func TestMetricsNotReachableOnPublicRouter(t *testing.T) {
	m := New()

	// A stand-in for the public router: the API's routes, with no /metrics
	// among them, exactly as main.go wires it.
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("GET /api/v1/vaults/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	public := m.Middleware(publicMux)(publicMux)

	for _, path := range []string{"/metrics", "/metrics/", "/api/v1/../metrics", "/METRICS"} {
		recorder := httptest.NewRecorder()
		public.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

		if recorder.Code == http.StatusOK {
			t.Fatalf("public router answered %s with 200; it must not serve metrics", path)
		}
		if strings.Contains(recorder.Body.String(), "nester_http_requests_total") {
			t.Fatalf("public router leaked exposition data at %s", path)
		}
	}
}

// TestMetricsServerServesExposition verifies the internal listener returns
// parseable Prometheus exposition format.
func TestMetricsServerServesExposition(t *testing.T) {
	m := New()

	// Record one request so at least one series exists to parse.
	mux := newTestMux()
	m.Middleware(mux)(mux).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/vaults/"+testUUID, nil))

	internal := httptest.NewServer(m.Handler())
	defer internal.Close()

	resp, err := internal.Client().Get(internal.URL + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from the internal endpoint, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// Parsed with Prometheus's own parser rather than string matching, so
	// this fails on malformed exposition rather than merely on missing text.
	// The parser must be built with NewTextParser; its zero value has no
	// name-validation scheme and panics.
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("exposition is not valid Prometheus text format: %v", err)
	}

	for _, required := range []string{
		"nester_http_requests_total",
		"nester_http_request_duration_seconds",
		"nester_http_requests_in_flight",
	} {
		if _, ok := families[required]; !ok {
			t.Errorf("expected %s in the exposition", required)
		}
	}

	// Runtime collectors should be present too; they are the first thing an
	// operator reads when the API misbehaves for non-route reasons.
	if _, ok := families["go_goroutines"]; !ok {
		t.Error("expected go runtime collector metrics in the exposition")
	}

	if strings.Contains(string(body), testUUID) {
		t.Fatal("uuid appeared in the exposition output")
	}
}

// TestMetricsServerOnlyExposesExpectedPaths guards against the internal
// listener quietly gaining a route.
func TestMetricsServerOnlyExposesExpectedPaths(t *testing.T) {
	m := New()
	server := NewServer("127.0.0.1:0", m.Handler(), discardLogger())

	recorder := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404 at / on the internal listener, got %d", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if recorder.Code == http.StatusOK {
		t.Fatal("expected the internal listener to reject POST /metrics")
	}

	recorder = httptest.NewRecorder()
	server.server.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 at /healthz on the internal listener, got %d", recorder.Code)
	}
}

// TestMetricsServerBindsSeparateListener proves the endpoint really is on its
// own port, and that a client hitting the public port cannot reach it.
func TestMetricsServerBindsSeparateListener(t *testing.T) {
	m := New()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	internalAddr := listener.Addr().String()
	listener.Close()

	server := NewServer(internalAddr, m.Handler(), discardLogger())
	go server.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	waitForListener(t, internalAddr)

	resp, err := http.Get("http://" + internalAddr + "/metrics")
	if err != nil {
		t.Fatalf("scrape internal listener: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from the internal listener, got %d", resp.StatusCode)
	}

	// The public server is a different listener on a different port; a
	// request for /metrics there hits the API's own routing, which has no
	// such pattern.
	publicMux := http.NewServeMux()
	publicMux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	public := httptest.NewServer(m.Middleware(publicMux)(publicMux))
	defer public.Close()

	if public.URL == "http://"+internalAddr {
		t.Fatal("public and internal listeners share an address")
	}

	publicResp, err := public.Client().Get(public.URL + "/metrics")
	if err != nil {
		t.Fatalf("request public /metrics: %v", err)
	}
	defer publicResp.Body.Close()

	if publicResp.StatusCode == http.StatusOK {
		t.Fatalf("public listener served /metrics with %d", publicResp.StatusCode)
	}
}

func waitForListener(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("listener at %s never came up", addr)
}
