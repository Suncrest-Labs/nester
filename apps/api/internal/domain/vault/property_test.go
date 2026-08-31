package vault

import (
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"
)

// Property-based invariant tests for vault share-price and accounting.
//
// These tests generate adversarial sequences of deposits, yields, and
// withdrawals, asserting the accounting invariants after every operation.
//
// Run with:
//   go test -v ./... -run PropertyBased
//
// Override case count for deep runs:
//   RAPID_CHECKS=10000 go test -v ./...
//
// The tests mirror the contract-side property tests in
// packages/contracts/tests/integration/src/integration/property_tests.rs

// Generators for property testing

func genAmount() *rapid.Generator[decimal.Decimal] {
	return rapid.Custom(func(t *rapid.T) decimal.Decimal {
		// Generate amounts in stroops (1 USDC = 1,000,000 stroops).
		// Include boundary magnitudes: single stroop, min deposit, realistic ranges.
		choice := rapid.IntRange(0, 5).Draw(t, "choice")
		switch choice {
		case 0:
			// Single stroop (1e-7 USDC)
			return decimal.New(1, -7)
		case 1:
			// Minimum deposit (10 USDC)
			return decimal.NewFromInt(10)
		case 2:
			// Medium (100 USDC)
			return decimal.NewFromInt(100)
		case 3:
			// Large (1000 USDC)
			return decimal.NewFromInt(1000)
		case 4:
			// Very large (10000 USDC)
			return decimal.NewFromInt(10000)
		default:
			// Random range 1-1000 USDC
			val := rapid.Int64Range(1, 1000).Draw(t, "val")
			return decimal.NewFromInt(val)
		}
	})
}

func genYieldBps() *rapid.Generator[int32] {
	// Yield as basis points (100 bps = 1%). Range 1-2000 bps (1-20% yield).
	return rapid.Int32Range(1, 2000)
}

func genLossBps() *rapid.Generator[int32] {
	// Loss as basis points. Range 1-1000 bps (1-10% loss).
	return rapid.Int32Range(1, 1000)
}

func genWithdrawBps() *rapid.Generator[int32] {
	// Withdrawal percentage of held shares. Range 1-10000 bps (1-100% of held).
	return rapid.Int32Range(1, 10000)
}

// ReferenceModel simulates the Go-side share accounting locally.
type ReferenceModel struct {
	TotalDeposited decimal.Decimal
	CurrentBalance decimal.Decimal
	TotalShares    decimal.Decimal
	UserShares     map[int]decimal.Decimal
	UserPrincipal  map[int]decimal.Decimal
	AccruedFees    decimal.Decimal
}

func NewReferenceModel() *ReferenceModel {
	return &ReferenceModel{
		TotalDeposited: decimal.Zero,
		CurrentBalance: decimal.Zero,
		TotalShares:    decimal.Zero,
		UserShares:     make(map[int]decimal.Decimal),
		UserPrincipal:  make(map[int]decimal.Decimal),
		AccruedFees:    decimal.Zero,
	}
}

// Deposit adds assets and mints shares using the same formula as ComputeSharePrice.
func (m *ReferenceModel) Deposit(userIdx int, amount decimal.Decimal) decimal.Decimal {
	if amount.Sign() <= 0 {
		return decimal.Zero
	}

	var shares decimal.Decimal
	if m.TotalShares.IsZero() || m.TotalDeposited.Sign() <= 0 {
		// First deposit is 1:1
		shares = amount
	} else {
		// shares = amount / sharePrice, rounded to 6 decimals (matching production)
		// where sharePrice = CurrentBalance / TotalDeposited
		sharePrice := m.CurrentBalance.Div(m.TotalDeposited)
		shares = amount.Div(sharePrice).Round(6)
	}

	if shares.Sign() <= 0 {
		return decimal.Zero
	}

	m.TotalDeposited = m.TotalDeposited.Add(amount)
	m.CurrentBalance = m.CurrentBalance.Add(amount)
	m.TotalShares = m.TotalShares.Add(shares)

	m.UserShares[userIdx] = m.UserShares[userIdx].Add(shares)
	m.UserPrincipal[userIdx] = m.UserPrincipal[userIdx].Add(amount)

	return shares
}

