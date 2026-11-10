//! The adapter driven by the real savings core: allocation through the
//! core's scoped pull, valuation via `position_value`, and withdrawals that
//! convert assets to shares through `max_withdraw` (R10) with the core's
//! 1 bp tolerance.

extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Ledger as _},
    token::{StellarAssetClient, TokenClient},
    Address, Env, String,
};

use nester_test_utils::mocks::{MockDefindexVault, MockDefindexVaultClient};
use savings_core::{Change, SavingsCore, SavingsCoreClient};
use savings_reserve::{SavingsReserve, SavingsReserveClient};

use crate::{DefindexAdapterContract, DefindexAdapterContractClient};

const UNIT: i128 = 10_000_000;

#[test]
fn core_allocates_values_and_withdraws_through_the_adapter() {
    let env = Env::default();
    env.mock_all_auths();

    let usdc_id = env
        .register_stellar_asset_contract_v2(Address::generate(&env))
        .address();
    let usdc = TokenClient::new(&env, &usdc_id);
    let usdc_admin = StellarAssetClient::new(&env, &usdc_id);

    let core_id = env.register_contract(None, SavingsCore);
    let core = SavingsCoreClient::new(&env, &core_id);
    let reserve = env.register_contract(None, SavingsReserve);
    SavingsReserveClient::new(&env, &reserve).initialize(&core_id);
    let owner = Address::generate(&env);
    let allocator = Address::generate(&env);
    core.initialize(
        &owner,
        &usdc_id,
        &reserve,
        &String::from_str(&env, "Nester Savings USDC"),
        &String::from_str(&env, "nsUSDC"),
        &0u64,
    );
    core.set_allocator(&owner, &allocator);

    let vault = MockDefindexVaultClient::new(&env, &env.register_contract(None, MockDefindexVault));
    vault.initialize(&usdc_id);
    let seeder = Address::generate(&env);
    usdc_admin.mint(&seeder, &(1_000 * UNIT));
    vault.deposit(
        &soroban_sdk::vec![&env, 1_000 * UNIT],
        &soroban_sdk::vec![&env, 0],
        &seeder,
        &true,
    );

    let adapter = DefindexAdapterContractClient::new(
        &env,
        &env.register_contract(None, DefindexAdapterContract),
    );
    adapter.initialize(&core_id, &vault.address, &usdc_id);
    let id = core.propose(
        &owner,
        &Change::AddAdapter(adapter.address.clone(), i128::MAX, 10_000),
    );
    env.ledger().with_mut(|l| l.timestamp += core.timelock());
    assert!(core.execute(&id));

    let alice = Address::generate(&env);
    usdc_admin.mint(&alice, &(100 * UNIT));
    core.deposit(&alice, &(100 * UNIT), &0);
    core.allocate(&allocator, &adapter.address, &(100 * UNIT));

    assert_eq!(core.idle(), 0);
    assert_eq!(adapter.max_withdraw(), 100 * UNIT);
    assert_eq!(core.total_assets(), 100 * UNIT);

    // 1,210 assets over 1,100 vault shares: the core's 100 shares are worth 110.
    let donor = Address::generate(&env);
    usdc_admin.mint(&donor, &(110 * UNIT));
    vault.simulate_yield(&donor, &(110 * UNIT));
    assert_eq!(core.total_assets(), 110 * UNIT);

    // Nothing is idle, so the core pulls exactly 33 USDC worth of shares.
    core.withdraw(&alice, &(33 * UNIT), &i128::MAX);
    assert_eq!(usdc.balance(&alice), 33 * UNIT);
    assert_eq!(usdc.balance(&adapter.address), 0);

    // Redeeming the rest drains the position down to the reserve's fee share.
    let rest = core.balance(&alice);
    core.redeem(&alice, &rest, &0);
    let received = usdc.balance(&alice);
    assert!(
        received > 108 * UNIT && received <= 110 * UNIT,
        "received {received}"
    );
    assert!(core.total_assets() <= 2 * UNIT);
}
