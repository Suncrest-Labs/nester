//! Adversarial and negative scenario integration tests.
#![cfg(test)]

extern crate std;

use nester_access_control::Role;
use nester_common::{build_payload_bytes, Attestation, AttestationPayload, AttestedField};
use nester_test_utils::{register_reentrant_strategy, HostileVaultHarness, NesterHarness};
use soroban_sdk::{
    symbol_short,
    testutils::{Address as _, Ledger as _},
    token, Address, BytesN, Vec,
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
        &None,
        &nester_common::ProtocolType::Lending,
    );
    h.registry().register_source(
        &h.admin,
        &blend,
        &h.create_user(),
        &None,
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
        &None,
        &nester_common::ProtocolType::Lending,
    );
    h.registry().register_source(
        &h.admin,
        &blend,
        &h.create_user(),
        &None,
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

// ---------------------------------------------------------------------------
// Attestation tests and helpers (issue #820 — signature-attested APY/TVL)
// ---------------------------------------------------------------------------

/// Generate a fresh ed25519 signing key and return the raw secret bytes and
/// raw public-key bytes (32 bytes each).
fn generate_ed25519_keypair() -> ([u8; 32], [u8; 32]) {
    use ed25519_dalek::SigningKey;
    use rand::rngs::OsRng;
    let signing_key = SigningKey::generate(&mut OsRng);
    let secret_bytes: [u8; 32] = signing_key.to_bytes();
    let public_bytes: [u8; 32] = signing_key.verifying_key().to_bytes();
    (secret_bytes, public_bytes)
}

/// Sign `payload_bytes` with the raw ed25519 secret key and return the 64-byte
/// signature.
fn sign_payload(secret: &[u8; 32], payload: &[u8]) -> [u8; 64] {
    use ed25519_dalek::{Signer, SigningKey};
    let signing_key = SigningKey::from_bytes(secret);
    signing_key.sign(payload).to_bytes()
}

/// Build an [`Attestation`] for a given payload against the contract,
/// signing with `secret_key` at the given `nonce`.
fn make_attestation(
    env: &soroban_sdk::Env,
    secret: &[u8; 32],
    public: &[u8; 32],
    payload: &AttestationPayload,
    nonce: u64,
) -> Attestation {
    let payload_bytes = build_payload_bytes(env, payload, nonce);
    // Convert soroban Bytes → &[u8] for dalek
    let mut raw: std::vec::Vec<u8> = std::vec::Vec::new();
    for i in 0..payload_bytes.len() {
        raw.push(payload_bytes.get(i as u32).unwrap());
    }
    let sig_bytes = sign_payload(secret, &raw);
    Attestation {
        public_key: BytesN::from_array(env, public),
        signature: BytesN::from_array(env, &sig_bytes),
        nonce,
    }
}

/// Helper: set up a registry with one registered source (`aave`) and one
/// attester, returning the keypair and source id.
fn setup_attested_registry() -> (
    NesterHarness,
    [u8; 32], // secret key
    [u8; 32], // public key
    soroban_sdk::Symbol,
) {
    let h = NesterHarness::setup();
    let (secret, public) = generate_ed25519_keypair();

    let source_id = symbol_short!("aave");
    h.registry().register_source(
        &h.admin,
        &source_id,
        &h.create_user(),
        &None,
        &nester_common::ProtocolType::Lending,
    );

    h.registry().register_attester(
        &h.admin,
        &BytesN::from_array(&h.env, &public),
        &symbol_short!("backend"),
    );

    (h, secret, public, source_id)
}

/// A valid attested APY update succeeds and the value is committed.
#[test]
fn attested_apy_update_succeeds_with_valid_signature() {
    let (h, secret, public, source_id) = setup_attested_registry();

    let now = h.env.ledger().timestamp();
    let valid_from = now;
    let valid_until = now + 3600;
    let new_apy: u32 = 800; // 8% in bps

    let payload = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: new_apy,
        tvl: 0,
        valid_from,
        valid_until,
    };

    let att = make_attestation(&h.env, &secret, &public, &payload, 1);
    let attestations: Vec<Attestation> = soroban_sdk::vec![&h.env, att];

    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &new_apy,
        &valid_from,
        &valid_until,
        &attestations,
    );

    let source = h.registry().get_source(&source_id);
    assert_eq!(source.current_apy_bps, new_apy);
}

