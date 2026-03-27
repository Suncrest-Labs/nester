package offramp

import (
	"fmt"
	"testing"
)

func TestParseStatus_ValidStatuses(t *testing.T) {
	for _, raw := range []string{
		string(StatusInitiated),
		string(StatusLiquidityMatched),
		string(StatusFiatDispatched),
		string(StatusConfirmed),
		string(StatusFailed),
	} {
		t.Run(raw, func(t *testing.T) {
			s, err := ParseStatus(raw)
			if err != nil {
				t.Fatalf("ParseStatus(%q) error = %v", raw, err)
			}
			if s != SettlementStatus(raw) {
				t.Fatalf("got %q, want %q", s, raw)
			}
		})
	}
}

func TestParseStatus_Invalid(t *testing.T) {
	for _, raw := range []string{"", "pending", "settled", "refunded", "nonsense"} {
		name := raw
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			_, err := ParseStatus(raw)
			if err != ErrInvalidStatus {
				t.Fatalf("ParseStatus(%q) want ErrInvalidStatus, got %v", raw, err)
			}
		})
	}
}

func TestSettlement_CanTransitionTo_AllValidEdges(t *testing.T) {
	cases := []struct {
		from SettlementStatus
		to   SettlementStatus
	}{
		{StatusInitiated, StatusLiquidityMatched},
		{StatusInitiated, StatusFailed},
		{StatusLiquidityMatched, StatusFiatDispatched},
		{StatusLiquidityMatched, StatusFailed},
		{StatusFiatDispatched, StatusConfirmed},
		{StatusFiatDispatched, StatusFailed},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_to_%s", c.from, c.to), func(t *testing.T) {
			s := Settlement{Status: c.from}
			if !s.CanTransitionTo(c.to) {
				t.Fatalf("expected valid transition %q -> %q", c.from, c.to)
			}
		})
	}
}

func TestSettlement_CanTransitionTo_InvalidTransitionsRejected(t *testing.T) {
	cases := []struct {
		from SettlementStatus
		to   SettlementStatus
	}{
		{StatusInitiated, StatusInitiated},
		{StatusInitiated, StatusFiatDispatched},
		{StatusInitiated, StatusConfirmed},
		{StatusLiquidityMatched, StatusInitiated},
		{StatusLiquidityMatched, StatusConfirmed},
		{StatusFiatDispatched, StatusInitiated},
		{StatusFiatDispatched, StatusLiquidityMatched},
		{StatusConfirmed, StatusInitiated},
		{StatusConfirmed, StatusFailed},
		{StatusFailed, StatusInitiated},
		{StatusFailed, StatusConfirmed},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("%s_to_%s", c.from, c.to), func(t *testing.T) {
			s := Settlement{Status: c.from}
			if s.CanTransitionTo(c.to) {
				t.Fatalf("expected invalid transition %q -> %q", c.from, c.to)
			}
		})
	}
}
