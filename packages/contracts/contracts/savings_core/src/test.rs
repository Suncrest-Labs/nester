extern crate std;

use nester_test_utils::{MockYieldAdapter, MockYieldAdapterClient};
use soroban_sdk::{
    testutils::{Address as _, Events as _, Ledger as _, MockAuth, MockAuthInvoke},
    token::{StellarAssetClient, TokenClient},
    vec, Address, Env, IntoVal, String, Symbol, Val,
};

use savings_reserve::{SavingsReserve, SavingsReserveClient};

use crate::{AmountShares, Change, SavingsCore, SavingsCoreClient, SavingsError};

const UNIT: i128 = 10_000_000;
const TIMELOCK: u64 = 259_200;

struct Setup<'a> {
    env: Env,
    core: SavingsCoreClient<'a>,
    usdc: TokenClient<'a>,
    usdc_admin: StellarAssetClient<'a>,
    owner: Address,
    allocator: Address,
    curator: Address,
    guardian: Address,
    reserve: Address,
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
    let curator = Address::generate(&env);
    let guardian = Address::generate(&env);
    let reserve = env.register_contract(None, SavingsReserve);
    SavingsReserveClient::new(&env, &reserve).initialize(&core_id);
    core.initialize(
        &owner,
        &usdc_id,
        &reserve,
        &String::from_str(&env, "Nester Savings USDC"),
        &String::from_str(&env, "nsUSDC"),
        &0u64,
    );
    core.set_allocator(&owner, &allocator);
    core.set_curator(&owner, &curator);
    core.set_guardian(&owner, &guardian);

    Setup {
        owner,
        allocator,
        curator,
        guardian,
        reserve,
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

    fn new_adapter(&self) -> MockYieldAdapterClient<'_> {
        let id = self.env.register_contract(None, MockYieldAdapter);
        let adapter = MockYieldAdapterClient::new(&self.env, &id);
        adapter.initialize(&self.usdc.address);
        adapter
    }

    /// Register an adapter through the timelock: propose, wait, execute.
    fn add_adapter(&self, abs_cap: i128, rel_cap_bps: u32) -> MockYieldAdapterClient<'_> {
        let adapter = self.new_adapter();
        let id = self.core.propose(
            &self.owner,
            &Change::AddAdapter(adapter.address.clone(), abs_cap, rel_cap_bps),
        );
        self.advance(self.core.timelock());
        assert!(self.core.execute(&id));
        adapter
    }

    fn advance(&self, seconds: u64) {
        self.env.ledger().with_mut(|l| l.timestamp += seconds);
    }

    fn now(&self) -> u64 {
        self.env.ledger().timestamp()
    }

    fn caps(&self, adapter: &Address) -> (i128, u32) {
        let cfg = self
            .core
            .adapters()
            .iter()
            .find(|c| &c.adapter == adapter)
            .unwrap();
        (cfg.abs_cap, cfg.rel_cap_bps)
    }

    fn last_topics(&self) -> soroban_sdk::Vec<Val> {
        self.env.events().all().last().unwrap().1
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
        .try_set_withdraw_order(&s.curator, &vec![&s.env, b.address.clone()])
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::InvalidAdapter.into());
    let err = s
        .core
        .try_set_withdraw_order(
            &s.owner,
            &vec![&s.env, b.address.clone(), a.address.clone()],
        )
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());
    s.core.set_withdraw_order(
        &s.curator,
        &vec![&s.env, b.address.clone(), a.address.clone()],
    );

    s.core.withdraw(&alice, &(50 * UNIT), &i128::MAX);

    assert_eq!(s.usdc.balance(&alice), 50 * UNIT);
    assert_eq!(s.core.allocation(&a.address), 40 * UNIT);
    assert_eq!(s.core.allocation(&b.address), 10 * UNIT);
}

// --- Governance ----------------------------------------------------------

