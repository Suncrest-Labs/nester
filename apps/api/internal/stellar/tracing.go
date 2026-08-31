package stellar

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/suncrestlabs/nester/apps/api/internal/telemetry"
)

// Soroban span attributes (nester#1054).
//
// This is the most sensitive instrumentation surface in the API: the invoker
// holds the operator's secret key and builds signed transaction envelopes.
// The following are recorded, and each was checked individually:
//
//   - contract ID — a public on-chain identifier, low cardinality (a handful
//     of deployed contracts), and the single most useful grouping key.
//   - function name — a compile-time identifier from the contract interface.
//   - RPC method — "simulateTransaction", "sendTransaction", "getTransaction".
//   - transaction hash — public once submitted, and how an operator pivots
//     from a trace to a block explorer. It is not secret: it identifies a
//     transaction, it does not authorise one.
//   - transaction status — "SUCCESS", "FAILED", "NOT_FOUND".
//
// The following are deliberately never recorded:
//
//   - the operator secret or its public address (c.kp) — key material, and
//     the address invites correlating operator activity.
//   - unsigned or signed transaction XDR — a signed envelope embeds the
//     signature and every operation argument, i.e. amounts and destinations.
//   - errorResultXdr from a failed submission — same exposure as above.
//   - simulation transactionData / result XDR — carries contract arguments.
//   - the RPC URL, which may embed an API key in hosted deployments.
//
// Ledger sequence is not recorded: the RPC responses this client parses do
// not return one (getTxResult carries only Status), and inventing a value or
// issuing an extra call purely for telemetry would be worse than omitting it.
const (
	attrContractID = attribute.Key("soroban.contract_id")
	attrFunction   = attribute.Key("soroban.function")
	attrRPCMethod  = attribute.Key("soroban.rpc_method")
	attrTxHash     = attribute.Key("soroban.transaction_hash")
	attrTxStatus   = attribute.Key("soroban.transaction_status")
)

// startContractSpan opens a span covering a whole contract interaction —
// build, simulate, sign, submit — under a name derived from the function
// being invoked.
func startContractSpan(ctx context.Context, operation, contractAddress, functionName string) (context.Context, trace.Span) {
	return otel.Tracer(telemetry.ScopeName).Start(ctx, "soroban."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attrContractID.String(contractAddress),
			attrFunction.String(functionName),
		),
	)
}

// startRPCSpan opens a child span around a single JSON-RPC round trip so the
// waterfall separates simulate from submit from polling.
func startRPCSpan(ctx context.Context, method string) (context.Context, trace.Span) {
	return otel.Tracer(telemetry.ScopeName).Start(ctx, "soroban.rpc/"+method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrRPCMethod.String(method)),
	)
}

// recordTxHash attaches a submitted transaction's hash to the span.
func recordTxHash(span trace.Span, hash string) {
	if hash == "" {
		return
	}
	span.SetAttributes(attrTxHash.String(hash))
}

// recordTxStatus attaches a transaction's lifecycle status to the span.
func recordTxStatus(span trace.Span, status string) {
	if status == "" {
		return
	}
	span.SetAttributes(attrTxStatus.String(status))
}
