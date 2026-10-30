extern crate std;

use nester_test_utils::{MockYieldAdapter, MockYieldAdapterClient};
use soroban_sdk::{
    testutils::{Address as _, Events as _, MockAuth, MockAuthInvoke},
    token::{StellarAssetClient, TokenClient},
    vec, Address, Env, IntoVal, String, Symbol,
};

use crate::{AdapterConfig, AmountShares, SavingsCore, SavingsCoreClient, SavingsError};

const UNIT: i128 = 10_000_000;

struct Setup<'a> {
    env: Env,
    core: SavingsCoreClient<'a>,
    usdc: TokenClient<'a>,
    usdc_admin: StellarAssetClient<'a>,
    owner: Address,
    allocator: Address,
}

fn setup<'a>() -> Setup<'a> {
    let env = Env::default();
    env.mock_all_auths();

    let issuer = Address::generate(&env);
    let usdc_id = env.register_stellar_asset_contract_v2(issuer).address();
    let core_id = env.register_contract(None, SavingsCore);
    let core = SavingsCoreClient::new(&env, &core_id);
    let owner = Address::generate(&env);
    let allocator = Address::generate(&env);
    core.initialize(
        &owner,
        &usdc_id,
        &Address::generate(&env),
        &String::from_str(&env, "Nester Savings USDC"),
        &String::from_str(&env, "nsUSDC"),
        &0u64,
    );
    core.set_allocator(&owner, &allocator);

    Setup {
        owner,
        allocator,
        usdc: TokenClient::new(&env, &usdc_id),
        usdc_admin: StellarAssetClient::new(&env, &usdc_id),
        core,
        env,
    }
}

impl Setup<'_> {
    fn funded_user(&self, amount: i128) -> Address {
        let user = Address::generate(&self.env);
        self.usdc_admin.mint(&user, &amount);
        user
    }

    fn add_adapter(&self, abs_cap: i128, rel_cap_bps: u32) -> MockYieldAdapterClient<'_> {
        let id = self.env.register_contract(None, MockYieldAdapter);
        let adapter = MockYieldAdapterClient::new(&self.env, &id);
        adapter.initialize(&self.usdc.address);
        self.core.add_adapter(
            &self.owner,
            &AdapterConfig {
                adapter: id,
                abs_cap,
                rel_cap_bps,
            },
        );
        adapter
    }

    fn uncapped_adapter(&self) -> MockYieldAdapterClient<'_> {
        self.add_adapter(i128::MAX, 10_000)
    }

    fn allocate(&self, adapter: &Address, assets: i128) {
        self.core.allocate(&self.allocator, adapter, &assets);
    }
}

#[test]
fn test_first_deposit_mints_one_to_one() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);

    let shares = s.core.deposit(&alice, &(100 * UNIT), &0);

    assert_eq!(shares, 100 * UNIT);
    assert_eq!(s.core.balance(&alice), 100 * UNIT);
    assert_eq!(s.core.total_supply(), 100 * UNIT);
    assert_eq!(s.core.total_assets(), 100 * UNIT);
    assert_eq!(s.usdc.balance(&alice), 0);
    assert_eq!(s.core.share_price(), UNIT);
}

#[test]
fn test_second_depositor_proportional() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    let bob = s.funded_user(110 * UNIT);

    s.core.deposit(&alice, &(100 * UNIT), &0);
    s.usdc_admin.mint(&s.core.address, &(10 * UNIT));

    let shares = s.core.deposit(&bob, &(110 * UNIT), &0);

    assert!(
        (shares - 100 * UNIT).abs() <= UNIT,
        "expected ~100 shares, got {shares}"
    );
    assert_eq!(s.core.total_assets(), 220 * UNIT);
}

#[test]
fn test_rounding_favors_vault() {
    let s = setup();
    let alice = s.funded_user(1_000 * UNIT);
    let bob = s.funded_user(333 * UNIT + 7);

    s.core.deposit(&alice, &(1_000 * UNIT), &0);
    s.usdc_admin.mint(&s.core.address, &(37 * UNIT + 3));

    let deposited = 333 * UNIT + 7;
    let shares = s.core.deposit(&bob, &deposited, &0);
    let assets_out = s.core.redeem(&bob, &shares, &0);

    assert!(assets_out <= deposited, "{assets_out} > {deposited}");
    assert_eq!(s.core.balance(&bob), 0);
}