#[test]
fn test_add_adapter_waits_for_timelock() {
    let s = setup();
    let adapter = s.new_adapter();
    let change = Change::AddAdapter(adapter.address.clone(), 50 * UNIT, 5_000);
    assert_eq!(s.core.timelock(), TIMELOCK);

    let id = s.core.propose(&s.curator, &change);
    assert_eq!(
        s.last_topics(),
        (Symbol::new(&s.env, "change_proposed"), id).into_val(&s.env)
    );
    assert_eq!(
        s.core.pending_changes(),
        vec![&s.env, (id, change, s.now() + TIMELOCK)]
    );

    let err = s.core.try_execute(&id).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NotReady.into());
    s.advance(TIMELOCK - 1);
    let err = s.core.try_execute(&id).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NotReady.into());
    assert_eq!(s.core.adapters().len(), 0);

    s.advance(1);
    assert!(s.core.execute(&id));
    assert_eq!(
        s.last_topics(),
        (Symbol::new(&s.env, "change_executed"), id).into_val(&s.env)
    );
    assert_eq!(s.caps(&adapter.address), (50 * UNIT, 5_000));
    assert_eq!(
        s.core.withdraw_order(),
        vec![&s.env, adapter.address.clone()]
    );
    assert_eq!(s.core.pending_changes().len(), 0);

    let err = s.core.try_execute(&id).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NoSuchChange.into());
}

#[test]
fn test_anyone_executes_after_eta() {
    let s = setup();
    let adapter = s.new_adapter();
    let id = s.core.propose(
        &s.owner,
        &Change::AddAdapter(adapter.address.clone(), i128::MAX, 10_000),
    );
    s.advance(TIMELOCK);

    s.core.execute(&id);

    assert!(s.env.auths().is_empty(), "execute must not require auth");
    assert_eq!(s.core.adapters().len(), 1);
}

#[test]
fn test_execute_rechecks_change() {
    let s = setup();
    let adapter = s.new_adapter();
    let change = Change::AddAdapter(adapter.address.clone(), UNIT, 10_000);
    let first = s.core.propose(&s.owner, &change);
    let second = s.core.propose(&s.curator, &change);
    assert_ne!(first, second);
    s.advance(TIMELOCK);

    assert!(s.core.execute(&first));

    // The duplicate no longer applies: it is dropped, not left stuck.
    assert!(!s.core.execute(&second));
    assert_eq!(
        s.last_topics(),
        (Symbol::new(&s.env, "change_dropped"), second).into_val(&s.env)
    );
    assert_eq!(s.core.pending_changes().len(), 0);
    assert_eq!(s.core.adapters().len(), 1);
    let err = s.core.try_execute(&second).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NoSuchChange.into());

    let err = s.core.try_propose(&s.owner, &change).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::InvalidAdapter.into());
    let unknown = Change::RaiseCap(Address::generate(&s.env), UNIT, 10_000);
    let err = s.core.try_propose(&s.owner, &unknown).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::InvalidAdapter.into());
    let over_100_pct = Change::RaiseCap(adapter.address.clone(), UNIT, 10_001);
    let err = s
        .core
        .try_propose(&s.owner, &over_100_pct)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::InvalidAdapter.into());

    // A queued raise for an adapter removed in the meantime is dropped too.
    let raise = Change::RaiseCap(adapter.address.clone(), 2 * UNIT, 10_000);
    let id = s.core.propose(&s.curator, &raise);
    s.core.remove_adapter(&s.owner, &adapter.address);
    s.advance(TIMELOCK);
    assert!(!s.core.execute(&id));
    assert_eq!(s.core.pending_changes().len(), 0);
    assert_eq!(s.core.adapters().len(), 0);
}

#[test]
fn test_pending_queue_is_capped() {
    let s = setup();
    let shorter = Change::SetTimelock(3_600);
    let mut ids = std::vec::Vec::new();
    for _ in 0..16 {
        ids.push(s.core.propose(&s.curator, &shorter));
    }
    assert_eq!(s.core.pending_changes().len(), 16);

    let err = s.core.try_propose(&s.owner, &shorter).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::QueueFull.into());
    // Risk-reducing changes never queue, so a full queue does not block them.
    assert_eq!(s.core.propose(&s.owner, &Change::SetTimelock(TIMELOCK)), 0);

    s.core.revoke(&s.guardian, &ids[3]);
    let id = s.core.propose(&s.owner, &shorter);
    assert!(!ids.contains(&id));
    assert_eq!(s.core.pending_changes().len(), 16);

    s.advance(TIMELOCK);
    assert!(s.core.execute(&ids[0]));
    assert_eq!(s.core.pending_changes().len(), 15);
    assert_eq!(s.core.timelock(), 3_600);
}