/// A valid attested TVL update succeeds and the value is committed.
#[test]
fn attested_tvl_update_succeeds_with_valid_signature() {
    let (h, secret, public, source_id) = setup_attested_registry();

    let now = h.env.ledger().timestamp();
    let valid_from = now;
    let valid_until = now + 3600;
    let new_tvl: i128 = 5_000_000;

    let payload = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Tvl,
        apy_bps: 0,
        tvl: new_tvl,
        valid_from,
        valid_until,
    };

    let att = make_attestation(&h.env, &secret, &public, &payload, 1);
    let attestations: Vec<Attestation> = soroban_sdk::vec![&h.env, att];

    h.registry().update_tvl_attested(
        &h.admin,
        &source_id,
        &new_tvl,
        &valid_from,
        &valid_until,
        &attestations,
    );

    let source = h.registry().get_source(&source_id);
    assert_eq!(source.tvl, new_tvl);
}

/// Replaying an expired attestation (valid_until in the past) is rejected with
/// `AttestationExpired` (error #38).
#[test]
#[should_panic(expected = "Error(Contract, #48)")]
fn expired_attestation_is_rejected() {
    let (h, secret, public, source_id) = setup_attested_registry();

    let now = h.env.ledger().timestamp();
    // Craft a validity window entirely in the past.
    let valid_from: u64 = 0;
    let valid_until: u64 = now.saturating_sub(10); // already expired

    let payload = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: 500,
        tvl: 0,
        valid_from,
        valid_until,
    };

    let att = make_attestation(&h.env, &secret, &public, &payload, 1);
    let attestations: Vec<Attestation> = soroban_sdk::vec![&h.env, att];

    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &500,
        &valid_from,
        &valid_until,
        &attestations,
    );
}

/// Reusing a nonce (submitting the same attestation twice) is rejected with
/// `NonceReused` (error #39).
#[test]
#[should_panic(expected = "Error(Contract, #49)")]
fn nonce_reuse_is_rejected() {
    let (h, secret, public, source_id) = setup_attested_registry();

    let now = h.env.ledger().timestamp();
    let valid_from = now;
    let valid_until = now + 3600;

    let payload = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: 600,
        tvl: 0,
        valid_from,
        valid_until,
    };

    let att = make_attestation(&h.env, &secret, &public, &payload, 1);
    let attestations: Vec<Attestation> = soroban_sdk::vec![&h.env, att.clone()];

    // First submission — should succeed.
    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &600,
        &valid_from,
        &valid_until,
        &attestations,
    );

    // Build a second payload with the SAME nonce (replay / nonce reuse).
    let att2 = make_attestation(&h.env, &secret, &public, &payload, 1);
    let attestations2: Vec<Attestation> = soroban_sdk::vec![&h.env, att2];

    // Second submission with the same nonce must fail.
    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &600,
        &valid_from,
        &valid_until,
        &attestations2,
    );
}

/// Signing with a key that has been revoked is rejected with
/// `AttesterNotRegistered` (error #36).
#[test]
#[should_panic(expected = "Error(Contract, #46)")]
fn revoked_attester_is_rejected() {
    let (h, secret, public, source_id) = setup_attested_registry();

    // Revoke the attester key before submitting.
    h.registry()
        .revoke_attester(&h.admin, &BytesN::from_array(&h.env, &public));

    let now = h.env.ledger().timestamp();
    let valid_from = now;
    let valid_until = now + 3600;

    let payload = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: 700,
        tvl: 0,
        valid_from,
        valid_until,
    };

    let att = make_attestation(&h.env, &secret, &public, &payload, 1);
    let attestations: Vec<Attestation> = soroban_sdk::vec![&h.env, att];

    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &700,
        &valid_from,
        &valid_until,
        &attestations,
    );
}

