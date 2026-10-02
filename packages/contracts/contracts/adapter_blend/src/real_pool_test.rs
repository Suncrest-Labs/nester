//! Integration test against Blend's actual, unmodified pool contract
//! (nester#1351).
//!
//! `test.rs` exercises this adapter against `MockBlendPool`, a hand-written
//! reimplementation of Blend's `submit`/`Positions` interface built to match
//! this adapter's own assumptions -- it certifies internal consistency, but
//! cannot catch the adapter and the mock silently drifting together away
//! from the real protocol. This module drives the adapter against Blend
//! Capital's official v1.0.0 pool release WASM instead (vendored in
//! `fixtures/`, see that directory's own note on version pinning), deployed
//! and capitalized through the real pool factory, backstop, emitter and
//! Comet LP contracts exactly as a production deployment requires, so a
//! genuine interface or protocol-behavior change on Blend's side shows up
//! here.

#![cfg(test)]
extern crate std;

mod blend_pool {
    soroban_sdk::contractimport!(file = "fixtures/blend_pool.wasm");
}
mod blend_backstop {
    soroban_sdk::contractimport!(file = "fixtures/blend_backstop.wasm");
}
mod blend_pool_factory {
    soroban_sdk::contractimport!(file = "fixtures/blend_pool_factory.wasm");
}
mod blend_emitter {
    soroban_sdk::contractimport!(file = "fixtures/blend_emitter.wasm");
}
mod blend_comet {
    soroban_sdk::contractimport!(file = "fixtures/blend_comet.wasm");
}

use soroban_sdk::{
    testutils::Address as _,
    token::{StellarAssetClient, TokenClient},
    Address, BytesN, Env, String, Vec,
};

use crate::{BlendAdapterContract, BlendAdapterContractClient};

const RESERVE_INDEX: u32 = 0;

struct RealPoolSetup {
    vault: Address,
    adapter: BlendAdapterContractClient<'static>,
    pool: blend_pool::Client<'static>,
    reserve_token: TokenClient<'static>,
    reserve_token_admin: StellarAssetClient<'static>,
}

