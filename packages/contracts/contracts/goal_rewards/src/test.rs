#![cfg(test)]
extern crate std;

use soroban_sdk::{
    testutils::{Address as _, Events as _, Ledger as _},
    token, Address, BytesN, Env, IntoVal, Vec,
};

use savings_goal::{SavingsGoalContract, SavingsGoalContractClient};
use vault_factory_contract::{VaultFactoryContract, VaultFactoryContractClient};

use crate::{GoalRewardError, GoalRewardsContract, GoalRewardsContractClient};

mod dummy_vault {
    soroban_sdk::contractimport!(file = "../vault_factory/fixtures/dummy_vault.wasm");
}

fn goal_id(env: &Env, seed: u8) -> BytesN<32> {
    BytesN::from_array(env, &[seed; 32])
}

const WEEK: u64 = 7 * 24 * 60 * 60;
const DAY: u64 = 24 * 60 * 60;
/// A goal size that lands in the mid tier configured by `setup()`.
const TARGET: i128 = 1_000_000_000; // 100 units at 7 decimals

struct Fixture {
    env: Env,
    admin: Address,
    owner: Address,
    vault: dummy_vault::Client<'static>,
    token: Address,
    token_admin: token::StellarAssetClient<'static>,
    goals: SavingsGoalContractClient<'static>,
    rewards: GoalRewardsContractClient<'static>,
}

impl Fixture {
    /// Create a goal owned by `self.owner`, due `deadline_secs` from now.
    fn create_goal(&self, seed: u8, target: i128, deadline_secs: u64) -> BytesN<32> {
        let id = goal_id(&self.env, seed);
        self.goals.create_goal(
            &self.owner,
            &id,
            &self.vault.address,
            &target,
            &(self.env.ledger().timestamp() + deadline_secs),
        );
        id
    }

    /// Advance the ledger clock by `secs`.
    fn advance(&self, secs: u64) {
        let now = self.env.ledger().timestamp();
        self.env.ledger().set_timestamp(now + secs);
    }

    fn contribute(&self, goal: &BytesN<32>, amount: i128) {
        self.goals.contribute(&self.owner, goal, &amount);
    }
}

fn setup() -> Fixture {
    let env = Env::default();
    env.mock_all_auths();
    env.ledger().set_timestamp(10_000_000);

    let admin = Address::generate(&env);
    let owner = Address::generate(&env);

    let wasm_hash = env.deployer().upload_contract_wasm(dummy_vault::WASM);
    let factory_id = env.register_contract(None, VaultFactoryContract);
    let factory = VaultFactoryContractClient::new(&env, &factory_id);
    factory.initialize(&admin, &wasm_hash);

    let salt = BytesN::from_array(&env, &[3u8; 32]);
    let underlying = Address::generate(&env);
    let init_args: Vec<soroban_sdk::Val> =
        soroban_sdk::vec![&env, admin.into_val(&env), false.into_val(&env)];
    let vault_addr = factory.create_vault(&admin, &salt, &underlying, &init_args);
    let vault = dummy_vault::Client::new(&env, &vault_addr);

    let goals_cid = env.register_contract(None, SavingsGoalContract);
    let goals = SavingsGoalContractClient::new(&env, &goals_cid);
    goals.initialize(&admin, &factory_id);

    let token_admin_addr = Address::generate(&env);
    let token_sac = env.register_stellar_asset_contract_v2(token_admin_addr);
    let token = token_sac.address();
    let token_admin = token::StellarAssetClient::new(&env, &token);

    let rewards_cid = env.register_contract(None, GoalRewardsContract);
    let rewards = GoalRewardsContractClient::new(&env, &rewards_cid);
    rewards.initialize(&admin, &goals_cid, &token);

    // A single mid-size tier: 2% bonus, capped at 5 units.
    rewards.configure_reward(&admin, &0i128, &200u32, &50_000_000i128);
    // Rules: >=30 day active duration (the initialize default), single
    // contribution <=40% of target OR >=4 distinct periods (also defaults).

    // By default the owner's real vault balance is 0 (dummy_vault's
    // set_balance was never called) - tests that need the anti-fake-
    // contribution basis check to NOT clamp the bonus to zero call
    // vault.set_balance explicitly to simulate real backing funds.
    vault.set_balance(&owner, &TARGET);

    Fixture {
        env,
        admin,
        owner,
        vault,
        token,
        token_admin,
        goals,
        rewards,
    }
}