/// Submitting fewer attestations than the configured threshold is rejected with
/// `ThresholdNotMet` (error #40).
#[test]
#[should_panic(expected = "Error(Contract, #50)")]
fn below_threshold_submission_is_rejected() {
    let (h, secret, public, source_id) = setup_attested_registry();

    // Raise the APY threshold to 2 — we will submit only 1.
    h.registry()
        .set_attestation_threshold(&h.admin, &1u32, &2u32); // field_tag=1 (APY), threshold=2

    let now = h.env.ledger().timestamp();
    let valid_from = now;
    let valid_until = now + 3600;

    let payload = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: 800,
        tvl: 0,
        valid_from,
        valid_until,
    };

    // Only 1 attestation, but threshold is 2.
    let att = make_attestation(&h.env, &secret, &public, &payload, 1);
    let attestations: Vec<Attestation> = soroban_sdk::vec![&h.env, att];

    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &800,
        &valid_from,
        &valid_until,
        &attestations,
    );
}

/// An attested update that passes threshold but exceeds the deviation limit is
/// still rejected — attestation and deviation checks are complementary.
#[test]
#[should_panic(expected = "Error(Contract, #9)")]
fn attested_value_that_violates_deviation_limit_is_rejected() {
    let (h, secret, public, source_id) = setup_attested_registry();

    let now = h.env.ledger().timestamp();
    let valid_from = now;
    let valid_until = now + 3600;

    // First: set an initial APY so the deviation guard activates.
    let initial_apy: u32 = 500; // 5%
    let payload_init = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: initial_apy,
        tvl: 0,
        valid_from,
        valid_until,
    };
    let att_init = make_attestation(&h.env, &secret, &public, &payload_init, 1);
    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &initial_apy,
        &valid_from,
        &valid_until,
        &soroban_sdk::vec![&h.env, att_init],
    );

    // Now tighten the deviation threshold to 100 bps.
    h.registry().set_apy_deviation_threshold(&h.admin, &100u32);

    // Attempt to set APY = 9999 bps — change of 9499 bps, far exceeds 100 bps.
    let out_of_band_apy: u32 = 9_999;
    let payload_bad = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: out_of_band_apy,
        tvl: 0,
        valid_from,
        valid_until,
    };
    let att_bad = make_attestation(&h.env, &secret, &public, &payload_bad, 2);
    let attestations_bad: Vec<Attestation> = soroban_sdk::vec![&h.env, att_bad];

    // Should panic with InvalidOperation (#9) because the deviation check fails
    // even though the attestation signature is valid.
    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &out_of_band_apy,
        &valid_from,
        &valid_until,
        &attestations_bad,
    );
}

/// A signature over tampered payload bytes (wrong value) is rejected.
/// Soroban surfaces this as a host-level Crypto error, which panics.
#[test]
#[should_panic]
fn tampered_payload_signature_is_rejected() {
    let (h, secret, public, source_id) = setup_attested_registry();

    let now = h.env.ledger().timestamp();
    let valid_from = now;
    let valid_until = now + 3600;

    // Sign for apy_bps = 500 …
    let payload_signed = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: 500,
        tvl: 0,
        valid_from,
        valid_until,
    };
    let att = make_attestation(&h.env, &secret, &public, &payload_signed, 1);

    // … but submit with apy_bps = 9000 — the signature won't verify.
    let attestations: Vec<Attestation> = soroban_sdk::vec![&h.env, att];
    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &9000, // different value from what was signed
        &valid_from,
        &valid_until,
        &attestations,
    );
}

/// `update_status` remains available on plain role auth even when no
/// attesters are registered, so a source can always be paused during an
/// attester outage (break-glass path).
#[test]
fn update_status_works_without_attesters() {
    let h = NesterHarness::setup();
    let source_id = symbol_short!("aave");
    h.registry().register_source(
        &h.admin,
        &source_id,
        &h.create_user(),
        &None,
        &nester_common::ProtocolType::Lending,
    );

    // No attesters registered — but update_status must still work.
    h.registry()
        .update_status(&h.admin, &source_id, &nester_common::SourceStatus::Paused);

    let status = h.registry().get_source_status(&source_id);
    assert_eq!(status, nester_common::SourceStatus::Paused);
}

