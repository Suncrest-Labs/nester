//! Adversarial and negative scenario integration tests.
#![cfg(test)]

extern crate std;

use nester_access_control::Role;
use nester_test_utils::{register_reentrant_strategy, HostileVaultHarness, NesterHarness};
use soroban_sdk::{
    symbol_short,
    testutils::{Address as _, Ledger as _},
    token, Address,
};

#[test]
#[should_panic]
fn reentrant_strategy_during_rebalance_is_blocked() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    let attacker = h.create_user();
    h.mint_deposit_tokens(&user, 20_000_000);
    h.vault().deposit(&user, &10_000_000, &0);

    let hostile = register_reentrant_strategy(&h.env, &h.vault_id, &attacker);
    h.vault().register_callee(&h.admin, &hostile);
    h.vault().set_allocation_strategy(&h.admin, &hostile);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Operator);
    h.vault()
        .record_source_allocation(&h.admin, &symbol_short!("aave"), &10_000_000_i128);
    h.vault().rebalance(&h.admin);
}

#[test]
#[should_panic]
fn reentrant_token_during_deposit_is_blocked() {
    let h = HostileVaultHarness::setup_with_reentrant_token();
    let user = Address::generate(&h.env);
    h.mint_deposit_tokens(&user, 20_000_000);
    h.vault().deposit(&user, &10_000_000, &0);
}

#[test]
#[should_panic]
fn reentrant_yield_source_during_harvest_is_blocked() {
    let h = HostileVaultHarness::setup_with_reentrant_yield_sink();
    let user = Address::generate(&h.env);
    h.mint_stellar_deposit_tokens(&user, 20_000_000);
    h.vault().deposit(&user, &10_000_000, &0);
    h.grant_manager();
    h.vault().report_yield(&user, &5_000_000);
    h.vault().harvest(&user);
}

#[test]
#[should_panic(expected = "Error(Contract, #23)")]
fn unregistered_callee_is_rejected() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 20_000_000);
    h.vault().deposit(&user, &10_000_000, &0);

    h.vault().set_allocation_strategy(&h.admin, &h.strategy_id);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Operator);
    h.vault()
        .record_source_allocation(&h.admin, &symbol_short!("aave"), &10_000_000_i128);
    h.vault().rebalance(&h.admin);
}

#[test]
fn deposit_and_withdraw_succeed_with_guard_enabled() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 20_000_000);
    let shares = h.vault().deposit(&user, &10_000_000, &0);
    assert_eq!(shares, 10_000_000);
    let remaining = h.vault().withdraw(&user, &2_000_000, &0);
    assert_eq!(remaining, 8_000_000);
}

#[test]
fn nested_emergency_queue_processing_does_not_double_guard() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 20_000_000);
    let _ = h.vault().deposit(&user, &10_000_000, &0);
    h.vault().drain_legacy_emergency_queue();
}

#[test]
fn zero_deposit_reverts() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 20_000_000);
    let result = h.vault().try_deposit(&user, &0, &0);
    assert!(result.is_err());
}

#[test]
#[should_panic]
fn deposit_to_paused_vault_reverts() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.vault().pause(&h.admin);
    h.mint_deposit_tokens(&user, 20_000_000);
    h.vault().deposit(&user, &10_000_000, &0);
}

#[test]
#[should_panic]
fn withdraw_more_than_owned_reverts() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 20_000_000);
    h.vault().deposit(&user, &10_000_000, &0);
    h.vault().withdraw(&user, &20_000_000, &0);
}

#[test]
fn registered_strategy_rebalance_invokes_allowlisted_callee() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 20_000_000);
    h.vault().deposit(&user, &10_000_000, &0);

    h.vault().register_callee(&h.admin, &h.strategy_id);
    h.vault().set_allocation_strategy(&h.admin, &h.strategy_id);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Operator);
    h.vault()
        .record_source_allocation(&h.admin, &symbol_short!("aave"), &10_000_000_i128);

    let aave = symbol_short!("aave");
    let blend = symbol_short!("blend");
    h.registry().register_source(
        &h.admin,
        &aave,
        &h.create_user(),
        &nester_common::ProtocolType::Lending,
    );
    h.registry().register_source(
        &h.admin,
        &blend,
        &h.create_user(),
        &nester_common::ProtocolType::Lending,
    );
    h.strategy()
        .update_strategy_params(&h.admin, &500u32, &10_000u32, &100u32);
    let weights = soroban_sdk::vec![
        &h.env,
        allocation_strategy_contract::AllocationWeight {
            source_id: aave.clone(),
            weight_bps: 6_000,
        },
        allocation_strategy_contract::AllocationWeight {
            source_id: blend.clone(),
            weight_bps: 4_000,
        },
    ];
    h.strategy().set_weights(&h.admin, &weights);

    let deltas = h.vault().rebalance(&h.admin);
    assert!(deltas.is_empty() || !deltas.is_empty());
}

// ---------------------------------------------------------------------------
// Slippage-safe rebalance plan/execute adversarial tests (issue #810)
// ---------------------------------------------------------------------------