/// Deploys and fully activates a real Blend v1.0.0 pool: pool factory,
/// emitter, backstop and a Comet LP pool, wired together in the exact order
/// and with the exact calls Blend's own deployment tooling uses (see
/// blend-capital/blend-utils's `deployBlend`/`setupPoolBackstop` reference
/// scripts), then points this adapter at it.
///
/// A bare `register_contract_wasm` for the pool is not enough: the backstop
/// only recognizes pools created through the pool factory, and the pool's
/// `submit`/`set_status` both revert against a backstop that has no
/// capitalization on record for it, which is exactly what deploying outside
/// the factory produces.
fn setup() -> RealPoolSetup {
    let env = Env::default();
    // submit() re-authorizes a token transfer several calls below the
    // top-level invocation (vault -> adapter -> pool -> token.transfer_from),
    // which plain mock_all_auths() rejects as "authorization not tied to the
    // root contract invocation". MockBlendPool's submit calls token.transfer
    // directly with no intermediate require_auth, so test.rs never needed
    // this.
    env.mock_all_auths_allowing_non_root_auth();
    // Five real Blend contracts plus two Stellar asset contracts vastly
    // exceed the default test budget, which is tuned for a handful of small
    // synthetic contracts.
    env.budget().reset_unlimited();

    let admin = Address::generate(&env);
    let oracle = Address::generate(&env);
    let whale = Address::generate(&env);

    // BLND and USDC: the two assets Blend's real backstop LP pool (Comet) is
    // denominated in on every real deployment.
    let blnd_admin = Address::generate(&env);
    let blnd_id = env.register_stellar_asset_contract_v2(blnd_admin).address();
    let blnd_admin_client = StellarAssetClient::new(&env, &blnd_id);

    let usdc_admin = Address::generate(&env);
    let usdc_id = env.register_stellar_asset_contract_v2(usdc_admin).address();
    let usdc_admin_client = StellarAssetClient::new(&env, &usdc_id);

    // The underlying reserve asset this adapter instance will deposit into
    // and withdraw from -- a separate token from BLND/USDC, exactly as in
    // production where a pool's reserves are unrelated to its own backstop
    // capitalization asset.
    let reserve_token_admin = Address::generate(&env);
    let reserve_token_id = env
        .register_stellar_asset_contract_v2(reserve_token_admin)
        .address();

    let pool_wasm_hash: BytesN<32> = env.deployer().upload_contract_wasm(blend_pool::WASM);

    let pool_factory_id = env.register_contract_wasm(None, blend_pool_factory::WASM);
    let pool_factory = blend_pool_factory::Client::new(&env, &pool_factory_id);

    let backstop_id = env.register_contract_wasm(None, blend_backstop::WASM);
    let emitter_id = env.register_contract_wasm(None, blend_emitter::WASM);

    // Comet: the constant-weight-product AMM Blend uses as its backstop LP
    // token. init() pulls its opening balances from the controller account
    // passed as its first argument (not from a balance the comet contract
    // already holds), so BLND/USDC are minted to admin first.
    let comet_id = env.register_contract_wasm(None, blend_comet::WASM);
    let comet = blend_comet::Client::new(&env, &comet_id);

    let admin_blnd = 5_001_000_000_000i128;
    let admin_usdc = 125_010_000_000i128;
    blnd_admin_client.mint(&admin, &admin_blnd);
    usdc_admin_client.mint(&admin, &admin_usdc);

    // 80/20 BLND/USDC, matching Blend's real backstop pool composition.
    // Comet weights are fixed-point fractions of 1_0000000 (7 decimals, the
    // same scalar used throughout this WASM's own i128 fields).
    let tokens = Vec::from_array(&env, [blnd_id.clone(), usdc_id.clone()]);
    let weights = Vec::from_array(&env, [8_000_000i128, 2_000_000i128]);
    let balances = Vec::from_array(&env, [admin_blnd, admin_usdc]);
    comet.init(&admin, &tokens, &weights, &balances, &3000i128);

    pool_factory.initialize(&blend_pool_factory::PoolInitMeta {
        backstop: backstop_id.clone(),
        blnd_id: blnd_id.clone(),
        pool_hash: pool_wasm_hash,
    });

    // The emitter must be initialized before the backstop: backstop.initialize
    // calls emitter.get_last_distro, which traps against an uninitialized
    // emitter. blend-utils's own deployBlend script initializes emitter
    // first for the same reason.
    blend_emitter::Client::new(&env, &emitter_id).initialize(&blnd_id, &backstop_id, &comet_id);

    blend_backstop::Client::new(&env, &backstop_id).initialize(
        &comet_id,
        &emitter_id,
        &usdc_id,
        &blnd_id,
        &pool_factory_id,
        &Vec::new(&env),
    );

    let pool_id = pool_factory.deploy(
        &admin,
        &String::from_str(&env, "test-pool"),
        &BytesN::from_array(&env, &[7u8; 32]),
        &oracle,
        &50_000u32,
        &4u32,
    );
    let pool = blend_pool::Client::new(&env, &pool_id);

    let reserve_config = blend_pool::ReserveConfig {
        c_factor: 900_0000,
        decimals: 7,
        index: 0,
        l_factor: 950_0000,
        max_util: 950_0000,
        r_base: 100_0000,
        r_one: 500_0000,
        r_three: 150_000_000,
        r_two: 1500_0000,
        reactivity: 1000,
        util: 800_0000,
    };
    pool.queue_set_reserve(&reserve_token_id, &reserve_config);
    let reserve_index = pool.set_reserve(&reserve_token_id);
    assert_eq!(
        reserve_index, RESERVE_INDEX,
        "this pool's only reserve is always index 0; a different value here \
         would mean Blend changed how reserve indices are assigned"
    );

    // Capitalize the backstop for this pool: join the Comet pool for LP
    // shares, deposit those shares against pool_id, then let the backstop
    // price its own holdings before the pool's status check reads them
    // (mirrors blend-utils's setupPoolBackstop reference script).
    //
    // Comet's init always mints its fixed opening total supply regardless of
    // the balances passed in, so joining for a further multiple of that
    // supply needs proportionally large token amounts -- requesting a
    // share of the balances already on deposit instead trips Comet's
    // ErrLimitIn slippage guard.
    let lp_amount = comet.get_total_supply() * 10;
    let whale_blnd = 50_010_000_000_000i128;
    let whale_usdc = 1_250_100_000_000i128;
    blnd_admin_client.mint(&whale, &whale_blnd);
    usdc_admin_client.mint(&whale, &whale_usdc);
    comet.join_pool(
        &lp_amount,
        &Vec::from_array(&env, [whale_blnd, whale_usdc]),
        &whale,
    );

    let backstop = blend_backstop::Client::new(&env, &backstop_id);
    backstop.deposit(&whale, &pool_id, &lp_amount);
    backstop.update_tkn_val();

    pool.set_status(&0u32);

    let vault = Address::generate(&env);
    let reserve_token = TokenClient::new(&env, &reserve_token_id);
    let reserve_token_admin_client = StellarAssetClient::new(&env, &reserve_token_id);

    let adapter_id = env.register_contract(None, BlendAdapterContract);
    let adapter = BlendAdapterContractClient::new(&env, &adapter_id);
    adapter.initialize(&vault, &pool_id, &reserve_token_id, &reserve_index);

    RealPoolSetup {
        vault,
        adapter,
        pool,
        reserve_token,
        reserve_token_admin: reserve_token_admin_client,
    }
}