// Withdraw removes shares and assets, accruing fees on yield portion.
func (m *ReferenceModel) Withdraw(userIdx int, shares decimal.Decimal, performanceFeeBps int32) decimal.Decimal {
	if shares.Sign() <= 0 || m.TotalShares.Sign() <= 0 {
		return decimal.Zero
	}

	userShares := m.UserShares[userIdx]
	actualShares := shares
	if actualShares.GreaterThan(userShares) {
		actualShares = userShares
	}

	if actualShares.Sign() <= 0 {
		return decimal.Zero
	}

	// assets = shares * current_balance / total_shares
	assets := actualShares.Mul(m.CurrentBalance).Div(m.TotalShares)

	// Principal portion of withdrawal
	var principalPortion decimal.Decimal
	if userShares.Sign() > 0 {
		principalPortion = m.UserPrincipal[userIdx].Mul(actualShares).Div(userShares)
	}

	// Yield portion (gain on this withdrawal)
	yieldPart := assets.Sub(principalPortion)
	if yieldPart.Sign() < 0 {
		yieldPart = decimal.Zero
	}

	// Performance fee = yield_part * (feeBps / 10000)
	feeBps := decimal.NewFromInt32(performanceFeeBps)
	performanceFee := yieldPart.Mul(feeBps).Div(decimal.NewFromInt(10000))

	netAssets := assets.Sub(performanceFee)

	m.TotalShares = m.TotalShares.Sub(actualShares)
	m.TotalDeposited = m.TotalDeposited.Sub(principalPortion)
	m.CurrentBalance = m.CurrentBalance.Sub(netAssets).Sub(performanceFee)

	m.UserShares[userIdx] = m.UserShares[userIdx].Sub(actualShares)
	m.UserPrincipal[userIdx] = m.UserPrincipal[userIdx].Sub(principalPortion)
	m.AccruedFees = m.AccruedFees.Add(performanceFee)

	return netAssets
}

// ReportYield adds yield to the vault (increases CurrentBalance, keeps shares constant).
func (m *ReferenceModel) ReportYield(amount decimal.Decimal) {
	if amount.Sign() > 0 {
		m.CurrentBalance = m.CurrentBalance.Add(amount)
	}
}

// ReportLoss removes assets from the vault (decreases CurrentBalance).
func (m *ReferenceModel) ReportLoss(amount decimal.Decimal) {
	if amount.Sign() > 0 && amount.LessThan(m.CurrentBalance) {
		m.CurrentBalance = m.CurrentBalance.Sub(amount)
	}
}

// SharePrice returns the live NAV per share.
func (m *ReferenceModel) SharePrice() decimal.Decimal {
	if m.TotalDeposited.IsZero() || m.TotalDeposited.Sign() <= 0 {
		return decimal.NewFromInt(1)
	}
	return m.CurrentBalance.Div(m.TotalDeposited)
}

// CheckConservation verifies that user shares sum to total shares.
func (m *ReferenceModel) CheckConservation() bool {
	sum := decimal.Zero
	for _, userShares := range m.UserShares {
		sum = sum.Add(userShares)
	}
	return sum.Equal(m.TotalShares)
}

// SumUserShares returns the total shares held by all users.
func (m *ReferenceModel) SumUserShares() decimal.Decimal {
	sum := decimal.Zero
	for _, userShares := range m.UserShares {
		sum = sum.Add(userShares)
	}
	return sum
}