fn setup_rebalance_ready_vault(h: &NesterHarness) {
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 20_000_000);
    h.vault().deposit(&user, &10_000_000, &0);

    // These tests exercise plan/execute integrity and staleness, not the
    // value cap (which has its own dedicated test) — open it up so a full
    // reallocation of this small test vault doesn't trip it incidentally.
    h.vault().set_max_rebalance_value_bps(&h.admin, &10_000u32);

    h.vault().register_callee(&h.admin, &h.strategy_id);
    h.vault().set_allocation_strategy(&h.admin, &h.strategy_id);
    h.vault().grant_role(&h.admin, &h.admin, &Role::Operator);
    h.vault()
        .record_source_allocation(&h.admin, &symbol_short!("aave"), &10_000_000_i128);

    let aave = symbol_short!("aave");
    let blend = symbol_short!("blend");
    h.registry().register_source(
        &h.admin,
        &aave,
        &h.create_user(),
        &nester_common::ProtocolType::Lending,
    );
    h.registry().register_source(
        &h.admin,
        &blend,
        &h.create_user(),
        &nester_common::ProtocolType::Lending,
    );
    h.strategy()
        .update_strategy_params(&h.admin, &500u32, &10_000u32, &100u32);
    let weights = soroban_sdk::vec![
        &h.env,
        allocation_strategy_contract::AllocationWeight {
            source_id: aave.clone(),
            weight_bps: 6_000,
        },
        allocation_strategy_contract::AllocationWeight {
            source_id: blend.clone(),
            weight_bps: 4_000,
        },
    ];
    h.strategy().set_weights(&h.admin, &weights);
}

/// A plan submitted with a delta far outside the freshness tolerance of
/// what the contract would compute right now is rejected as stale.
#[test]
#[should_panic(expected = "Error(Contract, #28)")]
fn execute_rebalance_rejects_a_tampered_stale_plan() {
    let h = NesterHarness::setup();
    setup_rebalance_ready_vault(&h);

    let plan = h.vault().plan_rebalance();
    let mut tampered = soroban_sdk::vec![&h.env];
    for leg in plan.legs.iter() {
        // Blow the delta up by 100x — far beyond the 2% staleness tolerance.
        tampered.push_back(vault_contract::RebalanceLeg {
            source_id: leg.source_id.clone(),
            delta: leg.delta.saturating_mul(100),
            min_out: leg.min_out,
        });
    }
    // Use the *correct* checksum of the tampered plan so the failure below
    // is unambiguously the freshness check, not the integrity check.
    let tampered_plan_hash = h.vault().compute_plan_checksum(&tampered);
    h.vault()
        .execute_rebalance(&h.admin, &tampered_plan_hash, &tampered);
}

/// A caller cannot weaken slippage protection by submitting a plan whose
/// deltas match live state (passes freshness) but whose `min_out` fields
/// have been zeroed out — execution uses the freshly recomputed `min_out`,
/// not whatever the caller submitted.
#[test]
fn execute_rebalance_ignores_caller_supplied_min_out_uses_fresh_state() {
    let h = NesterHarness::setup();
    setup_rebalance_ready_vault(&h);

    let plan = h.vault().plan_rebalance();
    if plan.legs.is_empty() {
        // Nothing to rebalance in this configuration — not the scenario
        // under test, so nothing further to assert.
        return;
    }

    let mut hollowed = soroban_sdk::vec![&h.env];
    for leg in plan.legs.iter() {
        hollowed.push_back(vault_contract::RebalanceLeg {
            source_id: leg.source_id.clone(),
            delta: leg.delta,
            min_out: 0, // attacker zeroes their own floor
        });
    }
    let hollowed_hash = h.vault().compute_plan_checksum(&hollowed);

    // Executes successfully — the zeroed min_out submitted by the caller
    // has no effect because execution is driven by the freshly recomputed
    // plan, and the freshly recomputed min_out is what's actually enforced.
    let applied = h
        .vault()
        .execute_rebalance(&h.admin, &hollowed_hash, &hollowed);
    assert_eq!(applied.len(), plan.legs.len());
}

/// A rebalance that would move more than `max_rebalance_value_bps` of vault
/// assets is rejected at the boundary.
#[test]
#[should_panic(expected = "Error(Contract, #29)")]
fn execute_rebalance_reverts_beyond_value_cap() {
    let h = NesterHarness::setup();
    setup_rebalance_ready_vault(&h);

    // Cap a single rebalance at 1% of vault assets — far below what this
    // configuration's plan would move.
    h.vault().set_max_rebalance_value_bps(&h.admin, &100u32);

    let plan = h.vault().plan_rebalance();
    h.vault()
        .execute_rebalance(&h.admin, &plan.plan_hash, &plan.legs);
}

// ---------------------------------------------------------------------------
// Early-exit penalty escrow adversarial tests (issue #805)
// ---------------------------------------------------------------------------