#[test]
fn deposit_and_withdraw_round_trip_against_the_real_pool() {
    let s = setup();
    s.reserve_token_admin.mint(&s.vault, &10_000_000_000);

    let units = s.adapter.deposit(&s.vault, &10_000_000_000, &0);
    assert!(
        units > 0,
        "the real pool must mint bTokens for a supply request"
    );
    assert_eq!(
        s.reserve_token.balance(&s.vault),
        0,
        "the vault's underlying must move to the pool on deposit"
    );

    // The adapter's own bookkeeping must agree with the real pool's
    // Positions map at the reserve index it was configured with -- this is
    // the exact cross-check this adapter's doc comment says the mock cannot
    // provide, since the mock's Positions are populated by the adapter's own
    // assumed shape rather than the real contract's.
    let adapter_id = s.adapter.address.clone();
    let positions = s.pool.get_positions(&adapter_id);
    assert_eq!(
        positions.supply.get(RESERVE_INDEX).unwrap_or(0),
        units,
        "the real pool's own Positions.supply must agree with the units the adapter reports"
    );
    assert_eq!(s.adapter.max_withdraw(), units);

    let assets = s.adapter.withdraw(&s.vault, &units, &0);
    assert!(
        assets > 0,
        "the real pool must return underlying for a withdraw request"
    );
    assert_eq!(
        s.reserve_token.balance(&s.vault),
        assets,
        "withdrawn underlying must reach the vault"
    );
    assert_eq!(s.adapter.max_withdraw(), 0);

    let positions_after = s.pool.get_positions(&adapter_id);
    assert_eq!(
        positions_after.supply.get(RESERVE_INDEX).unwrap_or(0),
        0,
        "the real pool must show the position fully closed after a full withdrawal"
    );
}

#[test]
fn partial_withdrawal_leaves_the_remainder_supplied_in_the_real_pool() {
    let s = setup();
    s.reserve_token_admin.mint(&s.vault, &10_000_000_000);

    let units = s.adapter.deposit(&s.vault, &10_000_000_000, &0);
    let withdrawn = s.adapter.withdraw(&s.vault, &(units / 4), &0);

    assert!(withdrawn > 0);
    assert_eq!(s.adapter.max_withdraw(), units - units / 4);

    let adapter_id = s.adapter.address.clone();
    let positions = s.pool.get_positions(&adapter_id);
    assert_eq!(
        positions.supply.get(RESERVE_INDEX).unwrap_or(0),
        units - units / 4,
        "the real pool must reflect the remaining position after a partial withdrawal"
    );
}

#[test]
#[should_panic(expected = "Error(Contract, #4)")]
fn withdraw_cannot_exceed_the_held_position_in_the_real_pool() {
    let s = setup();
    s.reserve_token_admin.mint(&s.vault, &10_000_000_000);

    let units = s.adapter.deposit(&s.vault, &10_000_000_000, &0);
    s.adapter.withdraw(&s.vault, &(units + 1), &0);
}
