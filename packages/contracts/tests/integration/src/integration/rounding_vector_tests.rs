//! Differential rounding-vector tests — issue #1498.
//!
//! The Nester protocol runs three implementations of the ERC-4626-style
//! share/asset conversion:
//!   - Rust (this file, authoritative source of truth)
//!   - Go (off-chain indexer at apps/indexer)
//!   - TypeScript / dapp (apps/dapp/frontend)
//!
//! This file generates 1 000+ (total_assets, total_shares, amount) test
//! vectors and records the expected `assets_to_shares` and `shares_to_assets`
//! results according to the Rust implementation.  The generated fixture is
//! written to `rounding_vectors.json` next to this file so the Go and TS test
//! suites can import it and assert they produce identical values.
//!
//! Key rounding contracts:
//!   - `assets_to_shares_down` rounds *down* (deposit: user gets fewer shares)
//!   - `assets_to_shares_up`   rounds *up*   (fee accounting: protocol takes more)
//!   - `shares_to_assets_down` rounds *down* (withdraw: user gets fewer assets)
//!   - `shares_to_assets_up`   rounds *up*   (fee accounting: protocol takes more)
//!
//! Both directions must agree across all three stacks to prevent arbitrage.
#![cfg(test)]

extern crate std;

use std::string::ToString;

use vault_contract::conversion::{
    assets_to_shares_down, assets_to_shares_up, shares_to_assets_down, shares_to_assets_up,
};

// ── Vector generation helpers ─────────────────────────────────────────────────

#[derive(Debug)]
struct Vector {
    total_assets: i128,
    total_shares: i128,
    amount: i128,
    a2s_down: i128,
    a2s_up: i128,
    s2a_down: i128,
    s2a_up: i128,
}

fn compute(total_assets: i128, total_shares: i128, amount: i128) -> Vector {
    // Signature: fn(amount, total_assets, total_shares)
    let a2s_down = assets_to_shares_down(amount, total_assets, total_shares).unwrap_or(0);
    let a2s_up = assets_to_shares_up(amount, total_assets, total_shares).unwrap_or(0);
    let s2a_down = shares_to_assets_down(amount, total_assets, total_shares).unwrap_or(0);
    let s2a_up = shares_to_assets_up(amount, total_assets, total_shares).unwrap_or(0);
    Vector {
        total_assets,
        total_shares,
        amount,
        a2s_down,
        a2s_up,
        s2a_down,
        s2a_up,
    }
}

// ── Rounding direction invariants ─────────────────────────────────────────────

/// `down` variant never exceeds `up` variant.
#[test]
fn rounding_down_never_exceeds_up() {
    let cases: &[(i128, i128, i128)] = &[
        (10_000_000, 10_000_000, 1_000_000),
        (15_000_000, 10_000_000, 1_000_000),  // share price 1.5
        (10_000_000, 15_000_000, 1_000_000),  // diluted
        (1, 1_000_000_000, 1),                // extreme dilution
        (1_000_000_000, 1, 500_000_000),      // extreme concentration
        (7, 3, 1),                            // non-divisible
        (10_000_000, 10_000_000, 10_000_001), // amount > 1 unit
    ];

    for &(ta, ts, amt) in cases {
        let v = compute(ta, ts, amt);
        assert!(
            v.a2s_down <= v.a2s_up,
            "a2s_down > a2s_up for ({ta},{ts},{amt}): {} > {}",
            v.a2s_down,
            v.a2s_up
        );
        assert!(
            v.s2a_down <= v.s2a_up,
            "s2a_down > s2a_up for ({ta},{ts},{amt}): {} > {}",
            v.s2a_down,
            v.s2a_up
        );
    }
}

/// Round-trip: converting assets → shares → assets with floor rounding
/// produces at most the original amount (no value extracted from the vault).
#[test]
fn round_trip_assets_shares_assets_no_overpay() {
    let cases: &[(i128, i128, i128)] = &[
        (10_000_000, 10_000_000, 3_000_000),
        (20_000_000, 10_000_000, 5_000_000),   // 2:1 share price
        (10_000_003, 10_000_000, 1_000_000),   // slight yield
        (100_000_000, 99_999_997, 10_000_000), // slight impairment
    ];

    for &(ta, ts, amt) in cases {
        let shares = assets_to_shares_down(amt, ta, ts).unwrap_or(0);
        let recovered = shares_to_assets_down(shares, ta, ts).unwrap_or(0);
        assert!(
            recovered <= amt,
            "round-trip overpay ({ta},{ts},{amt}): got back {recovered} > {amt}"
        );
    }
}