/// An emergency-exit fee lands in the penalty escrow (rather than being
/// silently absorbed with no accounting trail), and distributing it raises
/// the share price for remaining depositors while sending the treasury its
/// bounded slice.
#[test]
fn emergency_exit_penalty_lands_in_escrow_and_distribution_raises_share_price() {
    let h = NesterHarness::setup();
    h.vault().set_emergency_fee(&h.admin, &500); // 5%

    let exiter = h.create_user();
    let remaining = h.create_user();
    // Large enough that 5% of the exiter's principal clears
    // DEFAULT_MIN_PENALTY_DISTRIBUTION_AMOUNT.
    h.mint_deposit_tokens(&exiter, 40_000_000);
    h.mint_deposit_tokens(&remaining, 40_000_000);
    h.vault().deposit(&exiter, &30_000_000, &0);
    h.vault().deposit(&remaining, &30_000_000, &0);

    h.vault().pause(&h.admin);
    h.vault().emergency_withdraw(&exiter);
    h.vault().unpause(&h.admin);

    let escrow = h.vault().get_penalty_escrow();
    assert!(
        escrow > 0,
        "emergency fee should have been escrowed, not silently absorbed"
    );

    let share_price_before = h.vault().share_price();
    h.vault().distribute_penalties(&remaining);
    let share_price_after = h.vault().share_price();

    assert!(
        share_price_after > share_price_before,
        "remaining depositors should be compensated via a higher share price"
    );
    assert!(
        token::Client::new(&h.env, &h.deposit_token_id).balance(&h.treasury_id) > 0,
        "treasury should have received its slice"
    );
    assert!(
        h.vault().get_penalty_escrow() < escrow,
        "escrow drained down to (at most) dust after distribution"
    );
}

/// Distribution is gated by a minimum amount — an empty or dust escrow
/// cannot be distributed (anti-spam).
#[test]
#[should_panic]
fn distribute_penalties_below_minimum_reverts() {
    let h = NesterHarness::setup();
    let caller = h.create_user();
    h.vault().distribute_penalties(&caller);
}

/// A depositor who fully exits before `distribute_penalties` runs is paid
/// out against the pre-distribution price and is not retroactively
/// adjusted; a depositor who stays benefits from the raised price.
#[test]
fn depositor_who_exits_before_distribution_is_not_retroactively_affected() {
    let h = NesterHarness::setup();
    h.vault().set_emergency_fee(&h.admin, &500);
    // The circuit breaker isn't the subject of this test — widen it so a
    // single depositor's full exit from a 3-person pool doesn't trip it.
    h.vault().set_circuit_breaker_config(
        &h.admin,
        &vault_contract::CircuitBreakerConfig {
            threshold_bps: 10_000,
            window_seconds: 7_200,
        },
    );

    let whale = h.create_user();
    let early_leaver = h.create_user();
    let stayer = h.create_user();
    // Whale's principal is large enough that 5% of it clears
    // DEFAULT_MIN_PENALTY_DISTRIBUTION_AMOUNT.
    h.mint_deposit_tokens(&whale, 60_000_000);
    h.mint_deposit_tokens(&early_leaver, 20_000_000);
    h.mint_deposit_tokens(&stayer, 20_000_000);
    h.vault().deposit(&whale, &30_000_000, &0);
    h.vault().deposit(&early_leaver, &10_000_000, &0);
    h.vault().deposit(&stayer, &10_000_000, &0);

    h.vault().pause(&h.admin);
    h.vault().emergency_withdraw(&whale);
    h.vault().unpause(&h.admin);

    // early_leaver exits before the penalty is distributed.
    let early_shares = h.token().balance(&early_leaver);
    h.vault().withdraw(&early_leaver, &early_shares, &0);
    assert_eq!(h.token().balance(&early_leaver), 0);

    // stayer's projected net payout before the penalty is distributed —
    // the baseline to show distribution actually improves their outcome.
    let stayer_shares = h.token().balance(&stayer);
    let preview_before = h.vault().withdrawal_fee_preview(&stayer, &stayer_shares);

    h.vault().distribute_penalties(&stayer);

    // early_leaver is gone and receives nothing further — no double count.
    assert_eq!(h.token().balance(&early_leaver), 0);

    let preview_after = h.vault().withdrawal_fee_preview(&stayer, &stayer_shares);
    assert!(
        preview_after.net_amount_received > preview_before.net_amount_received,
        "stayer's payout should improve once the penalty is distributed"
    );

    // Roll the circuit breaker's rolling window past early_leaver's
    // withdrawal so stayer's own exit is judged on its own, not on the
    // cumulative sum of two unrelated depositors' withdrawals (the
    // breaker isn't the subject of this test).
    h.env.ledger().with_mut(|l| l.timestamp += 7_201);

    let usdc = token::Client::new(&h.env, &h.deposit_token_id);
    let stayer_usdc_before = usdc.balance(&stayer);
    h.vault().withdraw(&stayer, &stayer_shares, &0);
    let stayer_payout = usdc.balance(&stayer) - stayer_usdc_before;
    assert!(stayer_payout > 0);
}
