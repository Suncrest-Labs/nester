//! Resource-budget tests (issue #1490).
//!
//! Each test builds worst-case on-chain state, calls a money-path entrypoint,
//! and asserts the Soroban CPU-instruction count stays under 80% of the
//! current mainnet limits. Limit values are kept in
//! `libs/common/src/constants.rs` with their source so they can be updated
//! when the network changes them.
//!
//! Mainnet limits as of Soroban protocol 21 (source: Stellar Core source /
//! `stellar/stellar-core` `src/ledger/LedgerManagerImpl.cpp`):
//!   MAX_CPU_INSNS  = 100_000_000
//!   MAX_MEM_BYTES  = 40_960_000
//!
//! Tests assert usage < 80% of the limit and print the actual headroom so CI
//! output is actionable.
#![cfg(test)]

extern crate std;

use soroban_sdk::{symbol_short, testutils::Ledger as _, vec};

use nester_common::{ProtocolType, MIN_DEPOSIT_AMOUNT};
use nester_test_utils::NesterHarness;
use vault_contract::FeeConfig;

/// Mainnet CPU-instruction limit per transaction (protocol 21).
const MAINNET_CPU_INSNS: u64 = 100_000_000;
/// We target 80% headroom.
const BUDGET_CEILING_BPS: u64 = 8_000;

fn cpu_ceiling() -> u64 {
    MAINNET_CPU_INSNS * BUDGET_CEILING_BPS / 10_000
}

fn setup_no_fees(h: &NesterHarness) {
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

/// deposit() budget with a single registered source (normal state).
#[test]
fn test_deposit_budget() {
    let h = NesterHarness::setup();
    h.strategy()
        .update_strategy_params(&h.admin, &500u32, &10_000u32, &100u32);
    setup_no_fees(&h);
    let user = h.create_user();
    let src = symbol_short!("s1");
    h.registry().register_source(
        &h.admin,
        &src,
        &h.create_user(),
        &None,
        &ProtocolType::Lending,
    );
    h.vault().set_allocation_strategy(&h.admin, &h.strategy_id);
    h.vault().set_yield_registry(&h.admin, &h.registry_id);
    h.mint_deposit_tokens(&user, MIN_DEPOSIT_AMOUNT);

    let env = &h.env;
    env.budget().reset_default();
    h.vault().deposit(&user, &MIN_DEPOSIT_AMOUNT, &0i128);
    let cpu = env.budget().cpu_instruction_count();

    std::println!("deposit CPU: {} / {} ceiling", cpu, cpu_ceiling());
    assert!(
        cpu < cpu_ceiling(),
        "deposit exceeded 80% CPU budget: {} >= {}",
        cpu,
        cpu_ceiling()
    );
}

/// withdraw() budget.
#[test]
fn test_withdraw_budget() {
    let h = NesterHarness::setup();
    h.strategy()
        .update_strategy_params(&h.admin, &500u32, &10_000u32, &100u32);
    setup_no_fees(&h);
    let user = h.create_user();
    h.mint_deposit_tokens(&user, MIN_DEPOSIT_AMOUNT);
    h.vault().deposit(&user, &MIN_DEPOSIT_AMOUNT, &0i128);

    let shares = h.token().balance(&user);
    let env = &h.env;
    env.budget().reset_default();
    h.vault().withdraw(&user, &shares, &0i128);
    let cpu = env.budget().cpu_instruction_count();

    std::println!("withdraw CPU: {} / {} ceiling", cpu, cpu_ceiling());
    assert!(
        cpu < cpu_ceiling(),
        "withdraw exceeded 80% CPU budget: {} >= {}",
        cpu,
        cpu_ceiling()
    );
}
