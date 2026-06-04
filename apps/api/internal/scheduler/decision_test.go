package scheduler

import (
	"testing"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestDecide_NoYields(t *testing.T) {
	res := Decide(DecisionInput{MinAPYGainBPS: 50})
	if res.Rebalance {
		t.Errorf("expected no rebalance with no yields, got Rebalance=true")
	}
	if res.Reason != "no_yields" {
		t.Errorf("expected reason 'no_yields', got %q", res.Reason)
	}
}

func TestDecide_NoCurrentAllocation_RebalanceIntoBest(t *testing.T) {
	res := Decide(DecisionInput{
		Yields: []ProtocolYield{
			{Protocol: "blend", APY: d("0.05")},
			{Protocol: "aave", APY: d("0.07")},
		},
		MinAPYGainBPS: 50,
	})
	if !res.Rebalance {
		t.Errorf("expected rebalance when no current allocation and best yield > threshold")
	}
	if res.OptimalProtocol != "aave" {
		t.Errorf("expected aave, got %s", res.OptimalProtocol)
	}
	if res.ExpectedGainBPS != 700 {
		t.Errorf("expected 700 BPS gain (7%%), got %d", res.ExpectedGainBPS)
	}
	if res.Reason != "no_current_allocation" {
		t.Errorf("expected reason 'no_current_allocation', got %q", res.Reason)
	}
}

func TestDecide_NoCurrentAllocation_BelowThresholdIsNoOp(t *testing.T) {
	res := Decide(DecisionInput{
		Yields:        []ProtocolYield{{Protocol: "aave", APY: d("0.001")}}, // 10 BPS
		MinAPYGainBPS: 50,
	})
	if res.Rebalance {
		t.Errorf("expected no rebalance — 10 BPS < 50 BPS threshold")
	}
}

func TestDecide_AlreadyOptimal(t *testing.T) {
	res := Decide(DecisionInput{
		CurrentAllocations: []CurrentAllocation{
			{Protocol: "aave", Amount: d("1000")},
			{Protocol: "blend", Amount: d("100")},
		},
		Yields: []ProtocolYield{
			{Protocol: "aave", APY: d("0.07")},
			{Protocol: "blend", APY: d("0.05")},
		},
		MinAPYGainBPS: 50,
	})
	if res.Rebalance {
		t.Errorf("expected no rebalance when current top already matches optimal")
	}
	if res.Reason != "already_optimal" {
		t.Errorf("expected reason 'already_optimal', got %q", res.Reason)
	}
	if res.CurrentTopProtocol != "aave" {
		t.Errorf("expected current top 'aave', got %q", res.CurrentTopProtocol)
	}
}

func TestDecide_BelowThreshold(t *testing.T) {
	// Whole vault is in blend (5%). Best is aave at 5.4% → 40 BPS gain
	// — below the 50 BPS threshold, so we don't churn.
	res := Decide(DecisionInput{
		CurrentAllocations: []CurrentAllocation{
			{Protocol: "blend", Amount: d("1000")},
		},
		Yields: []ProtocolYield{
			{Protocol: "aave", APY: d("0.054")},
			{Protocol: "blend", APY: d("0.05")},
		},
		MinAPYGainBPS: 50,
	})
	if res.Rebalance {
		t.Errorf("expected no rebalance — 40 BPS < 50 BPS threshold")
	}
	if res.ExpectedGainBPS != 40 {
		t.Errorf("expected 40 BPS gain, got %d", res.ExpectedGainBPS)
	}
	if res.Reason != "below_threshold" {
		t.Errorf("expected reason 'below_threshold', got %q", res.Reason)
	}
}

func TestDecide_ClearWinRecommendsRebalance(t *testing.T) {
	// Whole vault is in blend (5%). Best is aave at 7% → 200 BPS gain
	// — well above the 50 BPS threshold.
	res := Decide(DecisionInput{
		CurrentAllocations: []CurrentAllocation{
			{Protocol: "blend", Amount: d("1000")},
		},
		Yields: []ProtocolYield{
			{Protocol: "blend", APY: d("0.05")},
			{Protocol: "aave", APY: d("0.07")},
		},
		MinAPYGainBPS: 50,
	})
	if !res.Rebalance {
		t.Errorf("expected rebalance — 200 BPS gain >> 50 BPS threshold")
	}
	if res.OptimalProtocol != "aave" {
		t.Errorf("expected aave, got %q", res.OptimalProtocol)
	}
	if res.CurrentTopProtocol != "blend" {
		t.Errorf("expected blend, got %q", res.CurrentTopProtocol)
	}
	if res.ExpectedGainBPS != 200 {
		t.Errorf("expected 200 BPS, got %d", res.ExpectedGainBPS)
	}
	if res.Reason != "rebalance" {
		t.Errorf("expected reason 'rebalance', got %q", res.Reason)
	}
}

func TestDecide_WeightedAPYIsAccurate(t *testing.T) {
	// 60% in blend (5%), 40% in aave (7%) → weighted 5.8%.
	// Best yield is compound at 6.5% → gain = 6.5% - 5.8% = 70 BPS.
	res := Decide(DecisionInput{
		CurrentAllocations: []CurrentAllocation{
			{Protocol: "blend", Amount: d("600")},
			{Protocol: "aave", Amount: d("400")},
		},
		Yields: []ProtocolYield{
			{Protocol: "blend", APY: d("0.05")},
			{Protocol: "aave", APY: d("0.07")},
			{Protocol: "compound", APY: d("0.065")},
		},
		MinAPYGainBPS: 50,
	})
	if res.CurrentWeightedAPY.String() != "0.058" {
		t.Errorf("expected weighted APY 0.058, got %s", res.CurrentWeightedAPY)
	}
	if res.OptimalAPY.String() != "0.07" {
		t.Errorf("expected optimal APY 0.07, got %s", res.OptimalAPY)
	}
	// Optimal is aave (7%) — not the same as the current top (blend at 600
	// is more than aave at 400). Gain = 7% - 5.8% = 120 BPS.
	if res.ExpectedGainBPS != 120 {
		t.Errorf("expected 120 BPS gain, got %d", res.ExpectedGainBPS)
	}
	if !res.Rebalance {
		t.Errorf("expected rebalance — 120 BPS > 50 BPS threshold")
	}
}

func TestDecide_ZeroTotalAmount_TreatedAsNoAllocation(t *testing.T) {
	res := Decide(DecisionInput{
		CurrentAllocations: []CurrentAllocation{
			{Protocol: "blend", Amount: d("0")},
		},
		Yields:        []ProtocolYield{{Protocol: "aave", APY: d("0.07")}},
		MinAPYGainBPS: 50,
	})
	if !res.Rebalance {
		t.Errorf("expected rebalance — zero total allocation should behave like 'no_current_allocation'")
	}
	if res.Reason != "no_current_allocation" {
		t.Errorf("expected reason 'no_current_allocation', got %q", res.Reason)
	}
}

func TestDecide_UnknownProtocolAllocationContributesZeroAPY(t *testing.T) {
	// We hold half our position in a protocol we no longer have a yield
	// quote for. That half contributes 0 APY, dragging the weighted
	// average down — which should make a moderate switch worthwhile.
	res := Decide(DecisionInput{
		CurrentAllocations: []CurrentAllocation{
			{Protocol: "unknown", Amount: d("500")},
			{Protocol: "blend", Amount: d("500")},
		},
		Yields: []ProtocolYield{
			{Protocol: "blend", APY: d("0.05")},
			{Protocol: "aave", APY: d("0.06")},
		},
		MinAPYGainBPS: 50,
	})
	if !res.Rebalance {
		t.Errorf("expected rebalance — unknown protocol contributes 0 APY, gain should clear threshold")
	}
}
