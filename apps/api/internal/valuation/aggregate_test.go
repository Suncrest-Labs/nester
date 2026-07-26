package valuation

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/suncrestlabs/nester/apps/api/internal/domain/portfolio"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func usdcHigh() PriceTable {
	return PriceTable{"USDC": {Rate: dec("1"), Confidence: portfolio.ConfidenceHigh}}
}

func TestAggregate_SingleVaultBreakdown(t *testing.T) {
	vid := uuid.New()
	in := Inputs{
		UserID: uuid.New(),
		Now:    time.Now(),
		Positions: []Position{
			{VaultID: vid, Asset: "USDC", Principal: dec("100"), Yield: dec("5"), Locked: dec("20")},
		},
	}
	val, err := Aggregate(in, usdcHigh())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !val.TotalValueUSDC.Equal(dec("105")) {
		t.Fatalf("total = %s, want 105", val.TotalValueUSDC)
	}
	if !val.PrincipalUSDC.Equal(dec("100")) || !val.YieldUSDC.Equal(dec("5")) {
		t.Fatalf("principal/yield = %s/%s, want 100/5", val.PrincipalUSDC, val.YieldUSDC)
	}
	if !val.LockedUSDC.Equal(dec("20")) || !val.FlexibleUSDC.Equal(dec("85")) {
		t.Fatalf("locked/flexible = %s/%s, want 20/85", val.LockedUSDC, val.FlexibleUSDC)
	}
	if !val.SettledUSDC.Equal(val.TotalValueUSDC) {
		t.Fatalf("settled %s should equal total %s", val.SettledUSDC, val.TotalValueUSDC)
	}
	if val.Confidence != portfolio.ConfidenceHigh {
		t.Fatalf("confidence = %s, want high", val.Confidence)
	}
}

// Identities that must hold to the stroop: total == principal+yield ==
// flexible+locked, and the sum of per-vault values equals the totals.
func TestAggregate_StroopExactIdentities(t *testing.T) {
	in := Inputs{
		UserID: uuid.New(),
		Now:    time.Now(),
		Positions: []Position{
			{VaultID: uuid.New(), Asset: "USDC", Principal: dec("33.3333333"), Yield: dec("0.6666667"), Locked: dec("10.1234567")},
			{VaultID: uuid.New(), Asset: "USDC", Principal: dec("0.0000001"), Yield: dec("0.0000002"), Locked: dec("0")},
		},
	}
	val, err := Aggregate(in, usdcHigh())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !val.TotalValueUSDC.Equal(val.PrincipalUSDC.Add(val.YieldUSDC)) {
		t.Fatalf("total %s != principal+yield %s", val.TotalValueUSDC, val.PrincipalUSDC.Add(val.YieldUSDC))
	}
	if !val.TotalValueUSDC.Equal(val.FlexibleUSDC.Add(val.LockedUSDC)) {
		t.Fatalf("total %s != flexible+locked %s", val.TotalValueUSDC, val.FlexibleUSDC.Add(val.LockedUSDC))
	}
	sum := decimal.Zero
	for _, v := range val.Vaults {
		sum = sum.Add(v.CurrentValueUSDC)
		if !v.CurrentValueUSDC.Equal(v.FlexibleUSDC.Add(v.LockedUSDC)) {
			t.Fatalf("vault current %s != flexible+locked %s", v.CurrentValueUSDC, v.FlexibleUSDC.Add(v.LockedUSDC))
		}
	}
	if !sum.Equal(val.TotalValueUSDC) {
		t.Fatalf("sum of vault values %s != total %s", sum, val.TotalValueUSDC)
	}
	// Every USDC figure must carry no more than 7 decimal places.
	if val.TotalValueUSDC.Exponent() < -int32(portfolio.USDCScale) {
		t.Fatalf("total has sub-stroop precision: %s", val.TotalValueUSDC)
	}
}

func TestAggregate_PendingExcludedFromTotal(t *testing.T) {
	in := Inputs{
		UserID:          uuid.New(),
		Now:             time.Now(),
		Positions:       []Position{{VaultID: uuid.New(), Asset: "USDC", Principal: dec("100"), Yield: dec("0")}},
		PendingDeposits: []AssetAmount{{Asset: "USDC", Amount: dec("50")}},
	}
	val, err := Aggregate(in, usdcHigh())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !val.TotalValueUSDC.Equal(dec("100")) {
		t.Fatalf("total = %s, want 100 (pending excluded)", val.TotalValueUSDC)
	}
	if !val.PendingDepositsUSDC.Equal(dec("50")) {
		t.Fatalf("pending = %s, want 50", val.PendingDepositsUSDC)
	}
}

func TestAggregate_MultiAssetConfidencePropagation(t *testing.T) {
	prices := PriceTable{
		"USDC": {Rate: dec("1"), Confidence: portfolio.ConfidenceHigh},
		"XLM":  {Rate: dec("0.10"), Confidence: portfolio.ConfidenceMedium},
	}
	in := Inputs{
		UserID: uuid.New(),
		Now:    time.Now(),
		Positions: []Position{
			{VaultID: uuid.New(), Asset: "USDC", Principal: dec("100"), Yield: dec("0")},
			{VaultID: uuid.New(), Asset: "XLM", Principal: dec("1000"), Yield: dec("0")}, // 1000 * 0.10 = 100
		},
	}
	val, err := Aggregate(in, prices)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !val.TotalValueUSDC.Equal(dec("200")) {
		t.Fatalf("total = %s, want 200", val.TotalValueUSDC)
	}
	if val.Confidence != portfolio.ConfidenceMedium {
		t.Fatalf("confidence = %s, want medium (worst of high/medium)", val.Confidence)
	}
}

func TestAggregate_MissingPriceIsError(t *testing.T) {
	in := Inputs{
		UserID:    uuid.New(),
		Now:       time.Now(),
		Positions: []Position{{VaultID: uuid.New(), Asset: "BTC", Principal: dec("1"), Yield: dec("0")}},
	}
	if _, err := Aggregate(in, usdcHigh()); err == nil {
		t.Fatal("expected error for unpriced asset")
	}
}

func TestAggregate_GoalProgress(t *testing.T) {
	in := Inputs{
		UserID: uuid.New(),
		Now:    time.Now(),
		Goals: []GoalInput{
			{GoalID: uuid.New(), Name: "Car", Asset: "USDC", Allocated: dec("250"), Target: dec("1000")},
			{GoalID: uuid.New(), Name: "Done", Asset: "USDC", Allocated: dec("1500"), Target: dec("1000")}, // clamps to 10000
		},
	}
	val, err := Aggregate(in, usdcHigh())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if val.Goals[0].ProgressBps != 2500 {
		t.Fatalf("goal 0 progress = %d bps, want 2500", val.Goals[0].ProgressBps)
	}
	if val.Goals[1].ProgressBps != 10000 {
		t.Fatalf("goal 1 progress = %d bps, want 10000 (clamped)", val.Goals[1].ProgressBps)
	}
}

func TestAggregate_EmptyPortfolioIsHighConfidence(t *testing.T) {
	val, err := Aggregate(Inputs{UserID: uuid.New(), Now: time.Now()}, usdcHigh())
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if !val.TotalValueUSDC.IsZero() || val.Confidence != portfolio.ConfidenceHigh {
		t.Fatalf("empty portfolio total=%s confidence=%s, want 0/high", val.TotalValueUSDC, val.Confidence)
	}
}