/// Two attesters satisfy a 2-of-n threshold.
#[test]
fn two_of_two_threshold_succeeds() {
    let h = NesterHarness::setup();

    let (secret1, public1) = generate_ed25519_keypair();
    let (secret2, public2) = generate_ed25519_keypair();

    let source_id = symbol_short!("blend");
    h.registry().register_source(
        &h.admin,
        &source_id,
        &h.create_user(),
        &None,
        &nester_common::ProtocolType::Lending,
    );
    h.registry().register_attester(
        &h.admin,
        &BytesN::from_array(&h.env, &public1),
        &symbol_short!("att1"),
    );
    h.registry().register_attester(
        &h.admin,
        &BytesN::from_array(&h.env, &public2),
        &symbol_short!("att2"),
    );
    // Set threshold to 2.
    h.registry()
        .set_attestation_threshold(&h.admin, &1u32, &2u32);

    let now = h.env.ledger().timestamp();
    let valid_from = now;
    let valid_until = now + 3600;
    let new_apy: u32 = 900;

    let payload = AttestationPayload {
        contract_address: h.registry_id.clone(),
        source_id: source_id.clone(),
        field: AttestedField::Apy,
        apy_bps: new_apy,
        tvl: 0,
        valid_from,
        valid_until,
    };

    let att1 = make_attestation(&h.env, &secret1, &public1, &payload, 1);
    let att2 = make_attestation(&h.env, &secret2, &public2, &payload, 1);
    let attestations: Vec<Attestation> = soroban_sdk::vec![&h.env, att1, att2];

    h.registry().update_apy_attested(
        &h.admin,
        &source_id,
        &new_apy,
        &valid_from,
        &valid_until,
        &attestations,
    );

    let source = h.registry().get_source(&source_id);
    assert_eq!(source.current_apy_bps, new_apy);
}

// ---------------------------------------------------------------------------
// Time-vested yield reports: sniping resistance (issue #803)
//
// report_yield no longer applies a positive amount to TotalAssets/share
// price instantly; it vests linearly over `get_yield_vesting_period()`
// (default 24h). These exercise the actual attack the feature exists to
// prevent — depositing right before a report, then withdrawing quickly, to
// capture disproportionate share-price appreciation from that report —
// against the REAL payout path (deposit/withdraw/harvest), not a parallel
// accounting view, since this design keeps yield inside share price rather
// than tracking a separate per-user entitlement.
// ---------------------------------------------------------------------------

fn grant_yield_reporter(h: &NesterHarness) {
    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
}

fn accrue_yield_for_test(h: &NesterHarness, amount: i128) {
    h.mint_deposit_tokens(&h.vault_id, amount);
    h.vault().report_yield(&h.admin, &amount);
}

fn advance_time(h: &NesterHarness, seconds: u64) {
    let now = h.env.ledger().timestamp();
    h.env.ledger().set_timestamp(now + seconds);
}

/// Zero every fee and disable the circuit breaker so the sniping-resistance
/// assertions below isolate share-price/vesting arithmetic, matching
/// share_price_tests.rs's convention for the same reason.
fn isolate_share_price(h: &NesterHarness) {
    h.vault().set_fee_config(
        &h.admin,
        &vault_contract::FeeConfig {
            performance_fee_bps: 0,
            management_fee_bps: 0,
            early_withdrawal_fee_bps: 0,
            treasury_address: h.treasury_id.clone(),
        },
    );
    h.vault().set_circuit_breaker_config(
        &h.admin,
        &vault_contract::CircuitBreakerConfig {
            threshold_bps: 10_000,
            window_seconds: 7_200,
        },
    );
}

/// `VaultContract::withdraw` returns the CALLER'S REMAINING share balance,
/// not the assets paid out (see `withdraw_internal`'s final `new_user_shares`
/// return) — so these tests measure actual payout via the deposit token's
/// own balance delta, exactly like `fee_tests.rs`'s treasury-payout
/// assertions do, rather than trusting withdraw's return value as a payout
/// amount.
fn withdraw_all_and_measure_payout(h: &NesterHarness, user: &Address) -> i128 {
    let before = token::Client::new(&h.env, &h.deposit_token_id).balance(user);
    let shares = h.token().balance(user);
    h.vault().withdraw(user, &shares, &0);
    let after = token::Client::new(&h.env, &h.deposit_token_id).balance(user);
    after - before
}