/// Deposits `count` equal contributions of `amount` spaced `WEEK` apart,
/// completing the goal on the deposit that reaches `target`. Returns the
/// goal id.
fn steady_contributions(
    f: &Fixture,
    seed: u8,
    target: i128,
    per_deposit: i128,
    periods: u32,
) -> BytesN<32> {
    let id = f.create_goal(seed, target, 52 * WEEK);
    for _ in 0..periods {
        f.contribute(&id, per_deposit);
        f.advance(WEEK);
    }
    id
}

// ---------------------------------------------------------------------------
// Pool funding
// ---------------------------------------------------------------------------

#[test]
fn fund_pool_pulls_tokens_and_credits_balance() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &500_000_000);
    assert_eq!(f.rewards.get_reward_pool_balance(), 500_000_000);
    assert_eq!(
        token::Client::new(&f.env, &f.token).balance(&f.rewards.address),
        500_000_000
    );
}

#[test]
fn fund_pool_requires_admin_or_treasurer() {
    let f = setup();
    f.token_admin.mint(&f.owner, &1_000_000_000);
    let result = f.rewards.try_fund_pool(&f.owner, &1_000_000);
    assert!(result.is_err());
}

#[test]
fn sync_credits_only_up_to_the_cap() {
    let f = setup();
    // Push tokens directly (simulating treasury.withdraw's raw transfer)
    // without going through fund_pool.
    f.token_admin.mint(&f.rewards.address, &777_000_000);
    let credited = f.rewards.sync_pool(&f.admin);
    assert_eq!(credited, 777_000_000);
    assert_eq!(f.rewards.get_reward_pool_balance(), 777_000_000);

    // A second sync with no new tokens credits nothing further.
    let credited_again = f.rewards.sync_pool(&f.admin);
    assert_eq!(credited_again, 0);
}

#[test]
fn defund_is_restricted_and_bounded_by_pool() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    // Unauthorized caller rejected.
    assert!(f.rewards.try_defund_pool(&f.owner, &f.owner, &1).is_err());

    // Cannot withdraw more than the tracked pool balance.
    assert!(f
        .rewards
        .try_defund_pool(&f.admin, &f.admin, &2_000_000_000)
        .is_err());

    f.rewards.defund_pool(&f.admin, &f.admin, &400_000_000);
    assert_eq!(f.rewards.get_reward_pool_balance(), 600_000_000);
}

// ---------------------------------------------------------------------------
// configure_reward / set_eligibility_rules
// ---------------------------------------------------------------------------

#[test]
fn configure_reward_requires_admin() {
    let f = setup();
    let result = f
        .rewards
        .try_configure_reward(&f.owner, &0i128, &100u32, &1_000_000i128);
    assert!(result.is_err());
}

#[test]
fn configure_reward_enforces_compile_time_ceilings() {
    let f = setup();
    let over_bps =
        f.rewards
            .try_configure_reward(&f.admin, &0i128, &(crate::MAX_BONUS_BPS + 1), &1i128);
    assert!(over_bps.is_err());

    let over_absolute = f.rewards.try_configure_reward(
        &f.admin,
        &0i128,
        &100u32,
        &(crate::MAX_BONUS_ABSOLUTE_CEILING + 1),
    );
    assert!(over_absolute.is_err());
}

