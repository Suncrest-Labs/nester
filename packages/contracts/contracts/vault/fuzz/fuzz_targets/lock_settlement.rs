#![no_main]

use arbitrary::Arbitrary;
use libfuzzer_sys::fuzz_target;
use nester_common::fees::{
    boosted_shares_after_yield, lock_break_penalty_bps, split_penalty, BASIS_POINT_SCALE,
};
use vault_contract::locks::total_weight;

#[derive(Debug, Arbitrary)]
struct LockPositionInput {
    shares: u64,
    boost_bps_offset: u16,
}

#[derive(Debug, Arbitrary)]
struct LockSettlementInput {
    flexible_shares: u64,
    positions: Vec<LockPositionInput>,
    total_assets: u64,
    yield_amount: u64,
    escrow: i64,
    depositor_share_bps: u16,
    full_penalty_bps: u16,
    elapsed_secs: u64,
    total_duration_secs: u64,
}

fuzz_target!(|input: LockSettlementInput| {
    // 1. Penalty escrow split conservation: depositor + treasury + dust == escrow
    let escrow = input.escrow as i128;
    let dep_bps = (input.depositor_share_bps as u32).min(BASIS_POINT_SCALE as u32);
    let (dep_slice, treas_slice, dust) = split_penalty(escrow, dep_bps);
    if escrow <= 0 {
        assert_eq!((dep_slice, treas_slice, dust), (0, 0, 0));
    } else {
        assert!(dep_slice >= 0 && treas_slice >= 0 && dust >= 0);
        assert_eq!(
            dep_slice + treas_slice + dust,
            escrow,
            "split_penalty failed conservation for escrow={escrow}, bps={dep_bps}"
        );
        assert!(dust <= 2, "split_penalty rounding dust {dust} > 2");
    }

    // 2. Early-break penalty linear decay bounds and monotonicity
    let full_pen = (input.full_penalty_bps as u32).min(BASIS_POINT_SCALE as u32);
    let pen = lock_break_penalty_bps(full_pen, input.elapsed_secs, input.total_duration_secs);
    assert!(
        pen <= full_pen,
        "lock_break_penalty_bps {pen} exceeded full_penalty_bps {full_pen}"
    );
    if input.elapsed_secs < u64::MAX {
        let pen_later = lock_break_penalty_bps(
            full_pen,
            input.elapsed_secs + 1,
            input.total_duration_secs,
        );
        assert!(
            pen_later <= pen,
            "penalty increased as time elapsed: {pen} -> {pen_later}"
        );
    }

    // 3. Yield boost settlement invariants
    let flex = (input.flexible_shares as i128).max(1);
    let mut locked_shares_sum: i128 = 0;
    let mut locked_weight_sum: i128 = 0;
    let mut locks: Vec<(i128, u32)> = Vec::new();

    for pos in input.positions.iter().take(16) {
        let s = (pos.shares as i128).max(1);
        let boost = (BASIS_POINT_SCALE as u32) + ((pos.boost_bps_offset as u32) % 20_001);
        locked_shares_sum = locked_shares_sum.saturating_add(s);
        locked_weight_sum = locked_weight_sum.saturating_add(s * (boost as i128));
        locks.push((s, boost));
    }

    let total_supply = flex.saturating_add(locked_shares_sum);
    let total_assets = (input.total_assets as i128).max(total_supply);
    let yield_amount = (input.yield_amount as i128).max(0);

    if let Ok(w) = total_weight(total_supply, locked_shares_sum, locked_weight_sum) {
        // 1x boost must never mint extra shares
        if let Ok(unboosted) = boosted_shares_after_yield(
            flex,
            BASIS_POINT_SCALE as u32,
            total_assets,
            total_supply,
            w,
            yield_amount,
        ) {
            assert_eq!(
                unboosted, flex,
                "1x boost position changed share count: {flex} -> {unboosted}"
            );
        }

        for &(s, boost) in &locks {
            if let Ok(new_s) = boosted_shares_after_yield(
                s,
                boost,
                total_assets,
                total_supply,
                w,
                yield_amount,
            ) {
                assert!(
                    new_s >= s,
                    "boosted position lost shares on positive yield: {s} -> {new_s}"
                );
            }
        }
    }
});
