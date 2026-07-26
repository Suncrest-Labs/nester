package performance

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubOnChainReader struct {
	reading APYReading
	err     error
	calls   int
}

func (s *stubOnChainReader) SourceAPYReading(_ context.Context, _ string) (APYReading, error) {
	s.calls++
	if s.err != nil {
		return APYReading{}, s.err
	}
	return s.reading, nil
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestAPYForRebalanceUsesFreshProtocolReported(t *testing.T) {
	now := time.Now().UTC()
	stub := &stubOnChainReader{reading: APYReading{
		BPS:        640,
		Confidence: APYProtocolReported,
		ObservedAt: now.Add(-time.Hour),
	}}
	r := NewRebalanceAPYResolver(stub)
	r.SetClock(fixedClock(now))

	bps, ok := r.APYForRebalance(context.Background(), "blend")
	if !ok {
		t.Fatal("fresh protocol-reported APY must be usable")
	}
	if bps != 640 {
		t.Fatalf("got %d, want 640", bps)
	}
}

func TestAPYForRebalanceRejectsUnavailable(t *testing.T) {
	now := time.Now().UTC()
	// An adapter reporting Unavailable carries a zero BPS. Treating that as a
	// real zero is exactly the bug this rule exists to prevent.
	stub := &stubOnChainReader{reading: APYReading{
		BPS:        0,
		Confidence: APYUnavailable,
		ObservedAt: now,
	}}
	r := NewRebalanceAPYResolver(stub)
	r.SetClock(fixedClock(now))

	if _, ok := r.APYForRebalance(context.Background(), "soroswap"); ok {
		t.Fatal("unavailable APY must not be usable for rebalancing")
	}
}

func TestAPYForRebalanceRejectsStaleReading(t *testing.T) {
	now := time.Now().UTC()
	stub := &stubOnChainReader{reading: APYReading{
		BPS:        800,
		Confidence: APYDerived,
		ObservedAt: now.Add(-MaxOnChainAPYAge - time.Minute),
	}}
	r := NewRebalanceAPYResolver(stub)
	r.SetClock(fixedClock(now))

	if _, ok := r.APYForRebalance(context.Background(), "blend"); ok {
		t.Fatal("stale reading must not drive a rebalance")
	}
}

func TestAPYForRebalanceRejectsReadError(t *testing.T) {
	r := NewRebalanceAPYResolver(&stubOnChainReader{err: errors.New("rpc down")})
	if _, ok := r.APYForRebalance(context.Background(), "blend"); ok {
		t.Fatal("a failed read must not yield a usable APY")
	}
}

// The resolver must have no DeFiLlama fallback path at all: when on-chain data
// is unusable the answer is "hold", never an off-chain substitute.
func TestAPYForRebalanceHasNoOffChainFallback(t *testing.T) {
	now := time.Now().UTC()
	stub := &stubOnChainReader{reading: APYReading{
		BPS:        0,
		Confidence: APYUnavailable,
		ObservedAt: now,
	}}
	r := NewRebalanceAPYResolver(stub)
	r.SetClock(fixedClock(now))

	bps, ok := r.APYForRebalance(context.Background(), "blend")
	if ok || bps != 0 {
		t.Fatalf("expected hold (0,false), got (%d,%v)", bps, ok)
	}
	if stub.calls != 1 {
		t.Fatalf("expected exactly one on-chain read, got %d", stub.calls)
	}
}

func TestReadingUsableRequiresTimestamp(t *testing.T) {
	r := APYReading{BPS: 500, Confidence: APYProtocolReported}
	if r.Usable(time.Now()) {
		t.Fatal("a reading with no observation time must not be usable")
	}
}

func TestConfidenceFromBPSFlag(t *testing.T) {
	cases := map[uint32]APYConfidence{
		0:   APYProtocolReported,
		1:   APYDerived,
		2:   APYUnavailable,
		99:  APYUnavailable, // unrecognised provenance is not trusted
	}
	for flag, want := range cases {
		if got := ConfidenceFromBPSFlag(flag); got != want {
			t.Errorf("flag %d: got %q, want %q", flag, got, want)
		}
	}
}