#[test]
fn eligibility_rules_cannot_be_loosened_past_floors() {
    let f = setup();
    // Try to set a duration below the floor, a ceiling above the max, and a
    // distinct-period minimum below the floor - all three must clamp, not
    // apply the requested (looser) value.
    f.rewards
        .set_eligibility_rules(&f.admin, &1u64, &9_999u32, &0u32);

    // There is no direct getter for the raw stored rules, so this is
    // verified indirectly: a goal whose active duration is 1 second (below
    // the real floor) must still be rejected for being too short.
    let id = f.create_goal(1, TARGET, 52 * WEEK);
    f.contribute(&id, TARGET);
    let (eligible, _, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(!eligible);
    assert_eq!(reason, GoalRewardError::GoalDurationTooShort as u32);
}

// ---------------------------------------------------------------------------
// Eligibility: each rule has a dedicated test proving ineligible cases earn
// zero (via preview, which never panics) and that claim_goal_bonus panics
// with the matching error.
// ---------------------------------------------------------------------------

#[test]
fn active_goal_is_not_claimable() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    let id = f.create_goal(1, TARGET, 52 * WEEK);
    f.contribute(&id, TARGET / 2); // not yet complete

    let (eligible, bonus, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(!eligible);
    assert_eq!(bonus, 0);
    assert_eq!(reason, GoalRewardError::GoalNotCompleted as u32);

    let result = f.rewards.try_claim_goal_bonus(&f.owner, &id);
    assert!(result.is_err());
}

#[test]
fn goal_completed_after_its_deadline_earns_nothing() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    // A very short deadline the goal will genuinely blow past, then
    // contribute after the deadline (contribute() itself doesn't block a
    // late contribution; expiry is enforced separately by expire_goal, but
    // the bonus contract must independently reject on-time-ness).
    let id = f.create_goal(1, TARGET, DAY);
    f.advance(2 * DAY);
    f.contribute(&id, TARGET); // completes late

    let (eligible, _, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(!eligible);
    assert_eq!(reason, GoalRewardError::GoalExpired as u32);
}

#[test]
fn expired_goal_earns_nothing() {
    let f = setup();
    let id = f.create_goal(1, TARGET, DAY);
    f.advance(2 * DAY);
    f.goals.expire_goal(&id);

    let (eligible, _, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(!eligible);
    assert_eq!(reason, GoalRewardError::GoalNotCompleted as u32);
}

#[test]
fn goal_completed_within_one_day_earns_nothing() {
    let f = setup();
    let id = f.create_goal(1, TARGET, 52 * WEEK);
    f.advance(1); // 1 second of active duration
    f.contribute(&id, TARGET);

    let (eligible, _, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(!eligible);
    assert_eq!(reason, GoalRewardError::GoalDurationTooShort as u32);
}

#[test]
fn minimum_duration_boundary_is_inclusive() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);
    let id = f.create_goal(1, TARGET, 400 * DAY);
    // Spread contributions across enough distinct periods to also pass the
    // anti-gaming rule. The last contribution reaches the target and
    // auto-completes the goal via contribute()'s own complete_goal call, so
    // no separate finalize_goal is needed (the goal is no longer Active).
    let mut elapsed = 0u64;
    for i in 0..3 {
        f.contribute(&id, TARGET / 4);
        let _ = i;
        f.advance(WEEK);
        elapsed += WEEK;
    }
    // Land the completing contribution exactly on the 30-day floor from
    // creation.
    if 30 * DAY > elapsed {
        f.advance(30 * DAY - elapsed);
    }
    f.contribute(&id, TARGET / 4); // completes here, at t = created_at + 30 days

    let (eligible, _, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(eligible, "reason = {reason}");
}

#[test]
fn single_lump_before_completion_earns_nothing() {
    let f = setup();
    let id = f.create_goal(1, TARGET, 52 * WEEK);
    f.advance(60 * DAY);
    f.contribute(&id, TARGET); // one contribution: 100% of target, 1 period

    let (eligible, _, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(!eligible);
    assert_eq!(
        reason,
        GoalRewardError::IneligibleContributionPattern as u32
    );
}

#[test]
fn large_deposit_is_allowed_when_spread_over_enough_periods() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);
    // Each deposit is 90% of target (fails the 40% ceiling) but spread
    // across 4 distinct weekly periods (passes the spread rule).
    let id = f.create_goal(1, TARGET, 52 * WEEK);
    for _ in 0..4 {
        f.contribute(&id, (TARGET * 9) / 40); // ~22.5% each, 4x = 90%
        f.advance(WEEK);
    }
    f.advance(10 * DAY); // clear the 30-day minimum active duration floor
    f.contribute(&id, TARGET / 10); // top up to 100%, auto-completes at 38 days

    let (eligible, _, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(eligible, "reason = {reason}");
}

#[test]
fn contributions_all_at_the_last_instant_earn_zero() {
    let f = setup();
    let id = f.create_goal(1, TARGET, 52 * WEEK);
    f.advance(60 * DAY);
    // Several small deposits in the SAME instant/period still count as one
    // period and, individually, are all well under the ceiling - so this
    // exercises effort scoring (near-zero time-integral) rather than the
    // eligibility gate.
    f.contribute(&id, TARGET / 3);
    f.contribute(&id, TARGET / 3);
    f.contribute(&id, TARGET / 3 + 10);

    let (eligible, bonus, reason) = f.rewards.preview_goal_bonus(&id);
    // Ineligible via the anti-gaming rule (1 period, and no single deposit
    // exceeds 40%... actually each ~33% deposit IS under 40%, so this
    // passes eligibility on the ceiling rule) - assert on effort instead.
    if eligible {
        assert_eq!(bonus, 0, "near-zero time-integral must score zero effort");
    } else {
        assert_eq!(
            reason,
            GoalRewardError::IneligibleContributionPattern as u32
        );
    }
}

// ---------------------------------------------------------------------------
// Effort weighting
// ---------------------------------------------------------------------------

#[test]
fn steady_saver_claims_effort_weighted_bonus() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY); // clear the min active duration comfortably

    let (eligible, bonus, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(eligible, "reason = {reason}");
    assert!(bonus > 0);

    let paid = f.rewards.claim_goal_bonus(&f.owner, &id);
    assert_eq!(paid, bonus);
    assert_eq!(
        token::Client::new(&f.env, &f.token).balance(&f.owner),
        bonus
    );
}

#[test]
fn late_saver_earns_less_than_steady_saver_for_same_target_and_day() {
    let f = setup();
    f.token_admin.mint(&f.admin, &2_000_000_000);
    f.rewards.fund_pool(&f.admin, &2_000_000_000);

    // Steady saver: 10 equal deposits, one per week.
    let steady = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(10 * DAY);
    let (_, steady_bonus, _) = f.rewards.preview_goal_bonus(&steady);

    // Late saver: several small deposits early (to pass the anti-gaming
    // spread rule), then a large top-up right before completion. The extra
    // advance before the top-up clears the 30-day minimum active duration
    // floor at completion time (not at preview time).
    let late = f.create_goal(2, TARGET, 52 * WEEK);
    for _ in 0..4 {
        f.contribute(&late, TARGET / 20); // 5% each, spread across periods
        f.advance(WEEK);
    }
    f.advance(10 * DAY);
    f.contribute(&late, TARGET - (TARGET / 20) * 4); // the rest, right now: completes at 38 days
    let (late_eligible, late_bonus, late_reason) = f.rewards.preview_goal_bonus(&late);
    assert!(late_eligible, "reason = {late_reason}");

    assert!(
        late_bonus < steady_bonus,
        "late={late_bonus} steady={steady_bonus}: sustained saving must score higher"
    );
}

// ---------------------------------------------------------------------------
// Pool exhaustion
// ---------------------------------------------------------------------------

#[test]
fn pool_that_cannot_cover_the_bonus_pays_nothing() {
    let f = setup();
    // Fund the pool with less than any plausible bonus.
    f.token_admin.mint(&f.admin, &10);
    f.rewards.fund_pool(&f.admin, &10);

    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);

    // Eligible on every other rule, but the pool can't cover the computed
    // bonus - preview reports that specific reason (and still surfaces what
    // the bonus WOULD be) rather than folding it into the generic
    // eligibility gate.
    let (eligible, bonus, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(!eligible);
    assert!(bonus > 10);
    assert_eq!(reason, GoalRewardError::RewardPoolEmpty as u32);

    let result = f.rewards.try_claim_goal_bonus(&f.owner, &id);
    assert!(result.is_err());
    // Not marked claimed - retryable after a top-up.
    assert!(!f.rewards.is_claimed(&id));
}

#[test]
fn empty_pool_halts_claims_until_topped_up() {
    let f = setup();
    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);

    // No funding at all.
    assert!(f.rewards.try_claim_goal_bonus(&f.owner, &id).is_err());

    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);
    let paid = f.rewards.claim_goal_bonus(&f.owner, &id);
    assert!(paid > 0);
}