#[test]
fn test_guardian_revokes_pending_change() {
    let s = setup();
    let adapter = s.new_adapter();
    let mallory = Address::generate(&s.env);
    let id = s.core.propose(
        &s.curator,
        &Change::AddAdapter(adapter.address.clone(), UNIT, 10_000),
    );

    for caller in [&mallory, &s.curator] {
        let err = s.core.try_revoke(caller, &id).unwrap_err().unwrap();
        assert_eq!(err, SavingsError::Unauthorized.into());
    }
    s.core.revoke(&s.guardian, &id);
    assert_eq!(
        s.last_topics(),
        (Symbol::new(&s.env, "change_revoked"), id).into_val(&s.env)
    );
    assert_eq!(s.core.pending_changes().len(), 0);

    s.advance(TIMELOCK);
    let err = s.core.try_execute(&id).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NoSuchChange.into());
    let err = s.core.try_revoke(&s.guardian, &id).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NoSuchChange.into());

    let id = s.core.propose(&s.curator, &Change::SetTimelock(3_600));
    s.core.revoke(&s.owner, &id);
    assert_eq!(s.core.pending_changes().len(), 0);
    assert_eq!(s.core.adapters().len(), 0);
    assert_eq!(s.core.timelock(), TIMELOCK);
}

#[test]
fn test_shorten_timelock_is_timelocked_lengthen_is_instant() {
    let s = setup();

    let id = s.core.propose(&s.curator, &Change::SetTimelock(400_000));
    assert_eq!(id, 0);
    assert_eq!(s.core.timelock(), 400_000);
    assert_eq!(s.core.pending_changes().len(), 0);

    let id = s.core.propose(&s.owner, &Change::SetTimelock(3_600));
    assert_ne!(id, 0);
    assert_eq!(s.core.timelock(), 400_000);
    assert_eq!(
        s.core.pending_changes().get(0).unwrap().2,
        s.now() + 400_000
    );

    s.advance(TIMELOCK);
    let err = s.core.try_execute(&id).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NotReady.into());
    s.advance(400_000 - TIMELOCK);
    s.core.execute(&id);
    assert_eq!(s.core.timelock(), 3_600);

    let adapter = s.new_adapter();
    let id = s.core.propose(
        &s.owner,
        &Change::AddAdapter(adapter.address.clone(), UNIT, 10_000),
    );
    assert_eq!(s.core.pending_changes().get(0).unwrap().2, s.now() + 3_600);
    s.advance(3_600);
    s.core.execute(&id);
}

#[test]
fn test_guardian_cannot_add_risk() {
    let s = setup();
    let adapter = s.new_adapter();
    let listed = s.add_adapter(UNIT, 1_000);
    let mallory = Address::generate(&s.env);

    for caller in [&s.guardian, &s.allocator, &mallory] {
        for change in [
            Change::AddAdapter(adapter.address.clone(), UNIT, 10_000),
            Change::RaiseCap(listed.address.clone(), 2 * UNIT, 2_000),
            Change::SetTimelock(3_600),
        ] {
            let err = s.core.try_propose(caller, &change).unwrap_err().unwrap();
            assert_eq!(err, SavingsError::Unauthorized.into());
        }
    }
    assert_eq!(s.core.pending_changes().len(), 0);
}

#[test]
fn test_pause_blocks_deposits_not_withdrawals() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    let bob = s.funded_user(10 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);

    let err = s.core.try_pause_deposits(&s.owner).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());
    s.core.pause_deposits(&s.guardian);
    assert!(s.core.deposits_paused());

    let err = s
        .core
        .try_deposit(&bob, &(10 * UNIT), &0)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::DepositsPaused.into());
    s.core.redeem(&alice, &(40 * UNIT), &0);
    s.core.withdraw(&alice, &(10 * UNIT), &i128::MAX);
    assert_eq!(s.usdc.balance(&alice), 50 * UNIT);

    let err = s
        .core
        .try_unpause_deposits(&s.guardian)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());
    s.core.unpause_deposits(&s.owner);
    assert!(!s.core.deposits_paused());
    s.core.deposit(&bob, &(10 * UNIT), &0);
}

