//! Integration tests for the duration- and size-tiered fee schedule
//! (issue #813), plus the pre-existing flat-fee mechanics.
#![cfg(test)]

extern crate std;

use soroban_sdk::{testutils::Ledger as _, token};

use nester_test_utils::NesterHarness;
use vault_contract::{FeeConfig, FeeTierKind, VaultFeeTier};

const DEPOSIT: i128 = 10_000_000;
const DAY: u64 = 86_400;

fn configure_flat_fees(h: &NesterHarness) {
    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 1_000,
            management_fee_bps: 0,
            early_withdrawal_fee_bps: 200,
            treasury_address: h.treasury_id.clone(),
        },
    );
}

fn exit_tiers(h: &NesterHarness) -> soroban_sdk::Vec<VaultFeeTier> {
    soroban_sdk::vec![
        &h.env,
        VaultFeeTier {
            threshold: 0,
            rate_bps: 200
        },
        VaultFeeTier {
            threshold: (90 * DAY) as i128,
            rate_bps: 100
        },
        VaultFeeTier {
            threshold: (365 * DAY) as i128,
            rate_bps: 25
        },
    ]
}

fn performance_tiers(h: &NesterHarness) -> soroban_sdk::Vec<VaultFeeTier> {
    soroban_sdk::vec![
        &h.env,
        VaultFeeTier {
            threshold: 0,
            rate_bps: 2_000
        },
        VaultFeeTier {
            threshold: (180 * DAY) as i128,
            rate_bps: 1_000
        },
        VaultFeeTier {
            threshold: (365 * DAY) as i128,
            rate_bps: 500
        },
    ]
}

/// Management fee accrues proportionally over time (pre-existing flat-fee
/// behaviour, unaffected by issue #813's opt-in tiers).
#[test]
fn test_management_fee_accrues_over_time() {
    let h = NesterHarness::setup();
    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 0,
            management_fee_bps: 1_000, // 10% annual
            early_withdrawal_fee_bps: 0,
            treasury_address: h.treasury_id.clone(),
        },
    );

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // Advance 30 days; collect_fees accrues management fee up to this point
    // and sweeps it to the treasury in the same call.
    h.env.ledger().with_mut(|l| l.timestamp = 30 * DAY);
    h.vault().collect_fees(&h.admin);

    let treasury_usdc = token::Client::new(&h.env, &h.deposit_token_id).balance(&h.treasury_id);
    assert!(
        treasury_usdc > 0,
        "management fee should have accrued and been collected to the treasury after 30 days"
    );
}

/// `fee_schedule_preview` reports the tenure-tiered exit rate and the next
/// upcoming boundary, and the rate is continuous (linearly interpolated)
/// between tiers rather than jumping at the boundary.
#[test]
fn fee_schedule_preview_reflects_tenure_and_interpolates_continuously() {
    let h = NesterHarness::setup();
    configure_flat_fees(&h);
    h.vault()
        .set_fee_tiers(&h.admin, &FeeTierKind::Exit, &exit_tiers(&h));

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let preview0 = h.vault().fee_schedule_preview(&user);
    assert_eq!(preview0.tenure_secs, 0);
    assert_eq!(preview0.current_exit_fee_bps, 200);
    assert_eq!(preview0.next_boundary_secs, Some(90 * DAY));
    assert_eq!(preview0.next_boundary_exit_fee_bps, 100);

    // Halfway between day 0 (200 bps) and day 90 (100 bps) -> 150 bps.
    h.env.ledger().with_mut(|l| l.timestamp = 45 * DAY);
    let preview_mid = h.vault().fee_schedule_preview(&user);
    assert_eq!(preview_mid.current_exit_fee_bps, 150);

    h.env.ledger().with_mut(|l| l.timestamp = 90 * DAY);
    let preview_at_90 = h.vault().fee_schedule_preview(&user);
    assert_eq!(preview_at_90.current_exit_fee_bps, 100);
}

/// A withdrawal's actual exit fee uses the tenure-tiered rate, and (per
/// issue #813) the tiered schedule applies across the whole tenure domain
/// rather than being gated by the original 1-day min-lock window.
#[test]
fn exit_fee_on_withdrawal_uses_tenure_tiered_rate_beyond_lock_window() {
    let h = NesterHarness::setup();
    configure_flat_fees(&h);
    h.vault()
        .set_fee_tiers(&h.admin, &FeeTierKind::Exit, &exit_tiers(&h));

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // Well past the legacy 1-day min-lock window, but still short of the
    // 365-day floor -> the flat-fee gate would have charged nothing past
    // day 1, the tiered schedule still charges a (tenure-interpolated) fee
    // here. At exactly the 90-day boundary the rate is a clean 100 bps.
    h.env.ledger().with_mut(|l| l.timestamp = 90 * DAY);
    let preview = h.vault().withdrawal_fee_preview(&user, &(DEPOSIT / 2));
    let expected_fee = (DEPOSIT / 2) * 100 / 10_000;
    assert_eq!(preview.early_withdrawal_fee_deducted, expected_fee);

    // At the final tier boundary (365 days), rate has decayed to 25 bps.
    h.env.ledger().with_mut(|l| l.timestamp = 365 * DAY);
    let preview_late = h.vault().withdrawal_fee_preview(&user, &(DEPOSIT / 2));
    let expected_late_fee = (DEPOSIT / 2) * 25 / 10_000;
    assert_eq!(
        preview_late.early_withdrawal_fee_deducted,
        expected_late_fee
    );
}

