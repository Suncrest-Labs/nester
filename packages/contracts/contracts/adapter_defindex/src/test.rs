extern crate std;

use soroban_sdk::{
    testutils::{Address as _, MockAuth, MockAuthInvoke},
    token::{StellarAssetClient, TokenClient},
    Address, Env, IntoVal,
};

use nester_common::adapters::ApyConfidence;
use nester_test_utils::mocks::{MockDefindexVault, MockDefindexVaultClient};

use crate::{DefindexAdapterContract, DefindexAdapterContractClient};

const UNIT: i128 = 10_000_000;

struct Setup {
    env: Env,
    core: Address,
    adapter: DefindexAdapterContractClient<'static>,
    vault: MockDefindexVaultClient<'static>,
    token: TokenClient<'static>,
    token_admin: StellarAssetClient<'static>,
}

/// A DeFindex vault that already holds 1,000 USDC from another depositor,
/// as a live vault would, so share math starts at 1 share : 1 asset.
fn setup() -> Setup {
    let env = Env::default();
    env.mock_all_auths();

    let core = Address::generate(&env);
    let token_id = env
        .register_stellar_asset_contract_v2(Address::generate(&env))
        .address();
    let token = TokenClient::new(&env, &token_id);
    let token_admin = StellarAssetClient::new(&env, &token_id);

    let vault = MockDefindexVaultClient::new(&env, &env.register_contract(None, MockDefindexVault));
    vault.initialize(&token_id);
    let seeder = Address::generate(&env);
    token_admin.mint(&seeder, &(1_000 * UNIT));
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
    adapter.initialize(&core, &vault.address, &token_id);

    Setup {
        env,
        core,
        adapter,
        vault,
        token,
        token_admin,
    }
}

impl Setup {
    fn fund_core(&self, amount: i128) {
        self.token_admin.mint(&self.core, &amount);
    }

    fn accrue_yield(&self, amount: i128) {
        let donor = Address::generate(&self.env);
        self.token_admin.mint(&donor, &amount);
        self.vault.simulate_yield(&donor, &amount);
    }
}

#[test]
fn test_deposit_and_value_track_vault_shares() {
    let s = setup();
    s.accrue_yield(100 * UNIT);
    s.fund_core(110 * UNIT);

    // 1,100 assets back 1,000 shares, so 110 assets mint 100 shares.
    let units = s.adapter.deposit(&s.core, &(110 * UNIT), &0);

    assert_eq!(units, 100 * UNIT);
    assert_eq!(s.vault.balance(&s.adapter.address), 100 * UNIT);
    assert_eq!(s.adapter.max_withdraw(), 100 * UNIT);
    assert_eq!(s.adapter.position_value(&s.core), 110 * UNIT);
    assert_eq!(s.token.balance(&s.core), 0);
    assert_eq!(s.token.balance(&s.adapter.address), 0);

    // Yield accrues to every share: 1,331 assets over 1,100 shares.
    s.accrue_yield(121 * UNIT);
    assert_eq!(s.adapter.position_value(&s.core), 121 * UNIT);
}

#[test]
fn test_position_value_rounds_down() {
    let s = setup();
    s.fund_core(100 * UNIT);
    s.adapter.deposit(&s.core, &(100 * UNIT), &0);
    s.accrue_yield(7);

    // 1,100 USDC + 7 stroops over 1,100 shares: 100 shares hold 100 USDC and
    // 7/11 of a stroop, which must not be reported.
    assert_eq!(s.adapter.position_value(&s.core), 100 * UNIT);
}

#[test]
fn test_position_value_is_zero_without_shares() {
    let s = setup();
    assert_eq!(s.adapter.position_value(&s.core), 0);
    assert_eq!(s.adapter.max_withdraw(), 0);
}

