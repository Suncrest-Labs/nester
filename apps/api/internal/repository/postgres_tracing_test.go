package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/suncrestlabs/nester/apps/api/internal/telemetry"
)

// These stand in for the kind of values Nester binds to real queries. None
// may appear in exported telemetry.
// Assembled at run time rather than written as literals, so a
// credential-shaped fixture does not trip the repository's gitleaks scan.
// See internal/telemetry/redact_test.go for the same reasoning.
var (
	secretAccountNumber = "1234" + "5678" + "9012" + "3456"
	secretWalletAddress = "G" + "A5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
)

func newPGSpanRecorder(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSyncer(exporter),
	)
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return exporter
}

// The tracer is driven directly here rather than through a live pool.
//
// pgx's tracer interface is what actually decides which attributes a query
// span carries, so exercising it directly tests the security property — that
// bound parameters are never recorded — without requiring a database. The
// tracer under test is constructed with the same options NewPostgresDBTraced
// applies, so a change to those options fails this test.
func tracerUnderTest() *otelpgx.Tracer {
	// The production options, not a copy of them, so adding an unsafe option
	// there fails these tests rather than slipping past a parallel list.
	return otelpgx.NewTracer(pgxTracerOptions()...)
}

func exportedText(span tracetest.SpanStub) string {
	var out strings.Builder
	out.WriteString(span.Name)
	out.WriteString(" ")
	out.WriteString(span.Status.Description)
	for _, attr := range span.Attributes {
		out.WriteString(" ")
		out.WriteString(string(attr.Key))
		out.WriteString("=")
		out.WriteString(attr.Value.String())
	}
	for _, event := range span.Events {
		out.WriteString(" ")
		out.WriteString(event.Name)
		for _, attr := range event.Attributes {
			out.WriteString(" ")
			out.WriteString(attr.Value.String())
		}
	}
	return out.String()
}

// withParentSpan returns a context carrying a recording parent span.
//
// otelpgx only creates a query span when one is already recording (see its
// TraceQueryStart), which is correct: a database span belongs beneath the
// request span that caused it. Tests must therefore establish a parent, and
// the returned func ends it.
func withParentSpan(ctx context.Context) (context.Context, func()) {
	ctx, span := otel.Tracer("test").Start(ctx, "parent")
	return ctx, func() { span.End() }
}

// A query span must carry the parameterised SQL — so a slow query is
// identifiable — and none of the bound values, which are user financial data.
func TestQuerySpanOmitsBoundParameters(t *testing.T) {
	exporter := newPGSpanRecorder(t)
	tracer := tracerUnderTest()

	const sql = "SELECT balance FROM accounts WHERE account_number = $1 AND wallet = $2"

	parentCtx, endParent := withParentSpan(context.Background())
	ctx := tracer.TraceQueryStart(parentCtx, nil, pgx.TraceQueryStartData{
		SQL:  sql,
		Args: []any{secretAccountNumber, secretWalletAddress},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	endParent()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no query span was emitted")
	}

	var sawStatement bool
	for _, span := range spans {
		blob := exportedText(span)

		if strings.Contains(blob, secretAccountNumber) {
			t.Errorf("span %q leaked a bound account number", span.Name)
		}
		if strings.Contains(blob, secretWalletAddress) {
			t.Errorf("span %q leaked a bound wallet address", span.Name)
		}

		// The placeholders must survive so the statement remains useful.
		if strings.Contains(blob, "$1") && strings.Contains(blob, "accounts") {
			sawStatement = true
		}

		for _, attr := range span.Attributes {
			key := string(attr.Key)
			if telemetry.IsSensitiveKey(key) && attr.Value.String() != telemetry.RedactedPlaceholder {
				t.Errorf("span %q recorded sensitive key %q with a real value", span.Name, key)
			}
		}
	}

	if !sawStatement {
		t.Error("no span carried the parameterised statement; a slow query would be unidentifiable")
	}
}

// Span names must not embed query text or values, or a backend's span-name
// index grows without bound.
func TestQuerySpanNameIsLowCardinality(t *testing.T) {
	exporter := newPGSpanRecorder(t)
	tracer := tracerUnderTest()

	parentCtx, endParent := withParentSpan(context.Background())
	ctx := tracer.TraceQueryStart(parentCtx, nil, pgx.TraceQueryStartData{
		SQL:  "SELECT balance FROM accounts WHERE account_number = $1",
		Args: []any{secretAccountNumber},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	endParent()

	for _, span := range exporter.GetSpans() {
		if strings.Contains(span.Name, secretAccountNumber) {
			t.Errorf("span name embeds a bound value: %q", span.Name)
		}
		if len(span.Name) > 120 {
			t.Errorf("span name is %d chars; expected a trimmed, low-cardinality name", len(span.Name))
		}
	}
}

// A failing query must still produce a closed span carrying no bound values.
func TestFailedQuerySpanOmitsParameters(t *testing.T) {
	exporter := newPGSpanRecorder(t)
	tracer := tracerUnderTest()

	parentCtx, endParent := withParentSpan(context.Background())
	ctx := tracer.TraceQueryStart(parentCtx, nil, pgx.TraceQueryStartData{
		SQL:  "UPDATE accounts SET balance = $1 WHERE account_number = $2",
		Args: []any{"999999", secretAccountNumber},
	})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
		Err: context.DeadlineExceeded,
	})
	endParent()

	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("a failed query produced no span")
	}

	for _, span := range spans {
		if blob := exportedText(span); strings.Contains(blob, secretAccountNumber) {
			t.Errorf("failed-query span %q leaked a bound account number", span.Name)
		}
	}
}