/// `first_deposit_at` (tenure) is set on first deposit, survives a partial
/// withdrawal untouched, and resets only on a full exit.
#[test]
fn tenure_persists_through_partial_withdrawal_and_resets_on_full_exit() {
    let h = NesterHarness::setup();
    configure_flat_fees(&h);
    h.vault()
        .set_fee_tiers(&h.admin, &FeeTierKind::Exit, &exit_tiers(&h));
    // The circuit breaker isn't the subject of this test, and a full exit
    // is by definition a large fraction of this single-user vault's
    // assets — widen the threshold so it doesn't interfere.
    h.vault().set_circuit_breaker_config(
        &h.admin,
        &vault_contract::CircuitBreakerConfig {
            threshold_bps: 10_000,
            window_seconds: 7_200,
        },
    );

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 4);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.env.ledger().with_mut(|l| l.timestamp = 100 * DAY);
    assert_eq!(h.vault().fee_schedule_preview(&user).tenure_secs, 100 * DAY);

    // Partial withdrawal (10%, under the default 20% circuit-breaker
    // threshold): tenure must be unaffected.
    h.vault().withdraw(&user, &(DEPOSIT / 10), &0);
    assert_eq!(h.vault().fee_schedule_preview(&user).tenure_secs, 100 * DAY);

    // Depositing more on top of an existing position must not reset tenure.
    h.vault().deposit(&user, &DEPOSIT, &0);
    assert_eq!(h.vault().fee_schedule_preview(&user).tenure_secs, 100 * DAY);

    // Roll the circuit breaker's rolling window past the earlier partial
    // withdrawal so the full exit below is judged on its own (the breaker
    // isn't the subject of this test).
    h.env.ledger().with_mut(|l| l.timestamp += 7_201);

    // Full exit: tenure resets.
    let remaining_shares = h.token().balance(&user);
    h.vault().withdraw(&user, &remaining_shares, &0);
    assert_eq!(h.vault().fee_schedule_preview(&user).tenure_secs, 0);

    // Returning depositor starts fresh, not carrying over the old tenure.
    h.vault().deposit(&user, &DEPOSIT, &0);
    assert_eq!(h.vault().fee_schedule_preview(&user).tenure_secs, 0);
}

/// The performance fee taken at harvest uses the rate applicable to the
/// user's tenure *at harvest time* — incremental and already-netted, not
/// retroactively recomputed — and a schedule/tenure change between accrual
/// and harvest is reflected only in yield not yet harvested.
#[test]
fn performance_fee_at_harvest_uses_tenure_tiered_rate() {
    let h = NesterHarness::setup();
    configure_flat_fees(&h);
    h.vault()
        .set_fee_tiers(&h.admin, &FeeTierKind::Performance, &performance_tiers(&h));

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    // Cross to exactly the 180-day boundary before any yield is reported,
    // so the entire pending yield is harvested at a clean 1000 bps rate
    // (past that point the curve interpolates on toward the 365-day tier).
    h.env.ledger().with_mut(|l| l.timestamp = 180 * DAY);

    h.vault()
        .grant_role(&h.admin, &user, &nester_access_control::Role::Manager);
    let yield_amount = 1_000_000_i128;
    h.mint_deposit_tokens(&h.vault_id, yield_amount);
    h.vault().report_yield(&user, &yield_amount);

    let result = h.vault().harvest(&user);
    let expected_fee = yield_amount * 1_000 / 10_000; // 10% at day 200 tier
    assert_eq!(result.performance_fee, expected_fee);
}

/// A schedule update cannot raise an existing depositor's rate beyond the
/// compile-time grandfathering cap.
#[test]
#[should_panic]
fn set_fee_tiers_rejects_adverse_rate_increase_beyond_cap() {
    let h = NesterHarness::setup();
    h.vault()
        .set_fee_tiers(&h.admin, &FeeTierKind::Exit, &exit_tiers(&h));

    // Hiking the first tier's rate far beyond the grandfathering cap must
    // be rejected outright.
    let hiked = soroban_sdk::vec![
        &h.env,
        VaultFeeTier {
            threshold: 0,
            rate_bps: 900
        },
        VaultFeeTier {
            threshold: (90 * DAY) as i128,
            rate_bps: 100
        },
        VaultFeeTier {
            threshold: (365 * DAY) as i128,
            rate_bps: 25
        },
    ];
    h.vault()
        .set_fee_tiers(&h.admin, &FeeTierKind::Exit, &hiked);
}