#[test]
fn test_lower_cap_and_zero_cap_instant() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let a = s.add_adapter(50 * UNIT, 5_000).address;
    let mallory = Address::generate(&s.env);

    s.core.lower_cap(&s.curator, &a, &(40 * UNIT), &4_000);
    assert_eq!(s.caps(&a), (40 * UNIT, 4_000));
    s.core.lower_cap(&s.owner, &a, &(30 * UNIT), &4_000);
    assert_eq!(s.caps(&a), (30 * UNIT, 4_000));
    for (abs, rel) in [(31 * UNIT, 4_000), (30 * UNIT, 4_001)] {
        let err = s
            .core
            .try_lower_cap(&s.curator, &a, &abs, &rel)
            .unwrap_err()
            .unwrap();
        assert_eq!(err, SavingsError::CapIncrease.into());
    }
    for caller in [&s.guardian, &mallory] {
        let err = s
            .core
            .try_lower_cap(caller, &a, &0, &0)
            .unwrap_err()
            .unwrap();
        assert_eq!(err, SavingsError::Unauthorized.into());
    }

    // A RaiseCap that raises nothing applies on the spot.
    let lower = Change::RaiseCap(a.clone(), 20 * UNIT, 3_000);
    assert_eq!(s.core.propose(&s.curator, &lower), 0);
    assert_eq!(s.caps(&a), (20 * UNIT, 3_000));

    let err = s.core.try_zero_cap(&mallory, &a).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());
    s.core.zero_cap(&s.guardian, &a);
    assert_eq!(s.caps(&a), (0, 0));
    let err = s
        .core
        .try_allocate(&s.allocator, &a, &UNIT)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::CapExceeded.into());

    // Raising either cap waits for the timelock.
    let raise = Change::RaiseCap(a.clone(), 60 * UNIT, 0);
    let id = s.core.propose(&s.curator, &raise);
    assert_ne!(id, 0);
    assert_eq!(s.caps(&a), (0, 0));
    s.advance(TIMELOCK);
    s.core.execute(&id);
    assert_eq!(s.caps(&a), (60 * UNIT, 0));
}

#[test]
fn test_remove_adapter_requires_zero_value() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let a = s.uncapped_adapter();
    let b = s.uncapped_adapter();
    s.allocate(&a.address, 10 * UNIT);

    let err = s
        .core
        .try_remove_adapter(&s.owner, &a.address)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::AdapterNotEmpty.into());
    let err = s
        .core
        .try_remove_adapter(&s.curator, &b.address)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());

    s.core.remove_adapter(&s.owner, &b.address);
    assert_eq!(s.core.adapters().len(), 1);
    assert_eq!(s.core.withdraw_order(), vec![&s.env, a.address.clone()]);

    s.core.deallocate(&s.allocator, &a.address, &(10 * UNIT));
    s.core.remove_adapter(&s.owner, &a.address);
    assert_eq!(s.core.adapters().len(), 0);
    assert_eq!(s.core.withdraw_order().len(), 0);
    assert_eq!(s.core.total_assets(), 100 * UNIT);
}

#[test]
fn test_roles_and_two_step_ownership() {
    let s = setup();
    let new_owner = Address::generate(&s.env);
    let mallory = Address::generate(&s.env);

    assert_eq!(s.core.owner(), s.owner);
    assert_eq!(s.core.curator(), Some(s.curator.clone()));
    assert_eq!(s.core.allocator(), Some(s.allocator.clone()));
    assert_eq!(s.core.guardian(), Some(s.guardian.clone()));

    for caller in [&s.curator, &s.guardian] {
        let err = s
            .core
            .try_set_curator(caller, &mallory)
            .unwrap_err()
            .unwrap();
        assert_eq!(err, SavingsError::Unauthorized.into());
        let err = s
            .core
            .try_set_guardian(caller, &mallory)
            .unwrap_err()
            .unwrap();
        assert_eq!(err, SavingsError::Unauthorized.into());
        let err = s
            .core
            .try_transfer_ownership(caller, &mallory)
            .unwrap_err()
            .unwrap();
        assert_eq!(err, SavingsError::Unauthorized.into());
    }

    s.core.transfer_ownership(&s.owner, &new_owner);
    assert_eq!(s.core.owner(), s.owner);
    let err = s.core.try_accept_ownership(&mallory).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());

    s.core.accept_ownership(&new_owner);
    assert_eq!(s.core.owner(), new_owner);
    let err = s
        .core
        .try_set_curator(&s.owner, &mallory)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());
    s.core.set_curator(&new_owner, &mallory);
    assert_eq!(s.core.curator(), Some(mallory));
    let err = s
        .core
        .try_accept_ownership(&new_owner)
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::Unauthorized.into());
}

