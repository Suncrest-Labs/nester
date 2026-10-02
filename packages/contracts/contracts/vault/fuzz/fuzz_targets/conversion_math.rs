#![no_main]

use arbitrary::Arbitrary;
use ethnum::I256;
use libfuzzer_sys::fuzz_target;
use vault_contract::conversion::{
    assets_to_shares_down, assets_to_shares_up, mul_div_down, mul_div_up, shares_to_assets_down,
    shares_to_assets_up,
};

// The four numeric inputs this target explores. `Arbitrary` derives byte
// decoding from the fuzzer's raw corpus, so libFuzzer can mutate each field
// independently instead of us having to hand-roll input parsing.
#[derive(Debug, Arbitrary)]
struct Input {
    a: i128,
    b: i128,
    c: i128,
}

// Widened, unchecked cross-check for `mul_div_*`: the exact rational value
// `a * b / c`, computed in ethnum's I256 so the product can never itself
// overflow regardless of what the checked i128 implementation returns (an
// i128 * i128 product needs up to 256 bits). Any Ok result the fuzz target
// produces is compared against this, so a checked-arithmetic bug that
// silently returns a wrong (but non-overflowing) i128 is still caught, not
// just a bug that panics or overflows outright.
fn exact_mul_div(a: i128, b: i128, c: i128) -> (i128, i128) {
    let product = I256::from(a) * I256::from(b);
    let denom = I256::from(c);
    let quotient = product / denom;
    let remainder = product % denom;
    (
        quotient
            .try_into()
            .expect("quotient must fit back in i128 for valid mul_div inputs"),
        remainder
            .try_into()
            .expect("remainder must fit back in i128 for valid mul_div inputs"),
    )
}

fuzz_target!(|input: Input| {
    let Input { a, b, c } = input;

    // mul_div_down / mul_div_up: the primitives every conversion function
    // is built on. Only exercise valid preconditions (non-negative operands,
    // positive denominator) -- the invalid-input branches are already
    // covered by conversion.rs's own unit tests, and this target's job is
    // the arithmetic itself, not input validation.
    if a >= 0 && b >= 0 && c > 0 {
        if let Ok(down) = mul_div_down(a, b, c) {
            let (exact_q, _exact_r) = exact_mul_div(a, b, c);
            assert_eq!(
                down, exact_q,
                "mul_div_down({a}, {b}, {c}) = {down}, exact floor = {exact_q}"
            );
        }
        if let Ok(up) = mul_div_up(a, b, c) {
            let (exact_q, exact_r) = exact_mul_div(a, b, c);
            let expected = if exact_r == 0 { exact_q } else { exact_q + 1 };
            assert_eq!(
                up, expected,
                "mul_div_up({a}, {b}, {c}) = {up}, exact ceil = {expected}"
            );
        }
        // up must never round below down for the same inputs.
        if let (Ok(down), Ok(up)) = (mul_div_down(a, b, c), mul_div_up(a, b, c)) {
            assert!(
                up >= down,
                "mul_div_up({a},{b},{c})={up} rounded below mul_div_down={down}"
            );
        }
    }

    // assets_to_shares_* / shares_to_assets_*: the share-price conversions
    // actually reachable from deposit()/withdraw(). `a` is the
    // amount, `b` is total_assets, `c` is total_shares.
    let amount = a;
    let total_assets = b;
    let total_shares = c;

    if let Ok(down) = assets_to_shares_down(amount, total_assets, total_shares) {
        assert!(
            down >= 0,
            "assets_to_shares_down produced a negative result"
        );
        if let Ok(up) = assets_to_shares_up(amount, total_assets, total_shares) {
            assert!(
                up >= down,
                "assets_to_shares_up({amount},{total_assets},{total_shares})={up} < \
                 assets_to_shares_down={down}"
            );
        }
    }

    if let Ok(down) = shares_to_assets_down(amount, total_assets, total_shares) {
        assert!(
            down >= 0,
            "shares_to_assets_down produced a negative result"
        );
        if let Ok(up) = shares_to_assets_up(amount, total_assets, total_shares) {
            assert!(
                up >= down,
                "shares_to_assets_up({amount},{total_assets},{total_shares})={up} < \
                 shares_to_assets_down={down}"
            );
        }
    }

    // Round-trip: converting assets to shares and back must never manufacture
    // value. This mirrors the vault's own proptest property 1a, but over the
    // fuzzer's unconstrained input space (including values well outside the
    // proptest ranges' 1..1_000_000_000 * XLM bounds) rather than a bounded
    // random sample.
    if total_shares > 0 && total_assets > 0 && amount >= 0 {
        if let Ok(shares) = assets_to_shares_down(amount, total_assets, total_shares) {
            if let (Some(new_total_assets), Some(new_total_shares)) = (
                total_assets.checked_add(amount),
                total_shares.checked_add(shares),
            ) {
                if let Ok(assets_back) =
                    shares_to_assets_down(shares, new_total_assets, new_total_shares)
                {
                    assert!(
                        assets_back <= amount,
                        "round-trip minted value: deposited {amount}, got back {assets_back} \
                         (total_assets={total_assets}, total_shares={total_shares})"
                    );
                }
            }
        }
    }
});
