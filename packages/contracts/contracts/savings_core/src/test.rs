extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Events as _},
    token::{StellarAssetClient, TokenClient},
    Address, Env, IntoVal, String, Symbol,
};

use crate::{AmountShares, SavingsCore, SavingsCoreClient, SavingsError};

const UNIT: i128 = 10_000_000;

struct Setup<'a> {
    env: Env,
    core: SavingsCoreClient<'a>,
    usdc: TokenClient<'a>,
    usdc_admin: StellarAssetClient<'a>,
}

fn setup<'a>() -> Setup<'a> {
    let env = Env::default();
    env.mock_all_auths();

    let issuer = Address::generate(&env);
    let usdc_id = env.register_stellar_asset_contract_v2(issuer).address();
    let core_id = env.register_contract(None, SavingsCore);
    let core = SavingsCoreClient::new(&env, &core_id);
    core.initialize(
        &Address::generate(&env),
        &usdc_id,
        &Address::generate(&env),
        &String::from_str(&env, "Nester Savings USDC"),
        &String::from_str(&env, "nsUSDC"),
        &0u64,
    );

    Setup {
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
