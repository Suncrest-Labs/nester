package stellar

import "github.com/shopspring/decimal"

// StroopsPerUnit is the fixed-point scale Stellar and Soroban use for asset
// amounts: one asset unit is 10^7 stroops. Soroban contract events carry
// stroops, while the vault ledger (vaults.total_deposited,
// vaults.current_balance, vault_transactions.amount) stores plain asset units.
//
// Every value crossing that boundary must be converted exactly once. Writing a
// raw stroop amount into a balance column inflates it by 1e7 (nester#1146).
const StroopsPerUnit int64 = 10_000_000

// StroopsToAssetUnits converts an on-chain stroop amount into the asset units
// the vault ledger stores.
//
// This is the single conversion helper for the chain -> ledger direction: the
// event indexer and the transaction chain-event verifier both route through it
// so the two write paths cannot drift apart in scale. Do not inline the
// division at a call site.
//
// The division is exact — decimal.Decimal is arbitrary precision, so no
// rounding is applied and sub-unit stroop remainders are preserved rather than
// silently truncated.
func StroopsToAssetUnits(stroops decimal.Decimal) decimal.Decimal {
	return stroops.Div(decimal.NewFromInt(StroopsPerUnit))
}

// AssetUnitsToStroops converts an asset-unit amount into the stroops a Soroban
// contract call expects. It is the exact inverse of StroopsToAssetUnits and is
// rounded to a whole stroop, the smallest representable on-chain quantity.
func AssetUnitsToStroops(units decimal.Decimal) decimal.Decimal {
	return units.Mul(decimal.NewFromInt(StroopsPerUnit)).Round(0)
}
