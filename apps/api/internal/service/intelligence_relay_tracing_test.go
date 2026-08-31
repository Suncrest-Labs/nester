package service_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/suncrestlabs/nester/apps/api/internal/service"
	"github.com/suncrestlabs/nester/apps/api/internal/telemetry"
	logpkg "github.com/suncrestlabs/nester/apps/api/pkg/logger"
)

// traceparentPattern is the W3C format: version-traceid-spanid-flags.
var traceparentPattern = regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-[0-9a-f]{2}$`)

func newRelaySpanRecorder(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})
	return exporter
}

// The relay must emit a traceparent whose IDs match the client span it just
// created. This is the sending half of the Go -> Python link asserted from the
// Go side; the Python suite asserts the receiving half against the same
// header format.
func TestRelayInjectsTraceparentMatchingItsClientSpan(t *testing.T) {
	exporter := newRelaySpanRecorder(t)

	var captured http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"reply":"ok"}`))
	}))
	defer upstream.Close()

	handler := service.NewRelayHandler(upstream.Client(), service.RelayConfig{
		BaseURL: upstream.URL,
		Timeout: 5 * time.Second,
	})

	body, _ := json.Marshal(service.ChatRequest{Message: "how are my savings doing?"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(string(body)))

	ctx := service.WithViewer(req.Context(), service.Viewer{
		UserID:        "user-1",
		WalletAddress: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
	})
	ctx = logpkg.WithRequestID(ctx, "req-relay-1")

	recorder := httptest.NewRecorder()
	handler.RelayChat(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusOK {
		t.Fatalf("relay returned %d, body=%s", recorder.Code, recorder.Body.String())
	}

	traceparent := captured.Get("traceparent")
	if traceparent == "" {
		t.Fatal("no traceparent was forwarded; the Python service would root a new trace")
	}

	matches := traceparentPattern.FindStringSubmatch(traceparent)
	if matches == nil {
		t.Fatalf("traceparent %q is not valid W3C format", traceparent)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 relay client span, got %d", len(spans))
	}
	clientSpan := spans[0]

	if got, want := matches[1], clientSpan.SpanContext.TraceID().String(); got != want {
		t.Errorf("traceparent trace ID = %s, want the client span's %s", got, want)
	}
	if got, want := matches[2], clientSpan.SpanContext.SpanID().String(); got != want {
		t.Errorf("traceparent parent span ID = %s, want the client span's %s", got, want)
	}
}

// X-Request-ID must continue to be forwarded exactly as before. Tracing is
// additive and must not displace the existing correlation mechanism.
func TestRelayStillForwardsRequestIDAlongsideTraceparent(t *testing.T) {
	newRelaySpanRecorder(t)

	var captured http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		_, _ = w.Write([]byte(`{"reply":"ok"}`))
	}))
	defer upstream.Close()

	handler := service.NewRelayHandler(upstream.Client(), service.RelayConfig{
		BaseURL: upstream.URL,
		Timeout: 5 * time.Second,
	})

	body, _ := json.Marshal(service.ChatRequest{Message: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(string(body)))
	ctx := service.WithViewer(req.Context(), service.Viewer{
		UserID:        "user-1",
		WalletAddress: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
	})
	ctx = logpkg.WithRequestID(ctx, "req-relay-2")

	handler.RelayChat(httptest.NewRecorder(), req.WithContext(ctx))

	if got := captured.Get("X-Request-ID"); got != "req-relay-2" {
		t.Errorf("X-Request-ID = %q, want %q; existing behaviour must be preserved", got, "req-relay-2")
	}
	if captured.Get("traceparent") == "" {
		t.Error("traceparent missing; both identifiers should be present")
	}
}

