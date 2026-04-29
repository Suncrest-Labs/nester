// CI trigger: update from 0xDeon fork
// ---------------------------------------------------------------------------
// Nester Protocol — Multi-Contract Integration Tests
//
// These tests exercise interactions that cross contract boundaries, validating
// that the contracts function correctly as a coherent system rather than in
// isolation.
//
// Run with:
//   cargo test -p nester-integration-tests
// or via the Makefile target:
//   make integration-test
// ---------------------------------------------------------------------------

#![cfg(test)]

extern crate std;

use soroban_sdk::{symbol_short, vec, Vec};

use allocation_strategy_contract::AllocationWeight;
use nester_access_control::Role;
use nester_common::ProtocolType;
use nester_test_utils::NesterHarness;

// ---------------------------------------------------------------------------
// Scenario 1 — Full protocol initialisation
// ---------------------------------------------------------------------------

/// All five contracts deploy and initialise without error, and cross-references
/// are wired correctly.
#[test]
fn all_contracts_initialise_cleanly() {
    let h = NesterHarness::setup();

    // Vault: not paused after initialisation.
    assert!(
        !h.vault().is_paused(),
        "vault should not be paused after init"
    );

    // Token: supply and assets start at zero.
    assert_eq!(h.token().total_supply(), 0);
    assert_eq!(h.token().total_assets(), 0);

    // Registry: no sources registered yet.
    let active = h.registry().get_active_sources();
    assert_eq!(
        active.len(),
        0,
        "registry should have no sources after init"
    );

    // Strategy: no weights set yet.
    let weights = h.strategy().get_weights();
    assert_eq!(
        weights.len(),
        0,
        "strategy should have no weights after init"
    );
}

// ---------------------------------------------------------------------------
// Scenario 2 — Registry + Strategy cross-contract flow
// ---------------------------------------------------------------------------

/// Register sources in the registry, then set weights in the strategy.
/// `set_weights` performs a cross-contract call to validate that each source
/// is active — this is the primary registry ↔ strategy integration point.
#[test]
fn strategy_set_weights_validates_sources_via_registry() {
    let h = NesterHarness::setup();

    let aave = symbol_short!("aave");
    let blend = symbol_short!("blend");

    h.registry().register_source(
        &h.admin,
        &aave,
        &h.create_user(), // mock contract address
        &ProtocolType::Lending,
    );
    h.registry()
        .register_source(&h.admin, &blend, &h.create_user(), &ProtocolType::Lending);

    // Both sources are active; set_weights should succeed.
    let weights: Vec<AllocationWeight> = vec![
        &h.env,
        AllocationWeight {
            source_id: aave.clone(),
            weight_bps: 6_000,
        },
        AllocationWeight {
            source_id: blend.clone(),
            weight_bps: 4_000,
        },
    ];
    h.strategy().set_weights(&h.admin, &weights);

    let stored = h.strategy().get_weights();
    assert_eq!(stored.len(), 2);
    assert_eq!(stored.get(0).unwrap().weight_bps, 6_000);
    assert_eq!(stored.get(1).unwrap().weight_bps, 4_000);
}

/// Weights that reference an unknown source must be rejected by the strategy
/// (the cross-contract validation fails).
#[test]
#[should_panic]
fn strategy_rejects_weights_for_unregistered_source() {
    let h = NesterHarness::setup();

    let ghost = symbol_short!("ghost");
    let weights: Vec<AllocationWeight> = vec![
        &h.env,
        AllocationWeight {
            source_id: ghost,
            weight_bps: 10_000,
        },
    ];
    h.strategy().set_weights(&h.admin, &weights); // must panic
}

/// Weights that reference a paused (inactive) source must be rejected.
#[test]
#[should_panic]
fn strategy_rejects_weights_for_paused_source() {
    let h = NesterHarness::setup();
    use nester_common::SourceStatus;

    let aave = symbol_short!("aave");
    h.registry()
        .register_source(&h.admin, &aave, &h.create_user(), &ProtocolType::Lending);
    // Pause the source so it is no longer active.
    h.registry()
        .update_status(&h.admin, &aave, &SourceStatus::Paused);

    let weights: Vec<AllocationWeight> = vec![
        &h.env,
        AllocationWeight {
            source_id: aave,
            weight_bps: 10_000,
        },
    ];
    h.strategy().set_weights(&h.admin, &weights); // must panic
}

// ---------------------------------------------------------------------------
// Scenario 3 — Allocation calculation
// ---------------------------------------------------------------------------

