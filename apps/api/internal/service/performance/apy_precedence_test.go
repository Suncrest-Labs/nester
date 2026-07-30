package performance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/vault"
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

func TestAPYForRebalance(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		reading APYReading
		err     error
		wantBPS uint32
		wantOK  bool
	}{
		{
			name:    "fresh protocol-reported rate is authoritative",
			reading: APYReading{BPS: 640, Confidence: APYProtocolReported, ObservedAt: now.Add(-time.Hour)},
			wantBPS: 640,
			wantOK:  true,
		},
		{
			name:    "fresh derived rate is usable",
			reading: APYReading{BPS: 310, Confidence: APYDerived, ObservedAt: now.Add(-2 * time.Hour)},
			wantBPS: 310,
			wantOK:  true,
		},
		{
			// The whole point of the confidence flag: a zero BPS carried by an
			// Unavailable reading is "unknown", never a real rate of zero.
			name:    "unavailable reading is not a zero rate",
			reading: APYReading{BPS: 0, Confidence: APYUnavailable, ObservedAt: now},
			wantOK:  false,
		},
		{
			name:    "stale reading is rejected",
			reading: APYReading{BPS: 800, Confidence: APYDerived, ObservedAt: now.Add(-MaxOnChainAPYAge - time.Minute)},
			wantOK:  false,
		},
		{
			name:    "reading exactly at the age bound is still usable",
			reading: APYReading{BPS: 500, Confidence: APYDerived, ObservedAt: now.Add(-MaxOnChainAPYAge)},
			wantBPS: 500,
			wantOK:  true,
		},
		{
			// A future timestamp is a clock or ledger fault, not fresh data.
			name:    "future timestamp is rejected",
			reading: APYReading{BPS: 900, Confidence: APYProtocolReported, ObservedAt: now.Add(time.Hour)},
			wantOK:  false,
		},
		{
			name:    "missing observation time is rejected",
			reading: APYReading{BPS: 500, Confidence: APYProtocolReported},
			wantOK:  false,
		},
		{
			name:    "unrecognised confidence is rejected",
			reading: APYReading{BPS: 700, Confidence: APYConfidence("something-else"), ObservedAt: now},
			wantOK:  false,
		},
		{
			name:   "read failure yields hold, not a fallback",
			err:    errors.New("rpc down"),
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubOnChainReader{reading: tc.reading, err: tc.err}
			r := NewRebalanceAPYResolver(stub)
			r.SetClock(fixedClock(now))

			bps, ok := r.APYForRebalance(context.Background(), "blend")
			if ok != tc.wantOK {
				t.Fatalf("usable = %v, want %v", ok, tc.wantOK)
			}
			if bps != tc.wantBPS {
				t.Fatalf("bps = %d, want %d", bps, tc.wantBPS)
			}
			// There is deliberately no DeFiLlama fallback: exactly one
			// on-chain read, and no second source consulted when it fails.
			if stub.calls != 1 {
				t.Fatalf("on-chain reads = %d, want exactly 1", stub.calls)
			}
		})
	}
}

func TestAPYForRebalanceWithoutReader(t *testing.T) {
	r := NewRebalanceAPYResolver(nil)
	if _, ok := r.APYForRebalance(context.Background(), "blend"); ok {
		t.Fatal("a resolver with no reader must never report a usable APY")
	}
}

func TestConfidenceFromBPSFlag(t *testing.T) {
	cases := []struct {
		name string
		flag uint32
		want APYConfidence
	}{
		{"protocol reported", 0, APYProtocolReported},
		{"derived", 1, APYDerived},
		{"unavailable", 2, APYUnavailable},
		{"unrecognised provenance is not trusted", 99, APYUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConfidenceFromBPSFlag(tc.flag); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Weighting must exclude allocations with no usable on-chain rate rather than
// failing the refresh or averaging them in as zero.
func TestWeightedVaultAPYSkippingUnknown(t *testing.T) {
	alloc := func(protocol string, amount int64) vault.Allocation {
		return vault.Allocation{Protocol: protocol, Amount: decimal.NewFromInt(amount)}
	}

	cases := []struct {
		name          string
		allocations   []vault.Allocation
		apy           map[string]uint32
		skipUnknown   bool
		wantBPS       uint32
		wantBreakdown int
		wantErr       bool
	}{
		{
			name:          "all rates known",
			allocations:   []vault.Allocation{alloc("blend", 100), alloc("soroswap", 100)},
			apy:           map[string]uint32{"blend": 400, "soroswap": 600},
			skipUnknown:   true,
			wantBPS:       500,
			wantBreakdown: 2,
		},
		{
			// The unknown source must not drag the average toward zero.
			name:          "mixed known and unknown excludes the unknown",
			allocations:   []vault.Allocation{alloc("blend", 100), alloc("soroswap", 100)},
			apy:           map[string]uint32{"blend": 400},
			skipUnknown:   true,
			wantBPS:       400,
			wantBreakdown: 1,
		},
		{
			name:          "all unknown yields zero weight and empty breakdown",
			allocations:   []vault.Allocation{alloc("blend", 100), alloc("soroswap", 100)},
			apy:           map[string]uint32{},
			skipUnknown:   true,
			wantBPS:       0,
			wantBreakdown: 0,
		},
		{
			// Legacy (non-resolver) mode keeps treating a gap as a data error.
			name:        "missing rate is an error when not skipping",
			allocations: []vault.Allocation{alloc("blend", 100)},
			apy:         map[string]uint32{},
			skipUnknown: false,
			wantErr:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := vault.Vault{ID: uuid.New(), Allocations: tc.allocations}
			bps, breakdown, err := weightedVaultAPYSkippingUnknown(v, tc.apy, tc.skipUnknown)

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error for a missing rate")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bps != tc.wantBPS {
				t.Fatalf("bps = %d, want %d", bps, tc.wantBPS)
			}
			if len(breakdown) != tc.wantBreakdown {
				t.Fatalf("breakdown entries = %d, want %d", len(breakdown), tc.wantBreakdown)
			}
		})
	}
}