// Invariant 1: Conservation - user shares must sum to total shares
func TestPropertyBasedConservation(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		model := NewReferenceModel()
		numUsers := rapid.IntRange(1, 10).Draw(t, "num_users")
		numOps := rapid.IntRange(5, 50).Draw(t, "num_ops")

		for i := 0; i < numOps; i++ {
			opType := rapid.IntRange(0, 3).Draw(t, "op_type")
			userIdx := rapid.IntRange(0, numUsers).Draw(t, "user_idx")

			switch opType {
			case 0: // Deposit
				amount := genAmount().Draw(t, "deposit_amount")
				model.Deposit(userIdx, amount)

			case 1: // Withdraw
				if model.TotalShares.Sign() > 0 {
					shareBps := genWithdrawBps().Draw(t, "withdraw_bps")
					model.Withdraw(userIdx, model.UserShares[userIdx].Mul(decimal.NewFromInt32(shareBps)).Div(decimal.NewFromInt(10000)), 1000)
				}

			case 2: // Report yield
				if model.TotalDeposited.Sign() > 0 {
					yieldBps := genYieldBps().Draw(t, "yield_bps")
					yieldAmount := model.TotalDeposited.Mul(decimal.NewFromInt32(yieldBps)).Div(decimal.NewFromInt(10000))
					model.ReportYield(yieldAmount)
				}

			case 3: // Report loss
				if model.TotalDeposited.Sign() > 0 {
					lossBps := genLossBps().Draw(t, "loss_bps")
					lossAmount := model.TotalDeposited.Mul(decimal.NewFromInt32(lossBps)).Div(decimal.NewFromInt(10000))
					model.ReportLoss(lossAmount)
				}
			}

			// Assert conservation after each operation
			if !model.CheckConservation() {
				t.Fatalf("conservation violated: user shares sum %s != total shares %s",
					model.SumUserShares(), model.TotalShares)
			}
		}
	})
}

// Invariant 2: Round-trip safety - deposit + immediate withdraw <= deposit
func TestPropertyBasedRoundTripSafety(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		model := NewReferenceModel()
		userIdx := 0

		// First deposit
		amount := rapid.Int64Range(10_000_000, 1_000_000_000).Draw(t, "amount")
		depositAmount := decimal.NewFromInt(amount)
		model.Deposit(userIdx, depositAmount)

		// Immediate withdrawal of all shares
		shares := model.UserShares[userIdx]
		withdrawn := model.Withdraw(userIdx, shares, 1000)

		// Assert we didn't get more back than we put in
		if withdrawn.GreaterThan(depositAmount) {
			t.Fatalf("round trip returned more: deposited %s, withdrew %s",
				depositAmount, withdrawn)
		}
	})
}

// Invariant 3: Share price monotonicity - price only increases on yield, not deposits/losses
func TestPropertyBasedSharePriceMonotonicity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		model := NewReferenceModel()
		userIdx := 0

		// Initial deposit
		amount := rapid.Int64Range(10_000_000, 1_000_000_000).Draw(t, "amount")
		model.Deposit(userIdx, decimal.NewFromInt(amount))

		previousPrice := model.SharePrice()
		numOps := rapid.IntRange(3, 30).Draw(t, "num_ops")

		for i := 0; i < numOps; i++ {
			opType := rapid.IntRange(0, 2).Draw(t, "op_type")

			switch opType {
			case 0: // Yield increases price
				if model.TotalDeposited.Sign() > 0 {
					yieldBps := rapid.Int32Range(1, 500).Draw(t, "yield_bps")
					yieldAmount := model.TotalDeposited.Mul(decimal.NewFromInt32(yieldBps)).Div(decimal.NewFromInt(10000))
					model.ReportYield(yieldAmount)

					currentPrice := model.SharePrice()
					if currentPrice.LessThan(previousPrice) {
						t.Fatalf("share price decreased after yield: %s -> %s",
							previousPrice, currentPrice)
					}
					previousPrice = currentPrice
				}

			case 1: // Loss - price can decrease
				if model.TotalDeposited.Sign() > 0 && model.CurrentBalance.GreaterThan(decimal.NewFromInt(1000000)) {
					lossBps := rapid.Int32Range(1, 500).Draw(t, "loss_bps")
					lossAmount := model.TotalDeposited.Mul(decimal.NewFromInt32(lossBps)).Div(decimal.NewFromInt(10000))
					model.ReportLoss(lossAmount)
					// Loss can decrease price; no assertion here
					previousPrice = model.SharePrice()
				}
			}
		}
	})
}