#[test]
fn test_initialize_requires_owner_auth() {
    let env = Env::default();
    let core = SavingsCoreClient::new(&env, &env.register_contract(None, SavingsCore));

    let res = core.try_initialize(
        &Address::generate(&env),
        &Address::generate(&env),
        &Address::generate(&env),
        &String::from_str(&env, "Nester Savings USDC"),
        &String::from_str(&env, "nsUSDC"),
        &0u64,
    );

    assert!(res.is_err());
}

#[test]
fn test_timelock_stays_within_bounds() {
    const MIN: u64 = 3_600;
    const MAX: u64 = 1_209_600;
    let s = setup();

    for caller in [&s.owner, &s.curator] {
        for seconds in [0, 1, MIN - 1, MAX + 1, u64::MAX] {
            let err = s
                .core
                .try_propose(caller, &Change::SetTimelock(seconds))
                .unwrap_err()
                .unwrap();
            assert_eq!(err, SavingsError::InvalidTimelock.into());
        }
    }
    assert_eq!(s.core.timelock(), TIMELOCK);
    assert_eq!(s.core.pending_changes().len(), 0);

    let id = s.core.propose(&s.owner, &Change::SetTimelock(MIN));
    assert_ne!(id, 0);
    s.core.revoke(&s.owner, &id);

    assert_eq!(s.core.propose(&s.curator, &Change::SetTimelock(MAX)), 0);
    assert_eq!(s.core.timelock(), MAX);
    assert_eq!(s.core.pending_changes().len(), 0);

    let init = |timelock: u64| {
        let core = SavingsCoreClient::new(&s.env, &s.env.register_contract(None, SavingsCore));
        let res = core.try_initialize(
            &s.owner,
            &s.usdc.address,
            &Address::generate(&s.env),
            &String::from_str(&s.env, "Nester Savings USDC"),
            &String::from_str(&s.env, "nsUSDC"),
            &timelock,
        );
        (core, res)
    };
    for seconds in [1, MIN - 1, MAX + 1] {
        let (_, res) = init(seconds);
        assert_eq!(
            res.unwrap_err().unwrap(),
            SavingsError::InvalidTimelock.into()
        );
    }
    let (core, res) = init(MIN);
    assert!(res.is_ok());
    assert_eq!(core.timelock(), MIN);
    let (core, res) = init(MAX);
    assert!(res.is_ok());
    assert_eq!(core.timelock(), MAX);
    let (core, res) = init(0);
    assert!(res.is_ok());
    assert_eq!(core.timelock(), TIMELOCK);
}

// --- Fees and the reserve ---------------------------------------------------

/// Deposit 100 USDC from a fresh user and allocate all of it to one adapter.
fn invested<'a>(s: &'a Setup<'a>) -> (Address, MockYieldAdapterClient<'a>) {
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let adapter = s.uncapped_adapter();
    s.allocate(&adapter.address, 100 * UNIT);
    (alice, adapter)
}

/// Move the adapter's position to `value`, backing any gain with tokens.
fn revalue(s: &Setup, adapter: &MockYieldAdapterClient, value: i128) {
    let current = s.core.allocation(&adapter.address);
    if value > current {
        s.usdc_admin.mint(&adapter.address, &(value - current));
    }
    adapter.set_value(&value);
}

#[test]
fn test_fee_defaults() {
    let s = setup();
    assert_eq!(s.core.fee_bps(), 1_000);
    assert_eq!(s.core.high_water_mark(), UNIT);
    assert_eq!(s.core.reserve(), s.reserve);
    assert_eq!(s.core.reserve_shares(), 0);
    assert_eq!(
        SavingsReserveClient::new(&s.env, &s.reserve).core(),
        s.core.address
    );
}

#[test]
fn test_fee_minted_to_reserve_on_gain() {
    let s = setup();
    let (_, adapter) = invested(&s);
    revalue(&s, &adapter, 110 * UNIT);

    let minted = s.core.accrue();

    assert!(minted > 0);
    assert_eq!(s.core.reserve_shares(), minted);
    let worth = s.core.convert_to_assets(&minted);
    assert!((worth - UNIT).abs() <= 1, "reserve worth {worth}");
    assert_eq!(s.core.total_assets(), 110 * UNIT);
    assert_eq!(s.core.high_water_mark(), s.core.share_price());
    assert_eq!(s.core.accrue(), 0);
}

