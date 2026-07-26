package performance

import (
	"context"
	"time"
)

// APY source precedence
// =====================
//
// Nester has two APY sources and they disagree constantly:
//
//  1. On-chain adapter APY, read from yield_registry (which pulls it from each
//     source's adapter). Authoritative but sometimes unknown — a pool adapter
//     with a young position reports Unavailable rather than a noisy number.
//  2. Off-chain DeFiLlama APY, polled every 6h by service.APYService. Always
//     present, never authoritative: it is a third-party aggregate over pools
//     that are not necessarily the ones this vault holds.
//
// Two APY numbers with no stated precedence is a known way to trigger
// rebalance loops: the allocator moves capital toward whichever source is
// reading high this tick, then back again when the other one wins. So the
// rule is explicit and one-directional:
//
//	On-chain adapter APY is AUTHORITATIVE for every rebalancing decision.
//	DeFiLlama APY is DISPLAY-ONLY and never feeds allocation targets.
//
// When on-chain APY is unknown (confidence Unavailable) or stale, the
// allocator does not fall back to DeFiLlama. It holds the source's existing
// allocation steady instead — see AllocationStrategy's handling of Paused and
// Degraded sources. Freezing an allocation is cheap; churning it on a number
// that describes a different pool is not.
//
// DeFiLlama remains useful and is deliberately kept: it powers historical APY
// charts, the protocol-health TVL checks, and operator-facing comparisons that
// flag when on-chain and market-wide yields diverge. It just never decides
// where capital goes.

// APYConfidence mirrors nester_common::adapters::ApyConfidence on-chain.
// Unavailable is distinct from a zero APY: unknown is not zero.
type APYConfidence string

const (
	// APYProtocolReported — rate read straight from the protocol.
	APYProtocolReported APYConfidence = "protocol_reported"
	// APYDerived — rate derived from observed position growth over a
	// sufficient window.
	APYDerived APYConfidence = "derived"
	// APYUnavailable — no meaningful rate. Callers must not treat the
	// accompanying value as a rate of zero.
	APYUnavailable APYConfidence = "unavailable"
)

// MaxOnChainAPYAge bounds how old an on-chain reading may be before the
// allocator stops trusting it. Registry refreshes are permissionless and
// expected far more often than this.
const MaxOnChainAPYAge = 24 * time.Hour

// APYReading is a single APY observation with its provenance.
type APYReading struct {
	BPS        uint32
	Confidence APYConfidence
	ObservedAt time.Time
}

// Usable reports whether this reading may drive a rebalancing decision:
// the confidence must not be Unavailable and the observation must be fresh.
func (r APYReading) Usable(now time.Time) bool {
	if r.Confidence == APYUnavailable || r.Confidence == "" {
		return false
	}
	if r.ObservedAt.IsZero() {
		return false
	}
	return now.Sub(r.ObservedAt) <= MaxOnChainAPYAge
}

// ConfidenceFromBPSFlag maps the registry's on-chain confidence discriminant
// (0 = ProtocolReported, 1 = Derived, 2 = Unavailable) to an APYConfidence.
// Any unrecognised value is treated as Unavailable — an unknown provenance is
// not a licence to trust the number.
func ConfidenceFromBPSFlag(flag uint32) APYConfidence {
	switch flag {
	case 0:
		return APYProtocolReported
	case 1:
		return APYDerived
	default:
		return APYUnavailable
	}
}

// OnChainAPYReader fetches an adapter-backed APY reading for a protocol from
// yield_registry. Implementations must surface the on-chain confidence value
// rather than flattening an unknown reading to zero.
type OnChainAPYReader interface {
	SourceAPYReading(ctx context.Context, protocolID string) (APYReading, error)
}

// RebalanceAPYResolver answers "what APY should the allocator use for this
// protocol?" It implements the precedence rule documented above.
type RebalanceAPYResolver struct {
	onChain OnChainAPYReader
	now     func() time.Time
}

// NewRebalanceAPYResolver builds a resolver over the given on-chain reader.
// There is deliberately no DeFiLlama dependency here: an off-chain aggregate
// must not be able to influence allocation targets even by accident.
func NewRebalanceAPYResolver(onChain OnChainAPYReader) *RebalanceAPYResolver {
	return &RebalanceAPYResolver{
		onChain: onChain,
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// SetClock overrides the time source. Test-only.
func (r *RebalanceAPYResolver) SetClock(clock func() time.Time) {
	r.now = clock
}

// APYForRebalance returns the APY the allocator should act on, and whether it
// may act at all.
//
// ok == false means "hold this source's current allocation steady": either the
// adapter reported Unavailable, the reading is stale, or the read failed. It
// never means "assume zero" and never falls back to DeFiLlama.
func (r *RebalanceAPYResolver) APYForRebalance(ctx context.Context, protocolID string) (bps uint32, ok bool) {
	if r.onChain == nil {
		return 0, false
	}
	reading, err := r.onChain.SourceAPYReading(ctx, protocolID)
	if err != nil {
		return 0, false
	}
	if !reading.Usable(r.now()) {
		return 0, false
	}
	return reading.BPS, true
}