#[test]
fn test_donation_does_not_steal_next_deposit() {
    let s = setup();
    let attacker = s.funded_user(1_000 * UNIT + 1);
    let victim = s.funded_user(1_000 * UNIT);

    s.core.deposit(&attacker, &1, &0);
    s.usdc.transfer(&attacker, &s.core.address, &(1_000 * UNIT));

    let shares = s.core.deposit(&victim, &(1_000 * UNIT), &0);
    let assets_out = s.core.redeem(&victim, &shares, &0);

    assert!(
        assets_out >= 999 * UNIT,
        "victim recovered only {assets_out}"
    );
}

#[test]
fn test_slippage_guards() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);

    let preview = s.core.convert_to_shares(&(100 * UNIT));
    let err = s
        .core
        .try_deposit(&alice, &(100 * UNIT), &(preview + 1))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Slippage.into());

    let shares = s.core.deposit(&alice, &(100 * UNIT), &preview);
    let preview_assets = s.core.convert_to_assets(&shares);
    let err = s
        .core
        .try_redeem(&alice, &shares, &(preview_assets + 1))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Slippage.into());

    let err = s
        .core
        .try_withdraw(&alice, &(10 * UNIT), &(10 * UNIT - 1))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Slippage.into());
}

#[test]
fn test_share_token_transfer() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    let bob = Address::generate(&s.env);

    let shares = s.core.deposit(&alice, &(100 * UNIT), &0);
    s.core.transfer(&alice, &bob, &(shares / 2));

    assert_eq!(s.core.balance(&alice), shares - shares / 2);
    assert_eq!(s.core.balance(&bob), shares / 2);
    assert_eq!(s.core.total_supply(), shares);

    s.core.approve(&bob, &alice, &(10 * UNIT), &1_000);
    assert_eq!(s.core.allowance(&bob, &alice), 10 * UNIT);
    s.core.transfer_from(&alice, &bob, &alice, &(4 * UNIT));
    assert_eq!(s.core.allowance(&bob, &alice), 6 * UNIT);
    assert_eq!(s.core.balance(&bob), shares / 2 - 4 * UNIT);

    let err = s
        .core
        .try_transfer(&bob, &alice, &(shares))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::InsufficientShares.into());

    assert_eq!(s.core.decimals(), 7);
    assert_eq!(s.core.symbol(), String::from_str(&s.env, "nsUSDC"));
}

#[test]
fn test_withdraw_burns_shares_rounded_up() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);

    s.core.deposit(&alice, &(100 * UNIT), &0);
    s.usdc_admin.mint(&s.core.address, &(UNIT / 3));

    let burned = s.core.withdraw(&alice, &(50 * UNIT), &(50 * UNIT));

    assert_eq!(s.usdc.balance(&alice), 50 * UNIT);
    assert!(s.core.convert_to_assets(&burned) >= 50 * UNIT - 1);
    assert_eq!(s.core.balance(&alice), 100 * UNIT - burned);
}

#[test]
fn test_zero_amounts_rejected() {
    let s = setup();
    let alice = s.funded_user(UNIT);

    let err = s.core.try_deposit(&alice, &0, &0).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::ZeroAmount.into());
    let err = s.core.try_redeem(&alice, &0, &0).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::ZeroAmount.into());
}

#[test]
fn test_deposit_and_withdraw_events() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);

    s.core.deposit(&alice, &(100 * UNIT), &0);
    let (_, topics, data) = s.env.events().all().last().unwrap();
    assert_eq!(
        topics,
        (Symbol::new(&s.env, "deposit"), alice.clone()).into_val(&s.env)
    );
    let data: AmountShares = data.into_val(&s.env);
    assert_eq!(data.amount, 100 * UNIT);
    assert_eq!(data.shares, 100 * UNIT);

    s.core.redeem(&alice, &(40 * UNIT), &0);
    let (_, topics, data) = s.env.events().all().last().unwrap();
    assert_eq!(
        topics,
        (Symbol::new(&s.env, "withdraw"), alice.clone()).into_val(&s.env)
    );
    let data: AmountShares = data.into_val(&s.env);
    assert_eq!(data.amount, 40 * UNIT);
    assert_eq!(data.shares, 40 * UNIT);
}

#[test]
fn test_total_assets_includes_adapter_value() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let adapter = s.uncapped_adapter();

    s.allocate(&adapter.address, 60 * UNIT);
    assert_eq!(s.core.idle(), 40 * UNIT);
    assert_eq!(s.core.allocation(&adapter.address), 60 * UNIT);
    assert_eq!(s.core.total_assets(), 100 * UNIT);

    s.usdc_admin.mint(&adapter.address, &(6 * UNIT));
    adapter.set_value(&(66 * UNIT));
    assert_eq!(s.core.total_assets(), 106 * UNIT);

    s.core
        .deallocate(&s.allocator, &adapter.address, &(30 * UNIT));
    assert!(s.core.idle() >= 70 * UNIT);
    assert_eq!(s.core.total_assets(), 106 * UNIT);
}