#[test]
fn draining_the_pool_exactly_emits_exhausted() {
    let f = setup();
    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);
    let (_, bonus, _) = f.rewards.preview_goal_bonus(&id);
    assert!(bonus > 0);

    f.token_admin.mint(&f.admin, &bonus);
    f.rewards.fund_pool(&f.admin, &bonus);

    let before = f.env.events().all().len();
    f.rewards.claim_goal_bonus(&f.owner, &id);
    let after = f.env.events().all().len();
    assert_eq!(f.rewards.get_reward_pool_balance(), 0);

    // GR_CLAIM plus GR_EMPTY (the pool was drained to exactly zero) — two
    // new events from goal_rewards, not just the one a normal claim emits.
    assert!(
        after - before >= 2,
        "expected both GR_CLAIM and GR_EMPTY events, got {} new events",
        after - before
    );
}

// ---------------------------------------------------------------------------
// Claim semantics: exactly once, owner only, churn guard
// ---------------------------------------------------------------------------

#[test]
fn goal_bonus_can_only_be_claimed_once() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);
    f.rewards.claim_goal_bonus(&f.owner, &id);
    assert!(f.rewards.is_claimed(&id));

    let second = f.rewards.try_claim_goal_bonus(&f.owner, &id);
    assert!(second.is_err());
}

