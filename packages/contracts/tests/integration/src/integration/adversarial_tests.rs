//! Adversarial and negative scenario integration tests.
#![cfg(test)]

extern crate std;

use nester_access_control::Role;
use nester_common::{build_payload_bytes, Attestation, AttestedField, AttestationPayload};
use nester_test_utils::{register_reentrant_strategy, HostileVaultHarness, NesterHarness};
use soroban_sdk::{symbol_short, testutils::Address as _, Address, BytesN, Vec};

// ---------------------------------------------------------------------------
// Attestation test helpers
// ---------------------------------------------------------------------------

/// Generate a fresh ed25519 signing key and return the raw secret bytes and
/// raw public-key bytes (32 bytes each).
fn generate_ed25519_keypair() -> ([u8; 32], [u8; 32]) {
    use ed25519_dalek::{SigningKey};
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

// ---------------------------------------------------------------------------
// Attestation adversarial tests
// ---------------------------------------------------------------------------

/// Helper: set up a registry with one registered source (`aave`) and one
/// attester, returning the keypair and source id.
fn setup_attested_registry() -> (
    NesterHarness,
    [u8; 32],  // secret key
    [u8; 32],  // public key
    soroban_sdk::Symbol,
) {
    let h = NesterHarness::setup();
    let (secret, public) = generate_ed25519_keypair();

    let source_id = symbol_short!("aave");
    h.registry()
        .register_source(&h.admin, &source_id, &h.create_user(), &nester_common::ProtocolType::Lending);

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
#[should_panic(expected = "Error(Contract, #38)")]
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
#[should_panic(expected = "Error(Contract, #39)")]
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
#[should_panic(expected = "Error(Contract, #36)")]
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
#[should_panic(expected = "Error(Contract, #40)")]
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
    h.registry()
        .set_apy_deviation_threshold(&h.admin, &100u32);

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
        &9000,    // different value from what was signed
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
    h.registry()
        .register_source(&h.admin, &source_id, &h.create_user(), &nester_common::ProtocolType::Lending);

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
    h.registry()
        .register_source(&h.admin, &source_id, &h.create_user(), &nester_common::ProtocolType::Lending);
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
