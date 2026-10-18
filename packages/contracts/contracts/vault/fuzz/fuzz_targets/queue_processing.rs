#![no_main]

use arbitrary::Arbitrary;
use libfuzzer_sys::fuzz_target;
use vault_contract::{
    conversion::shares_to_assets_down,
    queue::{
        compute_entry_fill, compute_round_cap, MAX_FILL_SHARE_BPS_CEILING, MAX_PROCESS_ENTRIES,
        MIN_QUEUE_REQUEST_SHARES,
    },
};

#[derive(Debug, Arbitrary, Clone, Copy)]
struct RawQueueEntry {
    shares_requested: u64,
    cancelled: bool,
}

#[derive(Debug, Arbitrary)]
struct QueueFuzzInput {
    entries: Vec<RawQueueEntry>,
    available_liquidity: u64,
    max_entries: u8,
    fill_bps: u16,
    total_assets: u64,
    total_shares: u64,
}

fuzz_target!(|input: QueueFuzzInput| {
    let available_liquidity = (input.available_liquidity as i128).max(0);
    let fill_bps = ((input.fill_bps as u32) % MAX_FILL_SHARE_BPS_CEILING).max(1);
    let round_cap = compute_round_cap(available_liquidity, fill_bps);
    assert!(
        round_cap >= 0 && round_cap <= available_liquidity,
        "round_cap {round_cap} out of [0, {available_liquidity}]"
    );

    let total_shares = (input.total_shares as i128).max(1);
    let total_assets = (input.total_assets as i128).max(total_shares);
    let shares_to_assets =
        |s: i128| shares_to_assets_down(s, total_assets, total_shares).unwrap_or(0);

    // Build FIFO sequence of active entries (simulating gaps from cancellations)
    let mut queue: Vec<(u64, i128)> = Vec::new();
    let mut seq: u64 = 1;
    for raw in input.entries.iter().take(64) {
        let req = (raw.shares_requested as i128).max(MIN_QUEUE_REQUEST_SHARES);
        if !raw.cancelled {
            queue.push((seq, req));
        }
        seq += 1;
    }

    let capped_max_entries = (input.max_entries as usize)
        .max(1)
        .min(MAX_PROCESS_ENTRIES as usize);
    let mut remaining_liquidity = available_liquidity;
    let mut touched = 0usize;
    let mut prev_seq: Option<u64> = None;
    let mut total_assets_disbursed: i128 = 0;

    for &(entry_seq, remaining_shares) in &queue {
        if touched >= capped_max_entries || remaining_liquidity <= 0 {
            break;
        }
        let (fill_shares, fill_assets, fully_filled) = compute_entry_fill(
            remaining_shares,
            round_cap,
            remaining_liquidity,
            shares_to_assets,
        );
        touched += 1;

        if fill_shares <= 0 || fill_assets <= 0 {
            continue;
        }

        // Strict FIFO sequence ordering
        if let Some(p) = prev_seq {
            assert!(
                entry_seq > p,
                "FIFO ordering violated: seq {entry_seq} after {p}"
            );
        }
        prev_seq = Some(entry_seq);

        assert!(
            fill_shares <= remaining_shares,
            "fill_shares {fill_shares} > remaining_shares {remaining_shares}"
        );
        assert!(
            fill_assets <= round_cap && fill_assets <= remaining_liquidity,
            "fill_assets {fill_assets} exceeded round_cap {round_cap} or remaining_liquidity {remaining_liquidity}"
        );
        assert_eq!(fully_filled, fill_shares >= remaining_shares);

        remaining_liquidity -= fill_assets;
        total_assets_disbursed += fill_assets;
    }

    assert!(
        total_assets_disbursed <= available_liquidity,
        "disbursed {total_assets_disbursed} > available_liquidity {available_liquidity}"
    );
    assert!(touched <= capped_max_entries);
});