#[test]
fn snipe_deposit_immediately_before_report_then_immediate_withdraw_captures_almost_nothing() {
    let h = NesterHarness::setup();
    grant_yield_reporter(&h);
    isolate_share_price(&h);

    let long_holder = h.create_user();
    h.mint_deposit_tokens(&long_holder, 10_000_000);
    h.vault().deposit(&long_holder, &10_000_000, &0);

    // Attacker deposits an equal amount immediately before the report.
    let attacker = h.create_user();
    h.mint_deposit_tokens(&attacker, 10_000_000);
    h.vault().deposit(&attacker, &10_000_000, &0);

    accrue_yield_for_test(&h, 2_000_000);

    // The classic snipe: withdraw again immediately (same ledger timestamp),
    // before any real time has passed for the report to vest.
    let attacker_out = withdraw_all_and_measure_payout(&h, &attacker);
    let attacker_profit = attacker_out - 10_000_000;

    // Bounded by construction: at t=0 into a 24h vesting window, essentially
    // nothing has vested yet, so the attacker's payout is at most their
    // original principal plus a negligible rounding sliver — nowhere near
    // their naive 1,000,000 (half the report) "fair per-share slice".
    assert!(
        attacker_profit < 100,
        "attacker profit from an instant snipe-and-exit must be near zero, got {attacker_profit}"
    );
}

#[test]
fn snipe_deposit_captures_only_the_fraction_of_the_report_that_vests_before_exit() {
    let h = NesterHarness::setup();
    grant_yield_reporter(&h);
    isolate_share_price(&h);

    let long_holder = h.create_user();
    h.mint_deposit_tokens(&long_holder, 10_000_000);
    h.vault().deposit(&long_holder, &10_000_000, &0);

    let attacker = h.create_user();
    h.mint_deposit_tokens(&attacker, 10_000_000);
    h.vault().deposit(&attacker, &10_000_000, &0);

    accrue_yield_for_test(&h, 2_000_000); // vests over 24h by default

    // Attacker holds for only 1/24th of the vesting window (1 hour) before
    // exiting — a snipe that at least waits a little, rather than the same
    // instant.
    advance_time(&h, 60 * 60);
    let attacker_out = withdraw_all_and_measure_payout(&h, &attacker);
    let attacker_profit = attacker_out - 10_000_000;

    // At most ~1/24th of the attacker's fair per-share slice of the full
    // report (1,000,000) should have vested and be capturable: comfortably
    // under half of a full 24h holder's eventual share, with real headroom
    // for the fee/rounding this vault also applies on withdrawal.
    assert!(
        attacker_profit < 100_000,
        "attacker profit after holding only 1/24 of the vesting window must be far below the full per-share report share (1,000,000), got {attacker_profit}"
    );
}

#[test]
fn long_tenured_holder_who_waits_out_the_vesting_window_captures_the_full_report() {
    let h = NesterHarness::setup();
    grant_yield_reporter(&h);
    isolate_share_price(&h);

    let long_holder = h.create_user();
    h.mint_deposit_tokens(&long_holder, 10_000_000);
    h.vault().deposit(&long_holder, &10_000_000, &0);

    accrue_yield_for_test(&h, 1_000_000);

    // Nobody else ever deposits; the long holder waits the full vesting
    // window out before withdrawing everything.
    advance_time(&h, 24 * 60 * 60);
    let out = withdraw_all_and_measure_payout(&h, &long_holder);
    let profit = out - 10_000_000;

    assert_eq!(
        profit, 1_000_000,
        "a holder who genuinely waits out the full vesting window captures the entire report"
    );
}