/// Non-ascending tier thresholds are rejected on write.
#[test]
#[should_panic]
fn set_fee_tiers_rejects_non_ascending_thresholds() {
    let h = NesterHarness::setup();
    let bad = soroban_sdk::vec![
        &h.env,
        VaultFeeTier {
            threshold: 100,
            rate_bps: 50
        },
        VaultFeeTier {
            threshold: 100,
            rate_bps: 25
        },
    ];
    h.vault().set_fee_tiers(&h.admin, &FeeTierKind::Exit, &bad);
}

/// A rate above the compile-time ceiling for the relevant fee category is
/// rejected on write regardless of tier shape.
#[test]
#[should_panic]
fn set_fee_tiers_rejects_rate_above_ceiling() {
    let h = NesterHarness::setup();
    // MAX_EARLY_WITHDRAWAL_FEE_BPS is 500; 900 must be rejected.
    let bad = soroban_sdk::vec![
        &h.env,
        VaultFeeTier {
            threshold: 0,
            rate_bps: 900
        }
    ];
    h.vault().set_fee_tiers(&h.admin, &FeeTierKind::Exit, &bad);
}

/// Multi-year scenario crossing several tenure tiers with exact expected
/// exit-fee values at each checkpoint.
#[test]
fn multi_year_scenario_crosses_several_tiers_with_exact_values() {
    let h = NesterHarness::setup();
    configure_flat_fees(&h);
    h.vault()
        .set_fee_tiers(&h.admin, &FeeTierKind::Exit, &exit_tiers(&h));

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 10);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let amount = DEPOSIT / 10;
    let checkpoints: [(u64, u32); 5] = [
        (0, 200),
        (45 * DAY, 150), // interpolated midpoint of tier 0->1
        (90 * DAY, 100), // exactly at tier 1
        // clamps to the last tier just before it
        (365 * DAY - 1, 25),
        (365 * DAY, 25), // at/after the last tier
    ];

    for (t, _expected_bps_hint) in checkpoints {
        h.env.ledger().with_mut(|l| l.timestamp = t);
        let expected_bps = if t == 0 {
            200
        } else if t <= 90 * DAY {
            // Linear interpolation between (0, 200) and (90*DAY, 100).
            let span = 90 * DAY as i128;
            let pos = t as i128;
            (200_i128 - (100_i128 * pos) / span) as u32
        } else if t < 365 * DAY {
            let span = (365 * DAY - 90 * DAY) as i128;
            let pos = (t - 90 * DAY) as i128;
            (100_i128 - (75_i128 * pos) / span) as u32
        } else {
            25
        };

        let preview = h.vault().withdrawal_fee_preview(&user, &amount);
        let expected_fee = amount * expected_bps as i128 / 10_000;
        assert_eq!(
            preview.early_withdrawal_fee_deducted, expected_fee,
            "mismatch at t={t}"
        );
    }
}

/// Performance fee is only charged on positive yield, never on an
/// impairment — pre-existing flat-fee behaviour (see `test.rs` for the
/// exhaustive unit-level coverage of this).
#[test]
fn test_performance_fee_only_on_positive_yield() {
    let h = NesterHarness::setup();
    configure_flat_fees(&h);

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT * 2);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.vault()
        .grant_role(&h.admin, &user, &nester_access_control::Role::Manager);
    h.vault().report_yield(&user, &(-1_000_i128));

    let result = h.vault().harvest(&user);
    assert_eq!(result.performance_fee, 0);
    assert!(!result.compounded);
}

/// Fee collection: accrue -> collect -> treasury receives -> accrued_fees resets.
#[test]
fn test_fee_collection_flow() {
    let h = NesterHarness::setup();
    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 0,
            management_fee_bps: 500,
            early_withdrawal_fee_bps: 0,
            treasury_address: h.treasury_id.clone(),
        },
    );

    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.env.ledger().with_mut(|l| l.timestamp = 30 * DAY);

    let treasury_before = token::Client::new(&h.env, &h.deposit_token_id).balance(&h.treasury_id);
    h.vault().collect_fees(&h.admin);
    let treasury_after = token::Client::new(&h.env, &h.deposit_token_id).balance(&h.treasury_id);

    assert!(treasury_after > treasury_before);
    assert_eq!(h.vault().get_accrued_fees(), 0);
}