#[test]
fn test_first_deposit_into_an_empty_vault_counts_only_shares_received() {
    let env = Env::default();
    env.mock_all_auths();
    let core = Address::generate(&env);
    let token_id = env
        .register_stellar_asset_contract_v2(Address::generate(&env))
        .address();
    let vault = MockDefindexVaultClient::new(&env, &env.register_contract(None, MockDefindexVault));
    vault.initialize(&token_id);
    let adapter = DefindexAdapterContractClient::new(
        &env,
        &env.register_contract(None, DefindexAdapterContract),
    );
    adapter.initialize(&core, &vault.address, &token_id);
    StellarAssetClient::new(&env, &token_id).mint(&core, &(10 * UNIT));

    // DeFindex locks 1,000 of the first depositor's shares to the vault.
    let units = adapter.deposit(&core, &(10 * UNIT), &0);

    assert_eq!(units, 10 * UNIT - 1_000);
    assert_eq!(adapter.max_withdraw(), 10 * UNIT - 1_000);
    assert_eq!(adapter.position_value(&core), 10 * UNIT - 1_000);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn test_deposit_reverts_below_min_units() {
    let s = setup();
    s.accrue_yield(1_000 * UNIT);
    s.fund_core(100 * UNIT);

    // At 2 assets per share, 100 assets mint 50 shares.
    s.adapter.deposit(&s.core, &(100 * UNIT), &(51 * UNIT));
}

#[test]
#[should_panic(expected = "Error(Contract, #5)")]
fn test_deposit_rejects_non_positive_amount() {
    let s = setup();
    s.adapter.deposit(&s.core, &0, &0);
}

#[test]
#[should_panic(expected = "Error(Contract, #3)")]
fn test_only_core_can_move_funds() {
    let s = setup();
    let stranger = Address::generate(&s.env);
    s.token_admin.mint(&stranger, &(100 * UNIT));

    s.adapter.deposit(&stranger, &(100 * UNIT), &0);
}

#[test]
fn test_withdraw_requires_core_auth() {
    let s = setup();
    s.fund_core(100 * UNIT);
    s.adapter.deposit(&s.core, &(100 * UNIT), &0);

    // No authorizations at all: the core has not signed this withdraw.
    s.env.set_auths(&[]);
    assert!(s.adapter.try_withdraw(&s.core, &(50 * UNIT), &0).is_err());
    assert_eq!(s.adapter.max_withdraw(), 100 * UNIT);
}

#[test]
fn test_withdraw_returns_underlying() {
    let s = setup();
    s.fund_core(100 * UNIT);
    s.adapter.deposit(&s.core, &(100 * UNIT), &0);
    // 1,210 assets over 1,100 shares: each share is worth 1.1.
    s.accrue_yield(110 * UNIT);

    let paid = s.adapter.withdraw(&s.core, &(50 * UNIT), &(55 * UNIT));

    assert_eq!(paid, 55 * UNIT);
    assert_eq!(s.token.balance(&s.core), 55 * UNIT);
    assert_eq!(s.token.balance(&s.adapter.address), 0);
    assert_eq!(s.adapter.max_withdraw(), 50 * UNIT);
    assert_eq!(s.adapter.position_value(&s.core), 55 * UNIT);

    let paid = s.adapter.withdraw(&s.core, &(50 * UNIT), &0);
    assert_eq!(paid, 55 * UNIT);
    assert_eq!(s.adapter.max_withdraw(), 0);
    assert_eq!(s.adapter.position_value(&s.core), 0);
}

#[test]
fn test_withdraw_pays_to_the_given_recipient() {
    let s = setup();
    s.fund_core(100 * UNIT);
    s.adapter.deposit(&s.core, &(100 * UNIT), &0);
    let to = Address::generate(&s.env);

    let paid = s.adapter.withdraw(&to, &(40 * UNIT), &0);

    assert_eq!(paid, 40 * UNIT);
    assert_eq!(s.token.balance(&to), 40 * UNIT);
}

#[test]
#[should_panic(expected = "Error(Contract, #17)")]
fn test_withdraw_reverts_below_min_out() {
    let s = setup();
    s.fund_core(100 * UNIT);
    s.adapter.deposit(&s.core, &(100 * UNIT), &0);

    s.adapter.withdraw(&s.core, &(50 * UNIT), &(50 * UNIT + 1));
}

#[test]
#[should_panic(expected = "Error(Contract, #4)")]
fn test_withdraw_cannot_exceed_shares_held() {
    let s = setup();
    s.fund_core(100 * UNIT);
    s.adapter.deposit(&s.core, &(100 * UNIT), &0);

    s.adapter.withdraw(&s.core, &(100 * UNIT + 1), &0);
}

#[test]
#[should_panic(expected = "Error(Contract, #5)")]
fn test_withdraw_rejects_non_positive_units() {
    let s = setup();
    s.adapter.withdraw(&s.core, &0, &0);
}

/// Pays at least what the core asks for: the core converts assets to units
/// as `ceil(held * assets / position_value)` and allows only 1 bp shortfall.
#[test]
fn test_withdraw_pays_at_least_core_requested_assets() {
    let s = setup();
    s.fund_core(1_000 * UNIT);
    s.adapter.deposit(&s.core, &(1_000 * UNIT), &0);
    s.accrue_yield(333_333_337);

    for requested in [1, 7, 12_345, 3 * UNIT + 1, 123 * UNIT + 456] {
        let held = s.adapter.max_withdraw();
        let value = s.adapter.position_value(&s.core);
        let units = ((held * requested + value - 1) / value).min(held);
        let paid = s.adapter.withdraw(&s.core, &units, &requested);
        assert!(paid >= requested, "asked {requested}, paid {paid}");
    }
}

#[test]
fn test_deposit_under_real_auth_needs_only_the_cores_scoped_pull() {
    let s = setup();
    let amount = 100 * UNIT;
    s.fund_core(amount);

    // The core signs exactly what savings_core pre-authorizes: this deposit
    // and the USDC pull into the adapter. The vault's own pull from the
    // adapter must be authorized by the adapter itself.
    s.env.mock_auths(&[MockAuth {
        address: &s.core,
        invoke: &MockAuthInvoke {
            contract: &s.adapter.address,
            fn_name: "deposit",
            args: (&s.core, amount, 0i128).into_val(&s.env),
            sub_invokes: &[MockAuthInvoke {
                contract: &s.token.address,
                fn_name: "transfer",
                args: (&s.core, &s.adapter.address, amount).into_val(&s.env),
                sub_invokes: &[],
            }],
        },
    }]);

    let units = s.adapter.deposit(&s.core, &amount, &0);

    assert_eq!(units, amount);
    assert_eq!(s.token.balance(&s.vault.address), 1_000 * UNIT + amount);
}

#[test]
fn test_withdraw_under_real_auth_needs_only_the_cores_signature() {
    let s = setup();
    s.fund_core(100 * UNIT);
    s.adapter.deposit(&s.core, &(100 * UNIT), &0);

    s.env.mock_auths(&[MockAuth {
        address: &s.core,
        invoke: &MockAuthInvoke {
            contract: &s.adapter.address,
            fn_name: "withdraw",
            args: (&s.core, 30 * UNIT, 0i128).into_val(&s.env),
            sub_invokes: &[],
        },
    }]);

    assert_eq!(s.adapter.withdraw(&s.core, &(30 * UNIT), &0), 30 * UNIT);
    assert_eq!(s.token.balance(&s.core), 30 * UNIT);
}

#[test]
fn test_apy_is_unavailable() {
    let s = setup();
    assert_eq!(
        s.adapter.current_apy().confidence,
        ApyConfidence::Unavailable
    );
}

#[test]
fn test_configuration_is_reported() {
    let s = setup();
    assert_eq!(s.adapter.underlying(), s.token.address);
    assert_eq!(s.adapter.get_core(), s.core);
    assert_eq!(s.adapter.get_defindex_vault(), s.vault.address);
    assert_eq!(s.adapter.max_deposit(), i128::MAX);
}

#[test]
#[should_panic(expected = "Error(Contract, #1)")]
fn test_initialize_is_single_use() {
    let s = setup();
    s.adapter
        .initialize(&s.core, &s.vault.address, &s.token.address);
}

#[test]
fn initialize_needs_no_auth_from_the_core() {
    let env = Env::default();
    let core = Address::generate(&env);
    let vault = Address::generate(&env);
    let underlying = Address::generate(&env);
    let adapter = DefindexAdapterContractClient::new(
        &env,
        &env.register_contract(None, DefindexAdapterContract),
    );

    adapter.initialize(&core, &vault, &underlying);

    assert_eq!(adapter.core(), core);
    assert!(adapter.try_initialize(&core, &vault, &underlying).is_err());
}