#[test]
fn test_allocate_respects_abs_and_rel_caps() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let abs = s.add_adapter(50 * UNIT, 10_000);
    let rel = s.add_adapter(i128::MAX, 4_000);

    let err = s
        .core
        .try_allocate(&s.allocator, &abs.address, &(51 * UNIT))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::CapExceeded.into());
    s.allocate(&abs.address, 50 * UNIT);

    let err = s
        .core
        .try_allocate(&s.allocator, &rel.address, &(41 * UNIT))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::CapExceeded.into());
    s.allocate(&rel.address, 40 * UNIT);

    assert_eq!(s.core.idle(), 10 * UNIT);
    assert_eq!(s.core.adapters().len(), 2);
}

#[test]
fn test_only_allocator_can_allocate() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let adapter = s.uncapped_adapter();
    let mallory = Address::generate(&s.env);

    let err = s
        .core
        .try_allocate(&mallory, &adapter.address, &UNIT)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());

    s.allocate(&adapter.address, 10 * UNIT);
    let err = s
        .core
        .try_deallocate(&mallory, &adapter.address, &UNIT)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());

    let err = s
        .core
        .try_set_allocator(&mallory, &mallory)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());
}

#[test]
fn test_allocate_preauthorizes_exact_pull() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let adapter = s.uncapped_adapter();
    let assets = 25 * UNIT;

    s.core
        .mock_auths(&[MockAuth {
            address: &s.allocator,
            invoke: &MockAuthInvoke {
                contract: &s.core.address,
                fn_name: "allocate",
                args: (s.allocator.clone(), adapter.address.clone(), assets).into_val(&s.env),
                sub_invokes: &[],
            },
        }])
        .allocate(&s.allocator, &adapter.address, &assets);

    assert_eq!(s.usdc.balance(&adapter.address), assets);
    assert_eq!(s.core.idle(), 100 * UNIT - assets);
}

#[test]
fn test_deposit_reverts_when_adapter_unavailable() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    let bob = s.funded_user(10 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let adapter = s.uncapped_adapter();
    s.allocate(&adapter.address, 30 * UNIT);
    adapter.set_failing(&true);

    let err = s
        .core
        .try_deposit(&bob, &(10 * UNIT), &0)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::AdapterUnavailable.into());
    assert_eq!(s.usdc.balance(&bob), 10 * UNIT);
}

#[test]
fn test_withdraw_skips_failing_adapter_conservatively() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let failing = s.uncapped_adapter();
    let healthy = s.uncapped_adapter();
    s.allocate(&failing.address, 30 * UNIT);
    s.allocate(&healthy.address, 30 * UNIT);
    failing.set_failing(&true);

    assert_eq!(s.core.total_assets(), 70 * UNIT);
    let preview = s.core.convert_to_assets(&(60 * UNIT));
    assert!(preview > 40 * UNIT && preview < 43 * UNIT);

    let paid = s.core.redeem(&alice, &(60 * UNIT), &preview);

    assert_eq!(paid, preview);
    assert_eq!(s.usdc.balance(&alice), preview);
    assert!(s.core.allocation(&healthy.address) < 30 * UNIT);
    assert_eq!(s.usdc.balance(&failing.address), 30 * UNIT);

    // The healthy position still values, but can no longer be withdrawn: the
    // remaining 40 shares are priced at ~28 USDC with almost nothing liquid.
    healthy.set_withdraw_failing(&true);
    let err = s
        .core
        .try_redeem(&alice, &(40 * UNIT), &0)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::InsufficientLiquidity.into());
}

#[test]
fn test_withdraw_pulls_in_withdraw_order() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let a = s.uncapped_adapter();
    let b = s.uncapped_adapter();
    s.allocate(&a.address, 40 * UNIT);
    s.allocate(&b.address, 40 * UNIT);
    assert_eq!(
        s.core.withdraw_order(),
        vec![&s.env, a.address.clone(), b.address.clone()]
    );

    let err = s
        .core
        .try_set_withdraw_order(&s.owner, &vec![&s.env, b.address.clone()])
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::InvalidAdapter.into());
    s.core.set_withdraw_order(
        &s.owner,
        &vec![&s.env, b.address.clone(), a.address.clone()],
    );

    s.core.withdraw(&alice, &(50 * UNIT), &i128::MAX);

    assert_eq!(s.usdc.balance(&alice), 50 * UNIT);
    assert_eq!(s.core.allocation(&a.address), 40 * UNIT);
    assert_eq!(s.core.allocation(&b.address), 10 * UNIT);
}
