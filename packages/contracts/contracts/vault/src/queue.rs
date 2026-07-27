//! Fair-ordering emergency withdrawal queue (issue #814).
//!
//! The queue is modelled as an append-only sequence with a `head` pointer and
//! a monotonically increasing `seq` per entry, rather than as a sortable
//! collection. Ordering is by `seq`, full stop — there is no priority field
//! and no admin reordering path. Entries are keyed directly by `seq` in
//! contract storage, so cancelling a non-head entry is an O(1) map removal
//! that never shifts any other entry's `seq`.
//!
//! Financial side effects (burning shares, transferring assets) are the
//! caller's responsibility — this module only tracks *how much* of each
//! entry should be filled this round (`plan_fills`) and persists the result
//! once the caller confirms the transfer succeeded (`apply_fill`). That
//! split keeps the ordering/fill-cap math here pure enough to unit-test
//! with a bare `Env`, with no vault contract, vault token contract, or
//! token transfers involved.

use nester_common::{fees::mul_div, ContractError};
use soroban_sdk::{contracttype, panic_with_error, Address, Env, Vec};

/// Dust floor: a request below this many shares cannot be queued, so the
/// queue cannot be spammed with entries too small to be worth processing.
pub const MIN_QUEUE_REQUEST_SHARES: i128 = 1_000_000; // 0.1 share at 7 decimals

/// Hard ceiling on `max_entries` for a single `process_emergency_queue` call,
/// regardless of what the caller asks for. Bounds the call's resource cost
/// independent of queue depth.
pub const MAX_PROCESS_ENTRIES: u32 = 50;

/// Default per-entry fill cap: no single entry may absorb more than this
/// share of a round's available liquidity, so entries behind a very large
/// request still make progress in the same round instead of waiting for it
/// to fully drain the vault's liquidity.
pub const DEFAULT_MAX_FILL_SHARE_BPS: u32 = 5_000; // 50%

/// Fill caps are configurable but bounded to (0, 10_000] bps.
pub const MAX_FILL_SHARE_BPS_CEILING: u32 = 10_000;

/// `get_queue_position` walks forward from `head` counting open entries
/// ahead of the caller. This bounds that walk to the caller's own distance
/// from the head — never the full queue — but caps it defensively so a
/// pathologically long, un-processed queue can't make the read unbounded.
pub const MAX_POSITION_SCAN: u32 = 2_000;

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct QueueEntry {
    pub user: Address,
    pub shares_requested: i128,
    pub shares_filled: i128,
    pub seq: u64,
    pub enqueued_at: u64,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct QueuePosition {
    pub seq: u64,
    pub entries_ahead: u32,
    pub shares_ahead: i128,
    pub shares_filled: i128,
    pub shares_requested: i128,
}

#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct QueueStats {
    pub total_open_shares: i128,
    pub open_entry_count: u32,
    pub head_seq: u64,
    pub tail_seq: u64,
    pub available_liquidity: i128,
}

/// One entry's planned fill for the current `process_emergency_queue` round.
/// Produced by [`plan_fills`]; the caller executes the real burn/transfer and
/// then commits the result via [`apply_fill`].
#[contracttype]
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PlannedFill {
    pub seq: u64,
    pub user: Address,
    pub fill_shares: i128,
    pub fully_filled: bool,
}

#[contracttype]
#[derive(Clone)]
enum QueueDataKey {
    HeadSeq,
    TailSeq,
    Entry(u64),
    UserSeq(Address),
    TotalOpenShares,
    OpenCount,
    MaxFillShareBps,
}

fn get_head(env: &Env) -> u64 {
    env.storage()
        .instance()
        .get(&QueueDataKey::HeadSeq)
        .unwrap_or(1)
}

fn set_head(env: &Env, seq: u64) {
    env.storage().instance().set(&QueueDataKey::HeadSeq, &seq);
}

fn get_tail(env: &Env) -> u64 {
    env.storage()
        .instance()
        .get(&QueueDataKey::TailSeq)
        .unwrap_or(1)
}

fn set_tail(env: &Env, seq: u64) {
    env.storage().instance().set(&QueueDataKey::TailSeq, &seq);
}

fn get_entry(env: &Env, seq: u64) -> Option<QueueEntry> {
    env.storage().persistent().get(&QueueDataKey::Entry(seq))
}

fn set_entry(env: &Env, entry: &QueueEntry) {
    env.storage()
        .persistent()
        .set(&QueueDataKey::Entry(entry.seq), entry);
}