// A relay span must not carry the user's message or the service API key.
func TestRelaySpanCarriesNoSensitiveData(t *testing.T) {
	exporter := newRelaySpanRecorder(t)

	const (
		secretMessage = "my account 1234567890 has a balance of 50000 USD"
		serviceAPIKey = "svc-key-do-not-log-abcdef123456"
	)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"reply":"ok"}`))
	}))
	defer upstream.Close()

	handler := service.NewRelayHandler(upstream.Client(), service.RelayConfig{
		BaseURL: upstream.URL,
		APIKey:  serviceAPIKey,
		Timeout: 5 * time.Second,
	})

	body, _ := json.Marshal(service.ChatRequest{Message: secretMessage})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(string(body)))
	ctx := service.WithViewer(req.Context(), service.Viewer{
		UserID:        "user-1",
		WalletAddress: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
	})

	handler.RelayChat(httptest.NewRecorder(), req.WithContext(ctx))

	for _, span := range exporter.GetSpans() {
		var blob strings.Builder
		blob.WriteString(span.Name)
		blob.WriteString(span.Status.Description)
		for _, attr := range span.Attributes {
			blob.WriteString(string(attr.Key))
			blob.WriteString(attr.Value.String())
		}
		for _, event := range span.Events {
			blob.WriteString(event.Name)
			for _, attr := range event.Attributes {
				blob.WriteString(attr.Value.String())
			}
		}

		exported := blob.String()
		if strings.Contains(exported, secretMessage) {
			t.Errorf("span %q leaked the user's message", span.Name)
		}
		if strings.Contains(exported, serviceAPIKey) {
			t.Errorf("span %q leaked the service API key", span.Name)
		}
	}
}

// An unreachable intelligence service must still produce a closed, errored
// span and the existing 502 response — telemetry must not mask the failure.
func TestRelayUpstreamFailureClosesSpan(t *testing.T) {
	exporter := newRelaySpanRecorder(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	upstreamURL := upstream.URL
	upstream.Close() // now refusing connections

	handler := service.NewRelayHandler(&http.Client{Timeout: time.Second}, service.RelayConfig{
		BaseURL: upstreamURL,
		Timeout: time.Second,
	})

	body, _ := json.Marshal(service.ChatRequest{Message: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(string(body)))
	ctx := service.WithViewer(req.Context(), service.Viewer{
		UserID:        "user-1",
		WalletAddress: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
	})

	recorder := httptest.NewRecorder()
	handler.RelayChat(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusBadGateway && recorder.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 502 or 504; tracing must not change error handling", recorder.Code)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected the relay span to be closed even on failure, got %d spans", len(spans))
	}

	// Asserting only that a span exists is what let missing RecordError calls
	// through: a span with UNSET status is invisible to error-based tail
	// sampling, so the failure would never be retained.
	span := spans[0]
	if span.Status.Code != codes.Error {
		t.Errorf("relay span status = %v, want Error; an unmarked failure is dropped by tail sampling", span.Status.Code)
	}

	var retained bool
	for _, attr := range span.Attributes {
		if attr.Key == telemetry.RetentionAttributeKey && attr.Value.AsBool() {
			retained = true
		}
	}
	if !retained {
		t.Error("failed relay call was not marked for retention")
	}
}

// An upstream 5xx is not a Go error, so it has to be marked on the span
// explicitly or the trace looks successful.
func TestRelayUpstream5xxMarksSpanErrored(t *testing.T) {
	exporter := newRelaySpanRecorder(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer upstream.Close()

	handler := service.NewRelayHandler(upstream.Client(), service.RelayConfig{
		BaseURL: upstream.URL,
		Timeout: 5 * time.Second,
	})

	body, _ := json.Marshal(service.ChatRequest{Message: "hello"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/intelligence/chat", strings.NewReader(string(body)))
	ctx := service.WithViewer(req.Context(), service.Viewer{
		UserID:        "user-1",
		WalletAddress: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
	})

	recorder := httptest.NewRecorder()
	handler.RelayChat(recorder, req.WithContext(ctx))

	if recorder.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502; existing behaviour must be preserved", recorder.Code)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("span status = %v, want Error for an upstream 5xx", spans[0].Status.Code)
	}
}