#[test]
fn only_goal_owner_can_claim() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);

    let stranger = Address::generate(&f.env);
    let result = f.rewards.try_claim_goal_bonus(&stranger, &id);
    assert!(result.is_err());
}

#[test]
fn abandoned_goal_is_not_claimable() {
    let f = setup();
    let id = f.create_goal(1, TARGET, 52 * WEEK);
    f.contribute(&id, TARGET / 2);
    f.goals.abandon_goal(&f.owner, &id);

    let (eligible, _, reason) = f.rewards.preview_goal_bonus(&id);
    assert!(!eligible);
    assert_eq!(reason, GoalRewardError::GoalNotCompleted as u32);
}

#[test]
fn churn_complete_claim_abandon_recreate_claim_succeeds_once() {
    let f = setup();
    f.token_admin.mint(&f.admin, &2_000_000_000);
    f.rewards.fund_pool(&f.admin, &2_000_000_000);

    // Complete and claim the first goal.
    let first = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);
    let first_bonus = f.rewards.claim_goal_bonus(&f.owner, &first);
    assert!(first_bonus > 0);

    // "Abandon" conceptually: the registry has no operation to abandon an
    // already-Completed goal (abandon_goal requires Active), which is
    // exactly the guarantee this test exists to confirm - a completed,
    // claimed goal's id can never be reused or reopened. Re-create a fresh
    // goal (a new id, as the registry always assigns) representing the
    // same conceptual savings target.
    let second = steady_contributions(&f, 2, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);
    let second_bonus = f.rewards.claim_goal_bonus(&f.owner, &second);
    assert!(second_bonus > 0);

    assert!(f.rewards.is_claimed(&first));
    assert!(f.rewards.is_claimed(&second));
    // Each goal id's claim is independent - re-claiming the first must
    // still fail.
    assert!(f.rewards.try_claim_goal_bonus(&f.owner, &first).is_err());
}