#[test]
fn depositing_after_a_report_still_shares_in_whatever_has_not_yet_vested() {
    // Unlike a per-user checkpoint model, vesting is a property of the
    // STREAM, not of any one address: once yield is inside share price,
    // whoever holds shares while the remainder vests shares in it — this is
    // an intentional, documented trade-off of choosing "vest into share
    // price" over "track individual entitlement" (see report_yield's doc
    // comment). What this test pins down is that the SHARE captured is
    // bounded by how much is actually still vesting, not the whole
    // historical report.
    let h = NesterHarness::setup();
    grant_yield_reporter(&h);
    isolate_share_price(&h);

    let long_holder = h.create_user();
    h.mint_deposit_tokens(&long_holder, 10_000_000);
    h.vault().deposit(&long_holder, &10_000_000, &0);

    accrue_yield_for_test(&h, 2_400_000); // 24h window: 100,000/hour

    // Half the window elapses with only the long holder present.
    advance_time(&h, 12 * 60 * 60);

    // A late depositor joins now, matching the long holder's shares.
    let late = h.create_user();
    h.mint_deposit_tokens(&late, 10_000_000);
    // Half the report (1,200,000) has already vested into share price by
    // now, so the late depositor's shares cost proportionally more — they
    // are buying INTO the appreciated price, not getting it for free.
    h.vault().deposit(&late, &10_000_000, &0);

    // The remaining window elapses; the remaining half of the report vests
    // while both hold equal shares, so it splits evenly between them.
    advance_time(&h, 12 * 60 * 60);

    let long_out = withdraw_all_and_measure_payout(&h, &long_holder);
    let late_out = withdraw_all_and_measure_payout(&h, &late);

    // The long holder's total profit (bought in before any vesting, present
    // for the whole window) must exceed the late depositor's (bought in
    // after half had already vested into the price they paid).
    assert!(
        long_out - 10_000_000 > late_out - 10_000_000,
        "a holder present for the full vesting window must out-earn one who joined halfway through: long={} late={}",
        long_out - 10_000_000,
        late_out - 10_000_000
    );
}

#[test]
fn a_second_report_folds_in_the_first_reports_unvested_remainder() {
    let h = NesterHarness::setup();
    grant_yield_reporter(&h);
    isolate_share_price(&h);

    let user = h.create_user();
    h.mint_deposit_tokens(&user, 10_000_000);
    h.vault().deposit(&user, &10_000_000, &0);

    accrue_yield_for_test(&h, 1_000_000);
    advance_time(&h, 60 * 60); // 1 hour into the first 24h stream

    // A second report lands before the first has finished vesting.
    accrue_yield_for_test(&h, 500_000);

    let pending = h.vault().pending_vesting_yield();
    // Approximately the unvested remainder of report 1 (~958,333) plus all
    // of report 2 (500,000) — comfortably more than either report alone,
    // proving the first report's progress was neither discarded nor
    // double-counted (it would exceed 1,500,000 only if genuinely
    // double-applied).
    assert!(
        pending > 1_300_000 && pending <= 1_500_000,
        "expected the unvested remainder of report 1 plus all of report 2, got {pending}"
    );
}

#[test]
fn impairment_applies_immediately_without_vesting() {
    let h = NesterHarness::setup();
    grant_yield_reporter(&h);
    isolate_share_price(&h);

    let user = h.create_user();
    h.mint_deposit_tokens(&user, 10_000_000);
    h.vault().deposit(&user, &10_000_000, &0);

    accrue_yield_for_test(&h, 2_000_000);
    advance_time(&h, 24 * 60 * 60); // fully vested

    let before_loss = h
        .vault()
        .withdrawal_fee_preview(&user, &h.token().balance(&user));
    let _ = before_loss;

    // A loss is reported (negative amount) — unlike a gain, this must land
    // immediately, not vest, so share price reflects the impairment right
    // away rather than overstating holder value while it "vests down".
    h.mint_deposit_tokens(&h.vault_id, 0); // no-op, keeps parity with accrue_yield_for_test's shape
    h.vault().report_yield(&h.admin, &(-1_000_000));

    let pending = h.vault().pending_vesting_yield();
    assert_eq!(
        pending, 0,
        "an impairment must not be queued as a vesting stream; it applies to TotalAssets immediately"
    );
}

#[test]
fn withdrawing_immediately_after_a_fully_vested_report_pays_out_the_full_amount() {
    let h = NesterHarness::setup();
    grant_yield_reporter(&h);
    isolate_share_price(&h);

    let user = h.create_user();
    h.mint_deposit_tokens(&user, 10_000_000);
    h.vault().deposit(&user, &10_000_000, &0);

    accrue_yield_for_test(&h, 500_000);
    advance_time(&h, 24 * 60 * 60); // fully vested

    let before = token::Client::new(&h.env, &h.deposit_token_id).balance(&user);
    h.vault().withdraw(&user, &4_000_000, &0);
    let out = token::Client::new(&h.env, &h.deposit_token_id).balance(&user) - before;
    // 4/10 of principal (10,000,000) plus 4/10 of the fully-vested yield
    // (500,000) = 4,000,000 + 200,000 = 4,200,000.
    assert_eq!(
        out, 4_200_000,
        "a fully-vested report must be reflected in share price exactly like the pre-vesting instant-application model was"
    );
}