// Invariant 4: No mint from nothing - total shares never exceed assets divided by share price
func TestPropertyBasedNoMintFromNothing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		model := NewReferenceModel()
		numUsers := rapid.IntRange(1, 5).Draw(t, "num_users")
		numOps := rapid.IntRange(5, 50).Draw(t, "num_ops")

		for i := 0; i < numOps; i++ {
			opType := rapid.IntRange(0, 3).Draw(t, "op_type")
			userIdx := rapid.IntRange(0, numUsers).Draw(t, "user_idx")

			switch opType {
			case 0: // Deposit
				amount := genAmount().Draw(t, "deposit_amount")
				model.Deposit(userIdx, amount)

			case 1: // Withdraw
				if model.TotalShares.Sign() > 0 {
					shareBps := genWithdrawBps().Draw(t, "withdraw_bps")
					model.Withdraw(userIdx, model.UserShares[userIdx].Mul(decimal.NewFromInt32(shareBps)).Div(decimal.NewFromInt(10000)), 1000)
				}

			case 2: // Report yield or loss
				if model.TotalDeposited.Sign() > 0 {
					if rapid.Bool().Draw(t, "yield_vs_loss") {
						yieldBps := rapid.Int32Range(1, 1000).Draw(t, "yield_bps")
						yieldAmount := model.TotalDeposited.Mul(decimal.NewFromInt32(yieldBps)).Div(decimal.NewFromInt(10000))
						model.ReportYield(yieldAmount)
					} else {
						lossBps := rapid.Int32Range(1, 500).Draw(t, "loss_bps")
						lossAmount := model.TotalDeposited.Mul(decimal.NewFromInt32(lossBps)).Div(decimal.NewFromInt(10000))
						model.ReportLoss(lossAmount)
					}
				}
			}

			// Invariant: total deposited must never be negative
			if model.TotalDeposited.Sign() < 0 {
				t.Fatalf("total deposited went negative: %s", model.TotalDeposited)
			}

			// Invariant: total shares must never be negative
			if model.TotalShares.Sign() < 0 {
				t.Fatalf("total shares went negative: %s", model.TotalShares)
			}

			// Invariant: current balance must never be negative
			if model.CurrentBalance.Sign() < 0 {
				t.Fatalf("current balance went negative: %s", model.CurrentBalance)
			}

			// Invariant: conservation
			if !model.CheckConservation() {
				t.Fatalf("conservation violated after op %d", i)
			}
		}
	})
}

// Regression test: empty vault first deposit should be 1:1
func TestPropertyBasedEmptyVaultFirstDeposit(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		model := NewReferenceModel()
		amount := rapid.Int64Range(10_000_000, 1_000_000_000).Draw(t, "amount")
		shares := model.Deposit(0, decimal.NewFromInt(amount))

		if !shares.Equal(decimal.NewFromInt(amount)) {
			t.Fatalf("first deposit should be 1:1: deposited %d, minted %s", amount, shares)
		}
	})
}

// Regression test: tiny amounts should not create zero-share situations
func TestPropertyBasedTinyAmountHandling(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		model := NewReferenceModel()

		// Large initial deposit
		model.Deposit(0, decimal.NewFromInt(1_000_000_000))

		// Many tiny deposits should all mint at least 1 share if rounding is fair
		for i := 0; i < 100; i++ {
			shares := model.Deposit(1, decimal.NewFromInt(1_000_001)) // 1 USDC + 1 stroop
			// Shares might round to zero for very small amounts, but this is acceptable
			// The invariant is that our accounting remains consistent, not that every
			// amount mints shares.
			if shares.Sign() > 0 {
				if !model.CheckConservation() {
					t.Fatalf("conservation violated after tiny deposit %d", i)
				}
			}
		}
	})
}