#[test]
fn test_no_fee_below_high_water_mark() {
    let s = setup();
    let (_, adapter) = invested(&s);
    revalue(&s, &adapter, 110 * UNIT);
    s.core.accrue();
    let fee_shares = s.core.reserve_shares();
    let hwm = s.core.high_water_mark();

    revalue(&s, &adapter, 100 * UNIT);
    assert_eq!(s.core.accrue(), 0);
    assert_eq!(s.core.high_water_mark(), hwm);

    revalue(&s, &adapter, 105 * UNIT);
    assert_eq!(s.core.accrue(), 0);
    assert_eq!(s.core.reserve_shares(), fee_shares);
    assert_eq!(s.core.high_water_mark(), hwm);

    // Only the gain above the old mark is charged: 110 -> 121 is +11 USDC.
    revalue(&s, &adapter, 121 * UNIT);
    let minted = s.core.accrue();
    let worth = s.core.convert_to_assets(&minted);
    let expected = 11 * UNIT / 10;
    assert!((worth - expected).abs() <= 2, "fee worth {worth}");
}

#[test]
fn test_fee_accrues_before_deposit_and_redeem() {
    let s = setup();
    let (alice, adapter) = invested(&s);
    let bob = s.funded_user(109 * UNIT);
    revalue(&s, &adapter, 110 * UNIT);

    let shares = s.core.deposit(&bob, &(109 * UNIT), &0);

    let fee_shares = s.core.reserve_shares();
    assert!(fee_shares > 0);
    // Bob pays the post-fee price, so his shares are worth what he paid.
    let worth = s.core.convert_to_assets(&shares);
    assert!((109 * UNIT - worth).abs() <= 1, "bob holds {worth}");

    revalue(&s, &adapter, 120 * UNIT);
    s.core.redeem(&alice, &(10 * UNIT), &0);
    assert!(s.core.reserve_shares() > fee_shares);
}

#[test]
fn test_fee_skipped_when_adapter_unavailable() {
    let s = setup();
    let alice = s.funded_user(100 * UNIT);
    s.core.deposit(&alice, &(100 * UNIT), &0);
    let healthy = s.uncapped_adapter();
    let failing = s.uncapped_adapter();
    s.allocate(&healthy.address, 50 * UNIT);
    s.allocate(&failing.address, 10 * UNIT);
    revalue(&s, &healthy, 80 * UNIT);
    failing.set_failing(&true);

    assert_eq!(s.core.accrue(), 0);
    s.core.redeem(&alice, &UNIT, &0);
    assert_eq!(s.core.reserve_shares(), 0);
    assert_eq!(s.core.high_water_mark(), UNIT);

    failing.set_failing(&false);
    assert!(s.core.accrue() > 0);
}

#[test]
fn test_fee_cap_enforced() {
    let s = setup();
    let err = s
        .core
        .try_propose(&s.owner, &Change::SetFee(2_001))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::FeeTooHigh.into());
    assert_eq!(s.core.fee_bps(), 1_000);
}

#[test]
fn test_raise_fee_timelocked_lower_instant() {
    let s = setup();

    assert_eq!(s.core.propose(&s.curator, &Change::SetFee(500)), 0);
    assert_eq!(s.core.fee_bps(), 500);

    let id = s.core.propose(&s.owner, &Change::SetFee(2_000));
    assert!(id > 0);
    assert_eq!(s.core.fee_bps(), 500);
    let err = s.core.try_execute(&id).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NotReady.into());

    s.advance(TIMELOCK);
    assert!(s.core.execute(&id));
    assert_eq!(s.core.fee_bps(), 2_000);
}

#[test]
fn test_zero_fee_still_advances_high_water_mark() {
    let s = setup();
    let (_, adapter) = invested(&s);
    s.core.propose(&s.owner, &Change::SetFee(0));
    revalue(&s, &adapter, 110 * UNIT);

    assert_eq!(s.core.accrue(), 0);
    assert_eq!(s.core.high_water_mark(), s.core.share_price());

    let id = s.core.propose(&s.owner, &Change::SetFee(1_000));
    s.advance(TIMELOCK);
    assert!(s.core.execute(&id));
    assert_eq!(s.core.accrue(), 0);
    assert_eq!(s.core.reserve_shares(), 0);
}