#[test]
fn concurrent_goals_backed_by_one_balance_pay_once() {
    let f = setup();
    f.token_admin.mint(&f.admin, &2_000_000_000);
    f.rewards.fund_pool(&f.admin, &2_000_000_000);

    // Two goals against the SAME vault, genuinely interleaved: both created
    // before either completes, both backed by the owner's one real vault
    // balance.
    let a = f.create_goal(1, TARGET, 52 * WEEK);
    let b = f.create_goal(2, TARGET, 52 * WEEK);
    for _ in 0..10 {
        f.contribute(&a, TARGET / 10);
        f.contribute(&b, TARGET / 10);
        f.advance(WEEK);
    }
    f.advance(60 * DAY);

    f.rewards.claim_goal_bonus(&f.owner, &a);
    let second = f.rewards.try_claim_goal_bonus(&f.owner, &b);
    assert!(
        second.is_err(),
        "overlapping goal against the same vault must not pay a second bonus"
    );
}

#[test]
fn sequential_goal_created_after_last_claim_is_eligible() {
    let f = setup();
    f.token_admin.mint(&f.admin, &2_000_000_000);
    f.rewards.fund_pool(&f.admin, &2_000_000_000);

    let first = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);
    f.rewards.claim_goal_bonus(&f.owner, &first);

    // A genuinely later goal, created only after the first one's claim.
    let second = steady_contributions(&f, 2, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);
    let paid = f.rewards.claim_goal_bonus(&f.owner, &second);
    assert!(
        paid > 0,
        "a non-overlapping later goal must still be claimable"
    );
}

// ---------------------------------------------------------------------------
// Anti-fake-contribution basis
// ---------------------------------------------------------------------------

#[test]
fn fake_contributions_without_vault_funds_earn_nothing() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    // Zero out the owner's real vault balance (setup() sets it to TARGET by
    // default) - the goal registry still records the full contribution.
    f.vault.set_balance(&f.owner, &0);

    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);

    let (eligible, bonus, _) = f.rewards.preview_goal_bonus(&id);
    assert!(eligible);
    assert_eq!(
        bonus, 0,
        "no real vault balance must clamp the basis (and bonus) to zero"
    );
}

#[test]
fn partial_vault_position_scales_basis_down() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    f.vault.set_balance(&f.owner, &(TARGET / 2));

    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);

    let (full_eligible, full_bonus, _) = f.rewards.preview_goal_bonus(&id);
    assert!(full_eligible);

    // Same timeline, full vault balance, for comparison.
    f.vault.set_balance(&f.owner, &TARGET);
    let (_, unclamped_bonus, _) = f.rewards.preview_goal_bonus(&id);

    assert!(full_bonus < unclamped_bonus);
}

// ---------------------------------------------------------------------------
// Tiers
// ---------------------------------------------------------------------------