/// After setting weights, `calculate_allocation` distributes a total across
/// sources according to their weights in basis points.
#[test]
fn calculate_allocation_distributes_total_proportionally() {
    let h = NesterHarness::setup();

    let aave = symbol_short!("aave");
    let blend = symbol_short!("blend");

    h.registry()
        .register_source(&h.admin, &aave, &h.create_user(), &ProtocolType::Lending);
    h.registry()
        .register_source(&h.admin, &blend, &h.create_user(), &ProtocolType::Staking);

    let weights: Vec<AllocationWeight> = vec![
        &h.env,
        AllocationWeight {
            source_id: aave.clone(),
            weight_bps: 7_000,
        },
        AllocationWeight {
            source_id: blend.clone(),
            weight_bps: 3_000,
        },
    ];
    h.strategy().set_weights(&h.admin, &weights);

    let total = 100_000_i128;
    let allocations = h.strategy().calculate_allocation(&total);

    // Sum of all allocations must equal total (including rounding remainder).
    let sum: i128 = allocations.iter().map(|(_, a)| a).sum();
    assert_eq!(sum, total, "allocations must sum to total");

    // aave gets 70%, blend gets 30%.
    let aave_alloc = h.strategy().get_source_allocation(&aave);
    let blend_alloc = h.strategy().get_source_allocation(&blend);

    assert_eq!(aave_alloc + blend_alloc, total);
    assert_eq!(blend_alloc, 30_000);
    // Rounding remainder goes to the highest-weight source (aave).
    assert_eq!(aave_alloc, 70_000);
}

/// Three sources — validates remainder assignment with uneven division.
#[test]
fn calculate_allocation_assigns_remainder_to_highest_weight_source() {
    let h = NesterHarness::setup();

    let a = symbol_short!("a");
    let b = symbol_short!("b");
    let c = symbol_short!("c");

    for id in [&a, &b, &c] {
        h.registry()
            .register_source(&h.admin, id, &h.create_user(), &ProtocolType::Lending);
    }

    // 33.33% each — intentionally uneven for a total of 10.
    let weights: Vec<AllocationWeight> = vec![
        &h.env,
        AllocationWeight {
            source_id: a.clone(),
            weight_bps: 3_334,
        },
        AllocationWeight {
            source_id: b.clone(),
            weight_bps: 3_333,
        },
        AllocationWeight {
            source_id: c.clone(),
            weight_bps: 3_333,
        },
    ];
    h.strategy().set_weights(&h.admin, &weights);

    let total = 10_i128;
    let allocations = h.strategy().calculate_allocation(&total);
    let sum: i128 = allocations.iter().map(|(_, a)| a).sum();
    assert_eq!(sum, total, "remainder must be fully assigned");
}

// ---------------------------------------------------------------------------
// Scenario 4 — Access control flow
// ---------------------------------------------------------------------------

/// Admin grants the Operator role to a new address; that operator can then
/// call `set_weights` on the strategy.
#[test]
fn admin_can_grant_operator_who_can_set_weights() {
    let h = NesterHarness::setup();

    let operator = h.create_user();
    let aave = symbol_short!("aave");

    h.registry()
        .register_source(&h.admin, &aave, &h.create_user(), &ProtocolType::Lending);
    h.strategy()
        .grant_role(&h.admin, &operator, &Role::Operator);

    let weights: Vec<AllocationWeight> = vec![
        &h.env,
        AllocationWeight {
            source_id: aave,
            weight_bps: 10_000,
        },
    ];
    // Operator should be allowed to set weights.
    h.strategy().set_weights(&operator, &weights);

    assert_eq!(h.strategy().get_weights().len(), 1);
}

/// A non-authorised address must be rejected when attempting to set weights.
#[test]
#[should_panic]
fn non_operator_cannot_set_weights() {
    let h = NesterHarness::setup();

    let outsider = h.create_user();
    let aave = symbol_short!("aave");

    h.registry()
        .register_source(&h.admin, &aave, &h.create_user(), &ProtocolType::Lending);

    let weights: Vec<AllocationWeight> = vec![
        &h.env,
        AllocationWeight {
            source_id: aave,
            weight_bps: 10_000,
        },
    ];
    h.strategy().set_weights(&outsider, &weights); // must panic
}

// ---------------------------------------------------------------------------
// Scenario 5 — Vault pause / unpause
// ---------------------------------------------------------------------------

/// When the vault is paused, `deposit` and `withdraw` must be rejected.
/// When it is unpaused, they succeed again.
#[test]
#[should_panic]
fn deposit_is_rejected_when_vault_is_paused() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.vault().pause(&h.admin);
    assert!(h.vault().is_paused());
    h.vault().deposit(&user, &100, &0); // must panic
}

#[test]
#[should_panic]
fn withdraw_is_rejected_when_vault_is_paused() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.vault().pause(&h.admin);
    h.vault().withdraw(&user, &100, &0); // must panic
}

#[test]
fn vault_accepts_deposit_after_unpause() {
    let h = NesterHarness::setup();
    let user = h.create_user();

    h.vault().pause(&h.admin);
    assert!(h.vault().is_paused());

    h.vault().unpause(&h.admin);
    assert!(!h.vault().is_paused());

    // Mint deposit tokens so the real transfer inside deposit() succeeds.
    h.mint_deposit_tokens(&user, 1_000);
    h.vault().deposit(&user, &100, &0);
}

