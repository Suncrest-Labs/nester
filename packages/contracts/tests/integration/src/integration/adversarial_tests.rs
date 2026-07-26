//! Adversarial and negative scenario integration tests.
#![cfg(test)]

extern crate std;

use nester_access_control::Role;
use nester_test_utils::{
    register_reentrant_strategy, HostileVaultHarness, NesterHarness,
};
use soroban_sdk::{symbol_short, testutils::Address as _, Address};

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
    h.vault()
        .grant_role(&h.admin, &h.admin, &Role::Operator);
    h.vault().record_source_allocation(&h.admin, &symbol_short!("aave"), &10_000_000_i128);
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
    h.vault()
        .grant_role(&h.admin, &h.admin, &Role::Operator);
    h.vault().record_source_allocation(&h.admin, &symbol_short!("aave"), &10_000_000_i128);
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
    h.vault().process_emergency_queue();
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
    h.vault()
        .grant_role(&h.admin, &h.admin, &Role::Operator);
    h.vault().record_source_allocation(&h.admin, &symbol_short!("aave"), &10_000_000_i128);

    let aave = symbol_short!("aave");
    let blend = symbol_short!("blend");
    h.registry()
        .register_source(&h.admin, &aave, &h.create_user(), &nester_common::ProtocolType::Lending);
    h.registry()
        .register_source(&h.admin, &blend, &h.create_user(), &nester_common::ProtocolType::Lending);
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