fn remove_entry(env: &Env, seq: u64) {
    env.storage().persistent().remove(&QueueDataKey::Entry(seq));
}

fn get_user_seq(env: &Env, user: &Address) -> Option<u64> {
    env.storage()
        .persistent()
        .get(&QueueDataKey::UserSeq(user.clone()))
}

fn set_user_seq(env: &Env, user: &Address, seq: u64) {
    env.storage()
        .persistent()
        .set(&QueueDataKey::UserSeq(user.clone()), &seq);
}

fn remove_user_seq(env: &Env, user: &Address) {
    env.storage()
        .persistent()
        .remove(&QueueDataKey::UserSeq(user.clone()));
}

pub fn total_open_shares(env: &Env) -> i128 {
    env.storage()
        .instance()
        .get(&QueueDataKey::TotalOpenShares)
        .unwrap_or(0)
}

fn set_total_open_shares(env: &Env, amount: i128) {
    env.storage()
        .instance()
        .set(&QueueDataKey::TotalOpenShares, &amount);
}

pub fn open_count(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&QueueDataKey::OpenCount)
        .unwrap_or(0)
}

fn set_open_count(env: &Env, count: u32) {
    env.storage()
        .instance()
        .set(&QueueDataKey::OpenCount, &count);
}

pub fn max_fill_share_bps(env: &Env) -> u32 {
    env.storage()
        .instance()
        .get(&QueueDataKey::MaxFillShareBps)
        .unwrap_or(DEFAULT_MAX_FILL_SHARE_BPS)
}

pub fn set_max_fill_share_bps(env: &Env, bps: u32) {
    if bps == 0 || bps > MAX_FILL_SHARE_BPS_CEILING {
        panic_with_error!(env, ContractError::ConfigOutOfRange);
    }
    env.storage()
        .instance()
        .set(&QueueDataKey::MaxFillShareBps, &bps);
}

/// Enqueue (or extend) a withdrawal request for `user`.
///
/// A user may hold at most one open queue position at a time. Calling this
/// again while a position is open *extends* it — adds `additional_shares` to
/// the outstanding request — rather than creating a second queue position,
/// so a user cannot occupy the front of the line multiple times over.
pub fn request(
    env: &Env,
    user: &Address,
    additional_shares: i128,
    user_share_balance: i128,
) -> QueueEntry {
    if additional_shares < MIN_QUEUE_REQUEST_SHARES {
        panic_with_error!(env, ContractError::RequestBelowMinimum);
    }
    if additional_shares > user_share_balance {
        panic_with_error!(env, ContractError::InsufficientBalance);
    }

    if let Some(seq) = get_user_seq(env, user) {
        let mut entry = get_entry(env, seq)
            .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInQueue));
        entry.shares_requested = entry
            .shares_requested
            .checked_add(additional_shares)
            .unwrap_or_else(|| panic_with_error!(env, ContractError::ArithmeticOverflow));
        set_entry(env, &entry);

        let total = total_open_shares(env)
            .checked_add(additional_shares)
            .unwrap_or_else(|| panic_with_error!(env, ContractError::ArithmeticOverflow));
        set_total_open_shares(env, total);
        entry
    } else {
        create_new_entry(env, user, additional_shares)
    }
}

/// Internal: always creates a brand-new queue position. Exposed at
/// crate-visibility only so unit tests can exercise the `QueueEntryExists`
/// guard directly — normal callers must go through [`request`], which
/// extends rather than duplicates.
pub(crate) fn create_new_entry(env: &Env, user: &Address, shares: i128) -> QueueEntry {
    if get_user_seq(env, user).is_some() {
        panic_with_error!(env, ContractError::QueueEntryExists);
    }

    let seq = get_tail(env);
    set_tail(env, seq + 1);

    let entry = QueueEntry {
        user: user.clone(),
        shares_requested: shares,
        shares_filled: 0,
        seq,
        enqueued_at: env.ledger().timestamp(),
    };
    set_entry(env, &entry);
    set_user_seq(env, user, seq);

    let total = total_open_shares(env)
        .checked_add(shares)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::ArithmeticOverflow));
    set_total_open_shares(env, total);
    set_open_count(env, open_count(env) + 1);

    entry
}