#[test]
fn non_admin_cannot_pause_vault() {
    use soroban_sdk::testutils::Address as _;

    // Use a fresh environment with no mocked auths so the role check fires.
    let result = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        let env2 = soroban_sdk::Env::default();
        let admin2 = soroban_sdk::Address::generate(&env2);
        let outsider = soroban_sdk::Address::generate(&env2);

        // Bootstrap the vault (needs mock_all_auths for initialize).
        env2.mock_all_auths();
        let token_admin2 = soroban_sdk::Address::generate(&env2);
        let deposit_token2 = env2
            .register_stellar_asset_contract_v2(token_admin2)
            .address();
        let vault_id2 = env2.register_contract(None, vault_contract::VaultContract);
        let treasury2 = soroban_sdk::Address::generate(&env2);
        let vault_token_id2 = env2.register_contract(None, vault_token::VaultTokenContract);
        vault_contract::VaultContractClient::new(&env2, &vault_id2).initialize(
            &admin2,
            &deposit_token2,
            &vault_token_id2,
            &treasury2,
        );
        vault_token::VaultTokenContractClient::new(&env2, &vault_token_id2).initialize(
            &vault_id2,
            &soroban_sdk::String::from_str(&env2, "Nester USDC Vault"),
            &soroban_sdk::String::from_str(&env2, "nUSDC"),
            &7u32,
        );

        // Strip all mocked auths so the role guard runs normally.
        env2.set_auths(&[]);
        vault_contract::VaultContractClient::new(&env2, &vault_id2).pause(&outsider);
    }));

    assert!(
        result.is_err(),
        "non-admin should not be able to pause the vault"
    );
}

// ---------------------------------------------------------------------------
// Scenario 6 — Multi-user VaultToken share accounting
// ---------------------------------------------------------------------------

/// Two users deposit, yield accrues, and both withdraw their correct
/// proportional amounts.  This exercises the VaultToken contract via the
/// harness's `seed_token_balance` helper and validates the share math across
/// multiple actors.
#[test]
fn two_users_receive_proportional_yield_on_withdrawal() {
    let h = NesterHarness::setup();

    let alice = h.create_user();
    let bob = h.create_user();

    // Alice deposits 10_000, Bob deposits 10_000 — equal shares.
    h.seed_token_balance(&alice, 10_000);
    h.seed_token_balance(&bob, 10_000);

    assert_eq!(h.token().total_supply(), 20_000);
    assert_eq!(h.token().total_assets(), 20_000);

    // Simulate 20% yield: total assets grow to 24_000.
    h.token().set_total_assets(&24_000_i128);

    // Each user redeems all their shares.
    let alice_out = h.token().burn_for_withdrawal(&alice, &10_000_i128);
    let bob_out = h.token().burn_for_withdrawal(&bob, &10_000_i128);

    assert_eq!(alice_out, 12_000, "alice should receive 50% of 24_000");
    assert_eq!(
        bob_out, 12_000,
        "bob should receive 50% of remaining assets"
    );
    assert_eq!(h.token().total_supply(), 0);
    assert_eq!(h.token().total_assets(), 0);
}

/// A user who deposits after yield has accrued should not capture prior yield.
#[test]
fn late_depositor_does_not_capture_prior_yield() {
    let h = NesterHarness::setup();

    let alice = h.create_user();
    let bob = h.create_user();

    h.seed_token_balance(&alice, 10_000);
    // Yield: 10_000 → 12_000
    h.token().set_total_assets(&12_000_i128);

    // Bob deposits 12_000 at the post-yield exchange rate: gets 10_000 shares.
    h.seed_token_balance(&bob, 12_000);

    // total_supply=20_000, total_assets=24_000
    assert_eq!(h.token().total_supply(), 20_000);

    let alice_out = h.token().burn_for_withdrawal(&alice, &10_000_i128);
    assert_eq!(alice_out, 12_000, "alice earned the yield");

    let bob_out = h.token().burn_for_withdrawal(&bob, &10_000_i128);
    assert_eq!(bob_out, 12_000, "bob receives exactly what he deposited");
}

// ---------------------------------------------------------------------------
// Scenario 7 — Vault <-> VaultToken integration
// ---------------------------------------------------------------------------

/// Vault deposits must mint vault-token shares, and withdrawals must burn them.
#[test]
fn vault_deposit_and_withdraw_syncs_vault_token_supply() {
    let h = NesterHarness::setup();
    let user = h.create_user();

    h.mint_deposit_tokens(&user, 2_000);
    let user_shares = h.vault().deposit(&user, &1_000, &0);
    assert_eq!(user_shares, 1_000);
    assert_eq!(h.token().balance(&user), 1_000);
    assert_eq!(h.token().total_supply(), 1_000);
    assert_eq!(h.token().total_assets(), 1_000);

    // Partial withdraw burns shares in the vault-token contract.
    let remaining = h.vault().withdraw(&user, &400, &0);
    assert_eq!(remaining, 600);
    assert_eq!(h.token().balance(&user), 600);
    assert_eq!(h.token().total_supply(), 600);
}