#[test]
fn test_loss_lowers_share_price() {
    let s = setup();
    let (_, adapter) = invested(&s);

    revalue(&s, &adapter, 90 * UNIT);

    // The virtual offset's 10_000 stroops do not lose value, so the price
    // sits a few stroops above an exact 10% drop.
    let price = s.core.share_price();
    assert!((price - 9 * UNIT / 10).abs() <= 10, "price {price}");
    assert_eq!(s.core.accrue(), 0);
    assert_eq!(s.core.high_water_mark(), UNIT);
}

#[test]
fn test_burn_reserve_restores_value_timelocked() {
    let s = setup();
    let (_, adapter) = invested(&s);
    revalue(&s, &adapter, 110 * UNIT);
    s.core.accrue();
    let fee_shares = s.core.reserve_shares();
    let hwm = s.core.high_water_mark();
    revalue(&s, &adapter, 100 * UNIT);
    let price_after_loss = s.core.share_price();

    let id = s.core.propose(&s.curator, &Change::BurnReserve(fee_shares));
    assert!(id > 0);
    assert_eq!(s.core.reserve_shares(), fee_shares);
    let err = s.core.try_execute(&id).unwrap_err().unwrap();
    assert_eq!(err, SavingsError::NotReady.into());

    s.advance(TIMELOCK);
    assert!(s.core.execute(&id));

    assert_eq!(s.core.reserve_shares(), 0);
    assert_eq!(s.core.total_supply(), 100 * UNIT);
    assert!(s.core.share_price() > price_after_loss);
    assert_eq!(s.core.share_price(), UNIT);
    assert_eq!(s.core.high_water_mark(), hwm);
}

#[test]
fn test_burn_reserve_never_charges_a_fee() {
    let s = setup();
    let (_, adapter) = invested(&s);
    revalue(&s, &adapter, 110 * UNIT);
    s.core.accrue();
    let fee_shares = s.core.reserve_shares();

    let id = s.core.propose(&s.owner, &Change::BurnReserve(fee_shares));
    s.advance(TIMELOCK);
    assert!(s.core.execute(&id));

    // The burn lifted the price above the old mark; the mark follows it so
    // the burned value is not charged again.
    assert_eq!(s.core.high_water_mark(), s.core.share_price());
    assert_eq!(s.core.accrue(), 0);
    assert_eq!(s.core.reserve_shares(), 0);
}

#[test]
fn test_burn_reserve_validated_at_propose_and_execute() {
    let s = setup();
    let (_, adapter) = invested(&s);
    revalue(&s, &adapter, 110 * UNIT);
    s.core.accrue();
    let fee_shares = s.core.reserve_shares();

    let err = s
        .core
        .try_propose(&s.owner, &Change::BurnReserve(0))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::ZeroAmount.into());
    let err = s
        .core
        .try_propose(&s.owner, &Change::BurnReserve(fee_shares + 1))
        .unwrap_err()
        .unwrap();
    assert_eq!(err, SavingsError::InsufficientShares.into());

    // Two burns of the whole reserve: the second finds it short and is
    // dropped rather than clamped.
    let first = s.core.propose(&s.owner, &Change::BurnReserve(fee_shares));
    let second = s.core.propose(&s.owner, &Change::BurnReserve(fee_shares));
    s.advance(TIMELOCK);
    assert!(s.core.execute(&first));
    assert!(!s.core.execute(&second));
    assert_eq!(s.core.reserve_shares(), 0);
    assert_eq!(s.core.pending_changes().len(), 0);
}

#[test]
fn test_reserve_cannot_transfer_shares() {
    let s = setup();
    let (alice, adapter) = invested(&s);
    revalue(&s, &adapter, 110 * UNIT);
    s.core.accrue();
    let fee_shares = s.core.reserve_shares();
    let alice_shares = s.core.balance(&alice);

    s.env.set_auths(&[]);
    assert!(s.core.try_transfer(&s.reserve, &alice, &1).is_err());
    assert!(s.core.try_approve(&s.reserve, &alice, &1, &1_000).is_err());
    assert!(s.core.try_redeem(&s.reserve, &1, &0).is_err());

    assert_eq!(s.core.reserve_shares(), fee_shares);
    assert_eq!(s.core.balance(&alice), alice_shares);
}