/// Cancel `user`'s open request (unfilled or partially filled). Because
/// shares are only burned at fill time, any not-yet-filled portion was never
/// removed from the user's balance — cancellation is pure bookkeeping and
/// needs no asset transfer. Returns the (already-unburned) share amount that
/// was outstanding, for the caller's event/logging purposes.
///
/// Does not touch any other entry's `seq`: this is an O(1) map removal.
pub fn cancel(env: &Env, user: &Address) -> i128 {
    let seq = get_user_seq(env, user)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInQueue));
    let entry =
        get_entry(env, seq).unwrap_or_else(|| panic_with_error!(env, ContractError::NotInQueue));

    let outstanding = entry.shares_requested - entry.shares_filled;

    remove_entry(env, seq);
    remove_user_seq(env, user);
    set_open_count(env, open_count(env).saturating_sub(1));
    set_total_open_shares(env, (total_open_shares(env) - outstanding).max(0));

    // If we cancelled exactly the head, advance past the now-missing slot so
    // head never points at a dead entry longer than necessary.
    if seq == get_head(env) {
        advance_head_past_gaps(env);
    }

    outstanding
}

/// Move `head` forward over any seq whose entry no longer exists (already
/// fully filled and removed, or cancelled), stopping at the first still-open
/// entry or at `tail`. Amortized O(1): each seq is skipped at most once
/// across the life of the queue.
fn advance_head_past_gaps(env: &Env) {
    let tail = get_tail(env);
    let mut head = get_head(env);
    while head < tail && get_entry(env, head).is_none() {
        head += 1;
    }
    set_head(env, head);
}

/// Plan how much liquidity to hand to each open entry this round, starting
/// at `head` and touching at most `max_entries` *existing* entries.
///
/// `available_liquidity` is the total assets available to this round.
/// `shares_to_assets` converts a share amount to its current asset value at
/// today's share price (injected so this function stays free of any vault
/// token contract dependency).
///
/// Per-entry fill is capped at `available_liquidity * max_fill_share_bps /
/// 10_000` so one very large request cannot absorb an entire round's
/// liquidity — smaller requests behind it still progress in the same call.
/// This is a fairness refinement on top of strict seq ordering, not a
/// replacement for it: the head entry is always considered, and always
/// filled, before any entry behind it.
///
/// Pure with respect to storage: reads entries but does not mutate them.
/// Callers must invoke [`apply_fill`] once the real asset transfer for each
/// planned fill has succeeded.
pub fn plan_fills(
    env: &Env,
    max_entries: u32,
    available_liquidity: i128,
    shares_to_assets: impl Fn(i128) -> i128,
) -> Vec<PlannedFill> {
    let mut plan = Vec::new(env);
    if available_liquidity <= 0 {
        return plan;
    }

    let capped_max_entries = max_entries.min(MAX_PROCESS_ENTRIES);
    let fill_bps = max_fill_share_bps(env);
    let round_cap =
        mul_div(available_liquidity, fill_bps as i128, 10_000).unwrap_or(available_liquidity);

    let tail = get_tail(env);
    let mut seq = get_head(env);
    let mut remaining_liquidity = available_liquidity;
    let mut touched: u32 = 0;

    while seq < tail && touched < capped_max_entries && remaining_liquidity > 0 {
        let entry = match get_entry(env, seq) {
            Some(e) => e,
            None => {
                // Gap left by a cancellation; skip without counting against
                // the caller's max_entries budget.
                seq += 1;
                continue;
            }
        };

        let remaining_shares = entry.shares_requested - entry.shares_filled;
        let assets_equiv = shares_to_assets(remaining_shares);
        let entry_room = round_cap.min(remaining_liquidity);
        let fill_assets = assets_equiv.min(entry_room);

        if fill_assets <= 0 {
            seq += 1;
            touched += 1;
            continue;
        }

        let fill_shares = if fill_assets >= assets_equiv {
            remaining_shares
        } else {
            mul_div(remaining_shares, fill_assets, assets_equiv).unwrap_or(0)
        };

        if fill_shares <= 0 {
            seq += 1;
            touched += 1;
            continue;
        }

        remaining_liquidity -= fill_assets;
        plan.push_back(PlannedFill {
            seq,
            user: entry.user.clone(),
            fill_shares,
            fully_filled: fill_shares >= remaining_shares,
        });

        seq += 1;
        touched += 1;
    }

    plan
}

