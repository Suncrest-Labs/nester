//! Golden-file tests for vault contract events — issue #1496.
//!
//! Each test invokes a state-changing vault entrypoint, captures the emitted
//! event(s), and asserts that the topic symbols match the values documented in
//! `packages/contracts/EVENTS.md`.  A separate `generate_event_fixtures` test
//! serialises representative events to `event_fixtures.json` so the Go indexer
//! tests can assert the same shape.
//!
//! Drift coverage: if a contract refactor renames a topic symbol (e.g. DEPOSIT
//! → DEP) this file fails CI before the Go decoder silently breaks.
#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Events as _, Ledger as _},
    IntoVal, Symbol,
};

use nester_access_control::Role;
use nester_test_utils::NesterHarness;
use vault_contract::FeeConfig;

const DEPOSIT: i128 = 10_000_000;

fn no_fees(h: &NesterHarness) {
    h.vault().set_fee_config(
        &h.admin,
        &FeeConfig {
            performance_fee_bps: 0,
            management_fee_bps: 0,
            early_withdrawal_fee_bps: 0,
            treasury_address: h.treasury_id.clone(),
        },
    );
}

/// Return the last event's topic[0] and topic[1] as `Symbol`s.
fn last_event_topics(h: &NesterHarness) -> (Symbol, Symbol) {
    let events = h.env.events().all();
    let (_, topics, _) = events.last().expect("expected at least one event").clone();
    let t0: Symbol = topics.get(0).unwrap().into_val(&h.env);
    let t1: Symbol = topics.get(1).unwrap().into_val(&h.env);
    (t0, t1)
}

// ── Topic-symbol golden tests ─────────────────────────────────────────────────

/// deposit() emits (VAULT, DEPOSIT, user).
#[test]
fn deposit_event_topics_match_events_md() {
    let h = NesterHarness::setup();
    no_fees(&h);
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let (contract, action) = last_event_topics(&h);
    assert_eq!(contract, Symbol::new(&h.env, "VAULT"), "deposit: topic[0]");
    assert_eq!(action, Symbol::new(&h.env, "DEPOSIT"), "deposit: topic[1]");
}

/// withdraw() emits (VAULT, WITHDRAW, user).
#[test]
fn withdraw_event_topics_match_events_md() {
    let h = NesterHarness::setup();
    no_fees(&h);
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    let shares = h.token().balance(&user);
    h.vault().withdraw(&user, &shares, &0);

    let (contract, action) = last_event_topics(&h);
    assert_eq!(contract, Symbol::new(&h.env, "VAULT"), "withdraw: topic[0]");
    assert_eq!(
        action,
        Symbol::new(&h.env, "WITHDRAW"),
        "withdraw: topic[1]"
    );
}

/// pause() emits (VAULT, PAUSE, caller).
#[test]
fn pause_event_topics_match_events_md() {
    let h = NesterHarness::setup();
    h.vault().pause(&h.admin);

    let (contract, action) = last_event_topics(&h);
    assert_eq!(contract, Symbol::new(&h.env, "VAULT"), "pause: topic[0]");
    assert_eq!(action, Symbol::new(&h.env, "PAUSE"), "pause: topic[1]");
}

/// unpause() emits (VAULT, UNPAUSE, caller).
#[test]
fn unpause_event_topics_match_events_md() {
    let h = NesterHarness::setup();
    h.vault().pause(&h.admin);
    h.vault().unpause(&h.admin);

    let (contract, action) = last_event_topics(&h);
    assert_eq!(contract, Symbol::new(&h.env, "VAULT"), "unpause: topic[0]");
    assert_eq!(action, Symbol::new(&h.env, "UNPAUSE"), "unpause: topic[1]");
}

/// report_yield() emits a YLD_STRT (vesting stream started) event.
#[test]
fn report_yield_emits_yld_strt_event() {
    let h = NesterHarness::setup();
    no_fees(&h);
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.mint_deposit_tokens(&h.vault_id, DEPOSIT / 10);
    h.vault().report_yield(&h.admin, &(DEPOSIT / 10));

    let expected = Symbol::new(&h.env, "YLD_STRT");
    let events = h.env.events().all();
    let has_yld_strt = events.iter().any(|(_, topics, _)| {
        topics.len() >= 2 && {
            let t1: Symbol = topics.get(1).unwrap().into_val(&h.env);
            t1 == expected
        }
    });

    assert!(has_yld_strt, "report_yield must emit a YLD_STRT event");
}

/// harvest() emits (VAULT, HARVEST, user).
#[test]
fn harvest_event_topics_match_events_md() {
    let h = NesterHarness::setup();
    no_fees(&h);
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.mint_deposit_tokens(&h.vault_id, DEPOSIT / 10);
    h.vault().report_yield(&h.admin, &(DEPOSIT / 10));
    h.env
        .ledger()
        .set_timestamp(h.env.ledger().timestamp() + h.vault().get_yield_vesting_period() + 1);
    h.vault().report_yield(&h.admin, &0);
    h.vault().harvest(&user);

    let expected = Symbol::new(&h.env, "HARVEST");
    let events = h.env.events().all();
    let has_harvest = events.iter().any(|(_, topics, _)| {
        topics.len() >= 2 && {
            let t1: Symbol = topics.get(1).unwrap().into_val(&h.env);
            t1 == expected
        }
    });

    assert!(has_harvest, "harvest must emit a HARVEST event");
}

// ── Fixture generation ────────────────────────────────────────────────────────

/// Serialise representative events to JSON for Go/TS decoder alignment.
/// Re-run and commit the updated file after any event schema change.
#[test]
fn generate_event_fixtures() {
    use std::fs;
    use std::path::PathBuf;

    let h = NesterHarness::setup();
    no_fees(&h);
    let user = h.create_user();
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);

    h.vault().grant_role(&h.admin, &h.admin, &Role::Manager);
    h.mint_deposit_tokens(&h.vault_id, DEPOSIT / 10);
    h.vault().report_yield(&h.admin, &(DEPOSIT / 10));
    h.env
        .ledger()
        .set_timestamp(h.env.ledger().timestamp() + h.vault().get_yield_vesting_period() + 1);
    h.vault().report_yield(&h.admin, &0);
    h.vault().harvest(&user);
    h.vault().pause(&h.admin);
    h.vault().unpause(&h.admin);

    // Second deposit + withdraw.
    h.mint_deposit_tokens(&user, DEPOSIT);
    h.vault().deposit(&user, &DEPOSIT, &0);
    let shares = h.token().balance(&user);
    h.vault().withdraw(&user, &shares, &0);

    let events = h.env.events().all();
    let mut entries = std::vec::Vec::new();

    for (contract_id, topics, _data) in events.iter() {
        if topics.len() < 2 {
            continue;
        }
        let t0: Symbol = topics.get(0).unwrap().into_val(&h.env);
        let t1: Symbol = topics.get(1).unwrap().into_val(&h.env);

        // Symbols implement Debug in testutils.
        entries.push(std::format!(
            "{{\"contract\":\"{:?}\",\"topic0\":\"{:?}\",\"topic1\":\"{:?}\"}}",
            contract_id,
            t0,
            t1
        ));
    }

    let json = std::format!("[\n  {}\n]\n", entries.join(",\n  "));

    let manifest = std::env::var("CARGO_MANIFEST_DIR").unwrap_or_else(|_| ".".to_string());
    let out = PathBuf::from(&manifest)
        .join("src")
        .join("integration")
        .join("event_fixtures.json");

    fs::write(&out, &json).expect("failed to write event_fixtures.json");
    std::println!("Wrote {} event entries to {}", entries.len(), out.display());
}
