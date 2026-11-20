# Mainnet RPC Read-Only Indexer Soak Runbook

## Problem

The event indexer (`apps/api/internal/stellar/poller.go` and `indexer.go`) has only been exercised against testnet RPC. Mainnet Soroban RPC providers differ in:

- **Retention window:** approximately 7 days on `https://mainnet.sorobanrpc.com` (compared to shorter/different testnet windows).
- **Rate limits:** stricter per-IP / per-key request-rate quotas (`HTTP 429`).
- **Latency:** higher p95/p99 `getEvents` and `getLatestLedger` latency under mainnet traffic.

Before mainnet launch, the poller must run **read-only** against mainnet RPC on staging for **24 hours or more** and pass all acceptance gates.

## Running the 24h Read-Only Soak

Run the read-only soak harness (`scripts/mainnet-rpc-soak.py`) from staging against the target mainnet RPC provider:

```bash
python3 scripts/mainnet-rpc-soak.py \
  --rpc-url "https://mainnet.sorobanrpc.com" \
  --duration 24h \
  --poll-interval 6 \
  --provider-max-rps 10 \
  --output-json results/mainnet-rpc-soak-report.json \
  --output-md results/mainnet-rpc-soak-report.md
```

The harness mirrors `EventPoller` read-only RPC semantics:

- Cold-starts with `getLatestLedger` and rewinds by `DefaultColdStartOffset` (12 ledgers), never requesting ledger `0`.
- Polls `getEvents` every `6s` (`IndexerPollInterval`) with an `8s` timeout (`IndexerRequestTimeout`).
- Tracks monotonic cursor advancement, gap alerts, `processed_events` deduplication, lag p50/p95/p99, RPC latency p50/p95/p99, error counts (`rate_limit`, `retention`, `timeout`, `http_error`, `rpc_error`, `network_error`), and request rate (`req/s`, `req/min`).

## Staging Database & Alert Checks (When Running Full Staging Indexer)

If also running the staging API's read-only event indexer against mainnet RPC, verify:

1. **Cursor continuity:** `system_state` cursor advances monotonically without stalls.
2. **Zero duplicate applications in `processed_events`:**
   ```sql
   SELECT event_id, COUNT(*)
   FROM processed_events
   GROUP BY event_id
   HAVING COUNT(*) > 1;
   ```
   Expected: `0` rows.
3. **No gap alerts fired:** confirm zero indexer gap / staleness alerts in Alertmanager / Prometheus over the 24h window.
4. **Request volume within provider limits:** confirm `request_rate_rps <= provider_max_rps` and `error_counts.rate_limit == 0`.

## Acceptance Criteria

- [ ] Soak duration >= 24h against mainnet RPC on staging.
- [ ] Soak report recorded with **lag p50/p99**, **error counts**, and **request rate**.
- [ ] Cursor advances continuously (`cursor_continuous == true`).
- [ ] `processed_events` has no duplicates (`duplicate_applications == 0`).
- [ ] Zero gap alerts fired (`gap_alert_count == 0`).
- [ ] Request volume stays within provider limits (`within_rate_limit == true`, `rate_limit == 0`).
- [ ] Results recorded in `docs/launch-readiness-register.md`.
- [ ] Any issues discovered during the soak are filed as GitHub issues.