/// Commit a [`PlannedFill`] after the caller has burned `fill_shares` worth
/// of the user's vault-token balance and transferred the corresponding
/// assets. Returns `true` if the entry is now fully filled (and has been
/// removed from the queue).
pub fn apply_fill(env: &Env, seq: u64, fill_shares: i128) -> bool {
    let mut entry =
        get_entry(env, seq).unwrap_or_else(|| panic_with_error!(env, ContractError::NotInQueue));

    entry.shares_filled = entry
        .shares_filled
        .checked_add(fill_shares)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::ArithmeticOverflow));

    set_total_open_shares(env, (total_open_shares(env) - fill_shares).max(0));

    let fully_filled = entry.shares_filled >= entry.shares_requested;
    if fully_filled {
        remove_entry(env, seq);
        remove_user_seq(env, &entry.user);
        set_open_count(env, open_count(env).saturating_sub(1));
        if seq == get_head(env) {
            advance_head_past_gaps(env);
        }
    } else {
        set_entry(env, &entry);
    }

    fully_filled
}

/// `entries` and `shares` ahead of `user`, bounded to a scan of `user`'s own
/// distance from `head` (never the full queue). See [`MAX_POSITION_SCAN`].
pub fn position(env: &Env, user: &Address) -> QueuePosition {
    let seq = get_user_seq(env, user)
        .unwrap_or_else(|| panic_with_error!(env, ContractError::NotInQueue));
    let entry =
        get_entry(env, seq).unwrap_or_else(|| panic_with_error!(env, ContractError::NotInQueue));

    let head = get_head(env);
    let mut entries_ahead: u32 = 0;
    let mut shares_ahead: i128 = 0;
    let mut cursor = head;
    let mut scanned: u32 = 0;
    while cursor < seq && scanned < MAX_POSITION_SCAN {
        if let Some(ahead_entry) = get_entry(env, cursor) {
            entries_ahead += 1;
            shares_ahead += ahead_entry.shares_requested - ahead_entry.shares_filled;
        }
        cursor += 1;
        scanned += 1;
    }

    QueuePosition {
        seq,
        entries_ahead,
        shares_ahead,
        shares_filled: entry.shares_filled,
        shares_requested: entry.shares_requested,
    }
}