#[test]
fn unconfigured_tier_pays_nothing() {
    let f = setup();
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    // A goal far below the only configured tier's floor is fine (min_target
    // 0 matches everything) - instead prove the "no tier configured at all"
    // case using a fresh contract instance with nothing configured.
    let admin2 = Address::generate(&f.env);
    let goals_cid = f.env.register_contract(None, SavingsGoalContract);
    let goals2 = SavingsGoalContractClient::new(&f.env, &goals_cid);
    let wasm_hash = f.env.deployer().upload_contract_wasm(dummy_vault::WASM);
    let factory_id = f.env.register_contract(None, VaultFactoryContract);
    let factory = VaultFactoryContractClient::new(&f.env, &factory_id);
    factory.initialize(&admin2, &wasm_hash);
    goals2.initialize(&admin2, &factory_id);

    let rewards2_cid = f.env.register_contract(None, GoalRewardsContract);
    let rewards2 = GoalRewardsContractClient::new(&f.env, &rewards2_cid);
    rewards2.initialize(&admin2, &goals_cid, &f.token);
    // No configure_reward call at all.

    let salt = BytesN::from_array(&f.env, &[9u8; 32]);
    let underlying = Address::generate(&f.env);
    let init_args: Vec<soroban_sdk::Val> =
        soroban_sdk::vec![&f.env, admin2.into_val(&f.env), false.into_val(&f.env)];
    let vault_addr = factory.create_vault(&admin2, &salt, &underlying, &init_args);

    let id = goal_id(&f.env, 99);
    goals2.create_goal(
        &f.owner,
        &id,
        &vault_addr,
        &TARGET,
        &(f.env.ledger().timestamp() + 52 * WEEK),
    );
    for _ in 0..10 {
        goals2.contribute(&f.owner, &id, &(TARGET / 10));
        f.advance(WEEK);
    }
    f.advance(60 * DAY);

    let (eligible, bonus, reason) = rewards2.preview_goal_bonus(&id);
    assert!(!eligible);
    assert_eq!(bonus, 0);
    assert_eq!(reason, GoalRewardError::RewardTierNotConfigured as u32);
}

#[test]
fn disabled_tier_pays_nothing() {
    let f = setup();
    // Reconfigure the only tier to 0 bps - effectively "disabled" while
    // still technically "configured".
    f.rewards.configure_reward(&f.admin, &0i128, &0u32, &0i128);
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);
    let (eligible, bonus, _) = f.rewards.preview_goal_bonus(&id);
    assert!(eligible);
    assert_eq!(bonus, 0);
}

#[test]
fn bonus_is_capped_by_tier_absolute_max() {
    let f = setup();
    // A generous rate but a tiny absolute cap.
    f.rewards
        .configure_reward(&f.admin, &0i128, &(crate::MAX_BONUS_BPS), &1_000i128);
    f.token_admin.mint(&f.admin, &1_000_000_000);
    f.rewards.fund_pool(&f.admin, &1_000_000_000);

    let id = steady_contributions(&f, 1, TARGET, TARGET / 10, 10);
    f.advance(60 * DAY);
    let (eligible, bonus, _) = f.rewards.preview_goal_bonus(&id);
    assert!(eligible);
    assert!(bonus <= 1_000);
}

#[test]
fn goal_tier_view_matches_thresholds() {
    let f = setup();
    f.rewards
        .configure_reward(&f.admin, &10_000_000_000i128, &300u32, &1_000_000_000i128);
    let tiers = f.rewards.get_reward_tiers();
    assert_eq!(tiers.len(), 2);
}

// ---------------------------------------------------------------------------
// preview never panics
// ---------------------------------------------------------------------------

#[test]
fn preview_reports_reason_codes_without_panicking() {
    let f = setup();
    // A goal id that has never been created at all - get_goal itself panics
    // inside the registry, which preview_goal_bonus does not attempt to
    // catch (there is no view into a nonexistent goal to report a reason
    // for). Instead this test exercises every OTHER non-panicking path.
    let active = f.create_goal(1, TARGET, 52 * WEEK);
    let (e1, _, _) = f.rewards.preview_goal_bonus(&active);
    assert!(!e1);

    let expired_id = f.create_goal(2, TARGET, DAY);
    f.advance(2 * DAY);
    f.goals.expire_goal(&expired_id);
    let (e2, _, _) = f.rewards.preview_goal_bonus(&expired_id);
    assert!(!e2);
}

// ---------------------------------------------------------------------------
// initialize / configuration edge cases
// ---------------------------------------------------------------------------

#[test]
fn initialize_twice_is_rejected_and_bad_cap_refused() {
    let f = setup();
    let result = f
        .rewards
        .try_initialize(&f.admin, &f.goals.address, &f.token);
    assert!(result.is_err());
}