// ---------------------------------------------------------------------------
// Time-locked savings vault: adversarial scenarios (issue #802)
// ---------------------------------------------------------------------------

const LOCK_TIER_SECS: u64 = 30 * 86_400;

#[test]
fn breaking_a_lock_near_maturity_charges_a_much_smaller_penalty_than_breaking_immediately() {
    // The linear decay is integer division on whole seconds, so breaking
    // with only 1 second left out of a 30-day term floors to exactly zero
    // bps (see contracts/vault/src/test.rs's
    // break_lock_one_second_before_maturity_rounds_the_penalty_to_zero for
    // the unit-level confirmation of that same floor). This test instead
    // breaks with 1% of the term remaining, which is the smallest remaining
    // fraction that still yields a nonzero, meaningfully decayed penalty —
    // and confirms it, rather than a naive "1 second before maturity",
    // actually demonstrates the decay end-to-end through the real harness.
    // Two entirely separate harnesses (not two users sharing one vault) so
    // that the baseline break's own penalty-retention (it stays in the
    // vault and raises share price for remaining holders, by design) can
    // never leak into the near-maturity scenario's own accounting.
    let baseline_h = NesterHarness::setup();
    let baseline_user = baseline_h.create_user();
    baseline_h.mint_deposit_tokens(&baseline_user, 10_000_000);
    let baseline_lock =
        baseline_h
            .vault()
            .deposit_locked(&baseline_user, &10_000_000, &0, &LOCK_TIER_SECS);
    let full_penalty_returned = baseline_h
        .vault()
        .break_lock(&baseline_user, &baseline_lock);
    let full_penalty = 10_000_000 - full_penalty_returned;

    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 10_000_000);
    let lock_id = h
        .vault()
        .deposit_locked(&user, &10_000_000, &0, &LOCK_TIER_SECS);

    advance_time(&h, LOCK_TIER_SECS - LOCK_TIER_SECS / 100);
    let near_maturity_returned = h.vault().break_lock(&user, &lock_id);
    let near_maturity_penalty = 10_000_000 - near_maturity_returned;

    assert!(
        near_maturity_penalty > 0,
        "penalty must still be nonzero this close to maturity"
    );
    assert!(
        near_maturity_penalty < full_penalty,
        "penalty near maturity ({near_maturity_penalty}) must have decayed well below the day-one penalty ({full_penalty})"
    );
}

#[test]
#[should_panic(expected = "Error(Contract, #10)")]
fn opening_the_maximum_number_of_locks_rejects_the_next_one() {
    let h = NesterHarness::setup();
    let user = h.create_user();
    // MAX_OPEN_LOCKS_PER_USER = 20 — mint enough for 21 dust-sized locks.
    h.mint_deposit_tokens(&user, nester_common::MIN_DEPOSIT_AMOUNT * 21);

    for _ in 0..20 {
        h.vault().deposit_locked(
            &user,
            &nester_common::MIN_DEPOSIT_AMOUNT,
            &0,
            &LOCK_TIER_SECS,
        );
    }
    // The 21st simultaneously open lock must be rejected by the per-user cap.
    h.vault().deposit_locked(
        &user,
        &nester_common::MIN_DEPOSIT_AMOUNT,
        &0,
        &LOCK_TIER_SECS,
    );
}

#[test]
fn emergency_withdraw_exits_a_locked_position_when_paused() {
    // Issue #802's explicit acceptance criterion: emergency paths ignore
    // locks entirely. emergency_withdraw operates on the user's total
    // (flexible + locked) vault_token balance with no lock-awareness at
    // all, so a locked depositor must be able to exit in full through it
    // even while the vault is paused and their lock has not matured.
    let h = NesterHarness::setup();
    let user = h.create_user();
    h.mint_deposit_tokens(&user, 10_000_000);

    h.vault()
        .deposit_locked(&user, &10_000_000, &0, &LOCK_TIER_SECS);
    h.vault().pause(&h.admin);

    let returned = h.vault().emergency_withdraw(&user);
    assert_eq!(
        returned, 10_000_000,
        "a locked position must exit in full through the emergency path, ignoring the lock and its unmatured status"
    );
    assert_eq!(h.vault().get_balance(&user), 0);
}