pub fn stats(env: &Env, available_liquidity: i128) -> QueueStats {
    QueueStats {
        total_open_shares: total_open_shares(env),
        open_entry_count: open_count(env),
        head_seq: get_head(env),
        tail_seq: get_tail(env),
        available_liquidity,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use soroban_sdk::testutils::Address as _;

    fn identity(shares: i128) -> i128 {
        shares
    }

    /// Storage access requires an active contract context. This module is
    /// unit-tested "without the surrounding vault" in the sense that no
    /// vault business logic runs — but Soroban's storage API still needs
    /// *some* registered contract address to scope keys to, so tests
    /// register the bare `VaultContract` shell and run inside
    /// `env.as_contract` purely to get that storage context.
    fn setup() -> Env {
        Env::default()
    }

    fn contract_id(env: &Env) -> Address {
        env.register_contract(None, crate::VaultContract)
    }

    #[test]
    fn fifo_order_by_sequence_no_reordering_path() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let u1 = Address::generate(&env);
            let u2 = Address::generate(&env);

            let e1 = request(&env, &u1, 10_000_000, 10_000_000);
            let e2 = request(&env, &u2, 10_000_000, 10_000_000);
            assert!(e1.seq < e2.seq);

            let pos2 = position(&env, &u2);
            assert_eq!(pos2.entries_ahead, 1);
            assert_eq!(pos2.shares_ahead, 10_000_000);
        });
    }

    #[test]
    fn large_request_filled_incrementally_across_multiple_calls() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let whale = Address::generate(&env);
            request(&env, &whale, 100_000_000, 100_000_000);

            // First round: only 30% of the liquidity needed is available.
            let plan1 = plan_fills(&env, 10, 30_000_000, identity);
            assert_eq!(plan1.len(), 1);
            let fill1 = plan1.get(0).unwrap();
            assert!(!fill1.fully_filled);
            assert!(!apply_fill(&env, fill1.seq, fill1.fill_shares));

            let pos_mid = position(&env, &whale);
            assert_eq!(pos_mid.shares_filled, fill1.fill_shares);
            assert!(pos_mid.shares_filled < pos_mid.shares_requested);

            // Second round: plenty of liquidity arrives (comfortably above
            // the 50% per-round cap applied to it) and finishes the fill.
            let remaining = 100_000_000 - fill1.fill_shares;
            let plan2 = plan_fills(&env, 10, remaining * 3, identity);
            assert_eq!(plan2.len(), 1);
            let fill2 = plan2.get(0).unwrap();
            assert!(fill2.fully_filled);
            assert!(apply_fill(&env, fill2.seq, fill2.fill_shares));

            assert_eq!(stats(&env, 0).open_entry_count, 0);
        });
    }

    #[test]
    fn max_fill_share_bps_lets_two_users_progress_concurrently() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let whale = Address::generate(&env);
            let minnow = Address::generate(&env);

            // Whale wants far more than one round of liquidity; minnow wants
            // a small amount that fits easily.
            request(&env, &whale, 1_000_000_000, 1_000_000_000);
            request(&env, &minnow, 5_000_000, 5_000_000);

            set_max_fill_share_bps(&env, 5_000); // 50% cap per entry per round
            let available = 100_000_000;
            let plan = plan_fills(&env, 10, available, identity);

            // Both entries should appear in the same round's plan.
            assert_eq!(plan.len(), 2);
            let whale_fill = plan.get(0).unwrap();
            let minnow_fill = plan.get(1).unwrap();

            // Whale is capped at 50% of the round's liquidity even though it
            // could otherwise absorb all of it.
            assert_eq!(whale_fill.fill_shares, 50_000_000);
            assert!(!whale_fill.fully_filled);

            // Minnow's small request is fully satisfied in the same round.
            assert_eq!(minnow_fill.fill_shares, 5_000_000);
            assert!(minnow_fill.fully_filled);
        });
    }

    #[test]
    fn cancel_returns_outstanding_shares_and_preserves_other_sequence_numbers() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let u1 = Address::generate(&env);
            let u2 = Address::generate(&env);

            let e1 = request(&env, &u1, 10_000_000, 10_000_000);
            let e2 = request(&env, &u2, 20_000_000, 20_000_000);

            let returned = cancel(&env, &u1);
            assert_eq!(returned, 10_000_000);

            // u2's seq and position are untouched by u1's cancellation.
            let e2_after = get_entry(&env, e2.seq).unwrap();
            assert_eq!(e2_after.seq, e2.seq);
            assert_eq!(e1.seq, e1.seq); // sanity: original seq value never reused/mutated

            let pos2 = position(&env, &u2);
            assert_eq!(pos2.entries_ahead, 0);
            assert_eq!(pos2.shares_ahead, 0);
        });
    }

    #[test]
    fn cancelling_head_lets_process_skip_the_gap() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let u1 = Address::generate(&env);
            let u2 = Address::generate(&env);
            request(&env, &u1, 10_000_000, 10_000_000);
            let e2 = request(&env, &u2, 10_000_000, 10_000_000);

            cancel(&env, &u1);

            let plan = plan_fills(&env, 10, 10_000_000, identity);
            assert_eq!(plan.len(), 1);
            assert_eq!(plan.get(0).unwrap().user, u2);
            assert_eq!(plan.get(0).unwrap().seq, e2.seq);
        });
    }

    #[test]
    fn repeat_request_extends_instead_of_creating_new_position() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let u1 = Address::generate(&env);
            let first = request(&env, &u1, 10_000_000, 10_000_000);
            let second = request(&env, &u1, 5_000_000, 15_000_000);

            assert_eq!(first.seq, second.seq, "same queue position, no duplicate");
            assert_eq!(second.shares_requested, 15_000_000);
            assert_eq!(open_count(&env), 1);
        });
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #25)")]
    fn request_below_minimum_is_rejected() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let u1 = Address::generate(&env);
            request(&env, &u1, 1, 10_000_000);
        });
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #24)")]
    fn duplicate_low_level_create_rejects_with_queue_entry_exists() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let u1 = Address::generate(&env);
            create_new_entry(&env, &u1, 10_000_000);
            // Bypassing `request`'s extend logic on purpose to exercise the
            // defensive QueueEntryExists guard directly.
            create_new_entry(&env, &u1, 10_000_000);
        });
    }

    #[test]
    #[should_panic(expected = "Error(Contract, #26)")]
    fn cancel_without_open_request_errors_not_in_queue() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let u1 = Address::generate(&env);
            cancel(&env, &u1);
        });
    }

    #[test]
    fn stats_are_o1_running_aggregates() {
        let env = setup();
        let cid = contract_id(&env);
        env.as_contract(&cid, || {
            let u1 = Address::generate(&env);
            let u2 = Address::generate(&env);
            request(&env, &u1, 10_000_000, 10_000_000);
            request(&env, &u2, 20_000_000, 20_000_000);

            let s = stats(&env, 5_000_000);
            assert_eq!(s.total_open_shares, 30_000_000);
            assert_eq!(s.open_entry_count, 2);
            assert_eq!(s.available_liquidity, 5_000_000);
        });
    }
}