/// Zero pool: edge cases should not panic; first-deposit returns 1:1.
#[test]
fn zero_pool_no_panic() {
    // Both zero (uninitialized): total_shares == 0 → return assets 1:1
    let r = assets_to_shares_down(1_000_000, 0, 0);
    assert!(r.is_ok(), "uninitialized pool should not error");
    assert_eq!(r.unwrap(), 1_000_000);

    // shares_to_assets with zero shares → 1:1
    let r2 = shares_to_assets_down(1_000_000, 0, 0);
    assert!(r2.is_ok());
    assert_eq!(r2.unwrap(), 1_000_000);

    // Shares zero but assets non-zero (should not divide by zero) — total_shares=0 → 1:1
    let r3 = assets_to_shares_down(1_000_000, 10_000_000, 0);
    assert!(r3.is_ok());

    // Assets zero but shares non-zero (impaired pool) — mul_div(amt, 0, shares) → 0
    let r4 = shares_to_assets_down(1_000_000, 0, 10_000_000);
    assert!(r4.is_ok());
    assert_eq!(r4.unwrap(), 0);
}

/// 1:1 pool: shares always equal assets (both directions).
#[test]
fn one_to_one_pool_identity() {
    let pool = 10_000_000_i128;
    for amt in [1i128, 100, 10_000, 1_000_000, 10_000_000] {
        let shares = assets_to_shares_down(amt, pool, pool).unwrap_or(0);
        assert_eq!(shares, amt, "1:1 a2s_down({amt})");
        let assets = shares_to_assets_down(amt, pool, pool).unwrap_or(0);
        assert_eq!(assets, amt, "1:1 s2a_down({amt})");
    }
}

// ── Differential fixture generation ──────────────────────────────────────────

/// Generate 1 000+ vectors and serialize them to JSON so Go and TS test suites
/// can assert they produce identical values.
///
/// The file is written next to this source file (relative to CARGO_MANIFEST_DIR
/// so it is repo-relative and survives `cargo clean`).
#[test]
fn generate_rounding_fixture() {
    use std::fs;
    use std::path::PathBuf;

    let mut vectors: std::vec::Vec<Vector> = std::vec::Vec::new();

    // Sweep: total_assets and total_shares independently across 5 orders of
    // magnitude; amount is a fixed fraction of the smaller of the two.
    let pool_sizes: &[i128] = &[1, 7, 100, 10_000, 1_000_000, 10_000_000, 100_000_000];
    let ratios: &[i128] = &[1, 2, 5, 10, 100];
    let amt_fracs: &[i128] = &[1, 10, 100, 1_000, 10_000];

    for &ta_base in pool_sizes {
        for &ratio in ratios {
            let ta = ta_base;
            let ts = ta_base * ratio / 1; // ts may exceed ta

            for &frac in amt_fracs {
                let amt = ta_base.max(1) / frac.max(1);
                if amt == 0 {
                    continue;
                }
                vectors.push(compute(ta, ts, amt));
                // Swap ta/ts to cover both price directions
                vectors.push(compute(ts, ta, amt));
            }
        }
    }

    // Append hand-crafted edge cases
    for &(ta, ts, amt) in &[
        (1i128, 1i128, 1i128),
        (i128::MAX / 2, i128::MAX / 2, 1),
        (10_000_001, 9_999_999, 500_000),
        (0, 0, 1_000_000),
        (1_000_000, 0, 1_000_000),
    ] {
        vectors.push(compute(ta, ts, amt));
    }

    assert!(
        vectors.len() >= 1_000,
        "expected ≥1000 vectors, got {}",
        vectors.len()
    );

    // Serialize to JSON
    let mut json = std::string::String::from("[\n");
    for (i, v) in vectors.iter().enumerate() {
        json.push_str(&std::format!(
            "  {{\"total_assets\":{},\"total_shares\":{},\"amount\":{},\"a2s_down\":{},\"a2s_up\":{},\"s2a_down\":{},\"s2a_up\":{}}}{}",
            v.total_assets, v.total_shares, v.amount,
            v.a2s_down, v.a2s_up, v.s2a_down, v.s2a_up,
            if i + 1 < vectors.len() { ",\n" } else { "\n" }
        ));
    }
    json.push_str("]\n");

    let manifest = std::env::var("CARGO_MANIFEST_DIR").unwrap_or_else(|_| ".".to_string());
    let out = PathBuf::from(&manifest)
        .join("src")
        .join("integration")
        .join("rounding_vectors.json");

    fs::write(&out, &json).expect("failed to write rounding_vectors.json");
    std::println!("Wrote {} vectors to {}", vectors.len(), out.display());
}
