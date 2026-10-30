//! End-to-end savings lifecycle: the real core, reserve, Blend adapter and
//! DeFindex adapter, wired to the mock Blend pool and mock DeFindex vault.
#![cfg(test)]

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Ledger as _},
    token::{StellarAssetClient, TokenClient},
    vec, Address, Env, String,
};

use adapter_blend::{BlendAdapterContract, BlendAdapterContractClient};
use adapter_defindex::{DefindexAdapterContract, DefindexAdapterContractClient};
use nester_test_utils::mocks::{
    MockBlendPool, MockBlendPoolClient, MockDefindexVault, MockDefindexVaultClient,
};
use savings_core::{Change, SavingsCore, SavingsCoreClient};
use savings_reserve::{SavingsReserve, SavingsReserveClient};

const UNIT: i128 = 10_000_000;
const TIMELOCK: u64 = 3_600;
const BURN_VESTING: u64 = 604_800;
const RESERVE_INDEX: u32 = 0;

fn advance(env: &Env, seconds: u64) {
    env.ledger().with_mut(|l| l.timestamp += seconds);
}

#[test]
fn test_savings_lifecycle() {
    let env = Env::default();
    env.mock_all_auths();

    let usdc_id = env
        .register_stellar_asset_contract_v2(Address::generate(&env))
        .address();
    let usdc = TokenClient::new(&env, &usdc_id);
    let usdc_admin = StellarAssetClient::new(&env, &usdc_id);

    // Deploy order: core, reserve, reserve(core), core(reserve).
    let core_id = env.register_contract(None, SavingsCore);
    let core = SavingsCoreClient::new(&env, &core_id);
    let reserve_id = env.register_contract(None, SavingsReserve);
    SavingsReserveClient::new(&env, &reserve_id).initialize(&core_id);
    let owner = Address::generate(&env);
    let allocator = Address::generate(&env);
    core.initialize(
        &owner,
        &usdc_id,
        &reserve_id,
        &String::from_str(&env, "Nester Savings USDC"),
        &String::from_str(&env, "nsUSDC"),
        &TIMELOCK,
    );
    core.set_allocator(&owner, &allocator);
    assert_eq!(core.timelock(), TIMELOCK);

    // Venues: a Blend pool and a DeFindex vault that already hold other
    // depositors' money.
    let pool = MockBlendPoolClient::new(&env, &env.register_contract(None, MockBlendPool));
    pool.initialize(&usdc_id, &RESERVE_INDEX);
    let vault = MockDefindexVaultClient::new(&env, &env.register_contract(None, MockDefindexVault));
    vault.initialize(&usdc_id);
    let seeder = Address::generate(&env);
    usdc_admin.mint(&seeder, &(1_000 * UNIT));
    vault.deposit(&vec![&env, 1_000 * UNIT], &vec![&env, 0], &seeder, &true);

    let blend =
        BlendAdapterContractClient::new(&env, &env.register_contract(None, BlendAdapterContract));
    blend.initialize(&core_id, &pool.address, &usdc_id, &RESERVE_INDEX);
    let defindex = DefindexAdapterContractClient::new(
        &env,
        &env.register_contract(None, DefindexAdapterContract),
    );
    defindex.initialize(&core_id, &vault.address, &usdc_id);

    // Both adapters enter through the timelock: 1,000 USDC absolute, 50% relative.
    let add_blend = core.propose(
        &owner,
        &Change::AddAdapter(blend.address.clone(), 1_000 * UNIT, 5_000),
    );
    let add_defindex = core.propose(
        &owner,
        &Change::AddAdapter(defindex.address.clone(), 1_000 * UNIT, 5_000),
    );
    assert_eq!(core.pending_changes().len(), 2);
    assert!(core.try_execute(&add_blend).is_err());
    advance(&env, TIMELOCK);
    assert!(core.execute(&add_blend));
    assert!(core.execute(&add_defindex));
    assert_eq!(core.adapters().len(), 2);

    // Two users deposit 1,000 USDC: first deposit is 1:1, the second pays the
    // same price.
    let alice = Address::generate(&env);
    let bob = Address::generate(&env);
    usdc_admin.mint(&alice, &(600 * UNIT));
    usdc_admin.mint(&bob, &(400 * UNIT));
    assert_eq!(core.deposit(&alice, &(600 * UNIT), &0), 600 * UNIT);
    assert_eq!(core.deposit(&bob, &(400 * UNIT), &0), 400 * UNIT);
    assert_eq!(core.total_assets(), 1_000 * UNIT);
    assert_eq!(core.total_supply(), 1_000 * UNIT);

    // The allocator puts 40% in each venue; 20% stays idle for withdrawals.
    core.allocate(&allocator, &blend.address, &(400 * UNIT));
    core.allocate(&allocator, &defindex.address, &(400 * UNIT));
    assert_eq!(core.idle(), 200 * UNIT);
    assert_eq!(core.allocation(&blend.address), 400 * UNIT);
    assert_eq!(core.allocation(&defindex.address), 400 * UNIT);

    // Yield: Blend's index rises 10%, and 140 USDC lands in a vault holding
    // 1,400 shares of which the adapter owns 400 (1,540 * 400 / 1,400 = 440).
    usdc_admin.mint(&pool.address, &(40 * UNIT));
    pool.set_index_bps(&11_000);
    let donor = Address::generate(&env);
    usdc_admin.mint(&donor, &(140 * UNIT));
    vault.simulate_yield(&donor, &(140 * UNIT));
    assert_eq!(core.allocation(&blend.address), 440 * UNIT);
    assert_eq!(core.allocation(&defindex.address), 440 * UNIT);
    assert_eq!(core.total_assets(), 1_080 * UNIT);

    // accrue charges 10% of the 80 USDC gain, paid in shares at the
    // post-dilution price: f * 1,080 / (1,000 + f) = 8, so f = 8,000 / 1,072.
    // The core's virtual offset moves this by a few stroops.
    let fee_shares = core.accrue();
    let expected_fee_shares = 8 * UNIT * 1_000 * UNIT / (1_072 * UNIT);
    assert!(
        (fee_shares - expected_fee_shares).abs() <= 6,
        "fee shares {fee_shares} vs {expected_fee_shares}"
    );
    assert_eq!(core.reserve_shares(), fee_shares);
    assert_eq!(core.balance(&reserve_id), fee_shares);
    assert_eq!(core.total_supply(), 1_000 * UNIT + fee_shares);
    assert!((core.convert_to_assets(&fee_shares) - 8 * UNIT).abs() <= 2);
    let peak_price = core.share_price();
    assert_eq!(core.high_water_mark(), peak_price);
    assert_eq!(core.accrue(), 0);

    // Loss: Blend's index falls to 0.8, taking its position from 440 to 320.
    pool.set_index_bps(&8_000);
    assert_eq!(core.allocation(&blend.address), 320 * UNIT);
    assert_eq!(core.total_assets(), 960 * UNIT);
    let supply_with_fee = 1_000 * UNIT + fee_shares;
    let loss_price = core.share_price();
    assert!(loss_price < peak_price);
    let expected_loss_price = 960 * UNIT * UNIT / supply_with_fee;
    assert!(
        (loss_price - expected_loss_price).abs() <= 1,
        "price {loss_price} vs {expected_loss_price}"
    );
    // Below the high-water mark: no new fee, and the mark does not drop.
    assert_eq!(core.accrue(), 0);
    assert_eq!(core.high_water_mark(), peak_price);
    assert_eq!(core.reserve_shares(), fee_shares);

    // The owner proposes burning the reserve's shares. Nothing is burned at
    // execution; the burn vests linearly over seven days.
    let burn = core.propose(&owner, &Change::BurnReserve(fee_shares));
    assert!(core.try_execute(&burn).is_err());
    advance(&env, TIMELOCK);
    assert!(core.execute(&burn));
    let start = env.ledger().timestamp();
    assert_eq!(core.reserve_shares(), fee_shares);
    assert_eq!(
        core.burn_schedule(),
        Some((fee_shares, 0, start, start + BURN_VESTING))
    );

    advance(&env, BURN_VESTING / 2);
    core.accrue();
    let half = fee_shares / 2;
    assert!((core.reserve_shares() - (fee_shares - half)).abs() <= 1);

    advance(&env, BURN_VESTING);
    core.accrue();
    assert_eq!(core.reserve_shares(), 0);
    assert_eq!(core.balance(&reserve_id), 0);
    assert_eq!(core.total_supply(), 1_000 * UNIT);
    assert_eq!(core.total_assets(), 960 * UNIT);
    let burned_price = core.share_price();
    assert_eq!(burned_price, 96 * UNIT / 100);
    assert!(burned_price > loss_price);

    // Alice redeems everything. Pro-rata of 960 USDC is exactly 576; the
    // core's virtual offset (V = 10,000 stroops) rounds in the redeemer's
    // favour while the price is below 1:
    // floor(600 * (960 USDC + V) / (1,000 shares + V)).
    let alice_out = core.redeem(&alice, &(600 * UNIT), &0);
    let expected_alice = 600 * UNIT * (960 * UNIT + 10_000) / (1_000 * UNIT + 10_000);
    assert_eq!(alice_out, expected_alice);
    assert!(alice_out >= 576 * UNIT);
    assert_eq!(usdc.balance(&alice), alice_out);

    // Bob, the last holder, redeems all his shares and takes exactly what
    // the core still holds: the conversion is capped at total assets.
    let remaining = core.total_assets();
    assert!((remaining - (960 * UNIT - alice_out)).abs() <= 2);
    let bob_out = core.redeem(&bob, &(400 * UNIT), &0);
    assert_eq!(bob_out, remaining);
    assert_eq!(usdc.balance(&bob), remaining);
    assert!(bob_out >= 384 * UNIT - 300);
    assert_eq!(core.total_assets(), 0);
    assert_eq!(core.total_supply(), 0);
}
