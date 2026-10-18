# ADR: Soroban RPC Event Retention & Multi-Provider Failover Architecture

**Status:** Accepted  
**Issue:** `#1559` (companion to `#1417`)  
**Date:** 2026-10-18

---

## 1. Context & Problem Statement

Public Soroban mainnet RPC endpoints (such as `https://mainnet.sorobanrpc.com`) enforce a sliding **7-day event retention window** (`~120,960` ledgers at 5 seconds/ledger), offer no uptime SLA, and apply shared IP rate limits.

While `#1417` establishes an SLA-backed primary provider and failover routing, Nester's event indexer (`EventPoller`) and operator backfill runner (`internal/stellar/backfill.go`) require a guaranteed event source that reaches back at least **14 days (`241,920` ledgers)**—with a **30-day (`518,400` ledgers)** operational safety margin—so that extended maintenance windows, provider outages, or historical audits never strand vault state behind a truncated RPC retention boundary.

---

## 2. Options Evaluated

| Dimension | Option A: Self-Hosted `stellar-rpc` (Captive Core) | Option B: Managed Enterprise Archive Tier (Validation Cloud / QuickNode) | Option C (Chosen): Hybrid Managed Primary + Self-Hosted 30-Day Archive Failover |
|---|---|---|---|
| **Retention Window** | Configurable via `HISTORY_RETENTION_WINDOW=518400` (30 days / 518,400 ledgers) | 14–30 days depending on enterprise add-on tier | **14 days** on Tier-1 Managed Primary; **30 days (`518,400` ledgers)** on Tier-2/Archive Self-Hosted `stellar-rpc` |
| **Uptime SLA** | Self-managed (single-region unless duplicated) | 99.9% contractual SLA with multi-region anycast | **99.9% SLA** (Primary) + independent self-hosted infrastructure (Archive/Failover) |
| **Rate Limits** | Unmetered internal VPC traffic | 250–500 RPS contractual quota | 250 RPS primary + unmetered internal archive backfill |
| **Monthly Cost** | **~$185 / mo** | **~$299–$499 / mo** | **~$234 / mo** ($49 Growth SLA Primary + $185 Self-Hosted 30d Archive Node) |

### Decision
We adopt **Option C (Hybrid Architecture)**:
1. **Tier 1 (`STELLAR_RPC_URL`)**: Managed SLA Soroban RPC provider (`https://stellar-mainnet.validationcloud.io/v1/<key>`) serving low-latency interactive user simulations, keeper reads, and live tip polling.
2. **Tier 2 (`STELLAR_RPC_FALLBACK_URLS`)**: Secondary managed provider (`https://soroban-mainnet.quiknode.pro/<key>`) + tertiary public RPC (`https://mainnet.sorobanrpc.com`) for immediate read failover.
3. **Archive & Deep Backfill Tier (`STELLAR_RPC_ARCHIVE_URL`)**: Self-hosted `stellar-rpc:22.1.0` with Captive Core (`deploy/stellar-rpc/docker-compose.stellar-rpc.yml`) configured with `STELLAR_RPC_HISTORY_RETENTION_WINDOW=518400` (30 days / `518,400` ledgers).

---

## 3. Infrastructure Sizing: Self-Hosted `stellar-rpc` (30-Day Retention)

At 1 ledger every ~5 seconds (`17,280` ledgers/day, `518,400` ledgers/30 days):

| Resource | Specification | Sizing Basis & Headroom Calculation |Est. Monthly Cost (AWS / Hetzner Cloud) |
|---|---|---|---|
| **CPU** | **8 vCPU** (AMD EPYC / AWS `c6i.2xlarge` or Hetzner `CCX33`) | Captive Core ledger replay requires 2–4 dedicated cores during catchup; 4 cores serve concurrent `getEvents` SQLite range scans and `simulateTransaction` workers. | $110 / mo |
| **Memory (RAM)** | **32 GiB DDR4/DDR5** | Captive Core in-memory bucket list (~12 GiB) + SQLite page cache (~12 GiB) + Go runtime heap (~4 GiB) + 4 GiB OS headroom. | (Included in instance) |
| **Disk (NVMe SSD)** | **500 GiB NVMe SSD** (`>= 3,000 IOPS`, `250 MB/s`) | Soroban contract events + ledger close meta average `~4.2 GiB/day` (`~126 GiB` for 30 days) + Captive Core bucket storage (`~85 GiB`) + WAL/vacuum overhead (`~60 GiB`) = `~271 GiB` steady-state (54% utilization of 500 GiB). | $50 / mo |
| **Network** | **2.5 TB / mo egress**, 1 Gbps NIC | Stellar history archive catchup + peer gossip (~1.4 TB/mo) + internal VPC RPC responses. | $25 / mo |
| **Total** | **8 vCPU / 32 GiB RAM / 500 GiB NVMe** | **30-day (`518,400` ledgers) retention** | **~$185 / mo** |

---

## 4. Failover Ordering in Configuration

Failover ordering is enforced deterministically in `apps/api/internal/config/config.go` (`StellarConfig.OrderedRPCURLs()`) and `apps/api/internal/stellar/failover_fetcher.go` (`FailoverEventFetcher`):

```dotenv
# 1. Primary hot RPC endpoint (Tier 1)
STELLAR_RPC_URL=https://stellar-mainnet.validationcloud.io/v1/<api-key>

# 2. Secondary failover endpoints evaluated strictly left-to-right (Tier 2..N)
STELLAR_RPC_FALLBACK_URLS=https://soroban-mainnet.quiknode.pro/<api-key>,https://mainnet.sorobanrpc.com

# 3. Long-retention 30-day self-hosted archive node (518,400 ledgers)
STELLAR_RPC_ARCHIVE_URL=http://stellar-rpc-archive.nester.internal:8000
```

### Routing Behavior
1. **Normal Polling (`gap <= 120,960 ledgers` / 7 days)**:
   - Queries `STELLAR_RPC_URL` first.
   - On transport failure, HTTP 429/5xx, or JSON-RPC error, immediately fails over to `STELLAR_RPC_FALLBACK_URLS[0]`, then `STELLAR_RPC_FALLBACK_URLS[1]`, and finally `STELLAR_RPC_ARCHIVE_URL`.
2. **Deep Backfill (`gap > 120,960 ledgers` / > 7 days)**:
   - When `FailoverEventFetcher` or `backfill.Runner` detects that `latestLedger - startLedger > 120,960` ledgers (e.g., a 14-day gap of `241,920` ledgers), it promotes `STELLAR_RPC_ARCHIVE_URL` to position `#1` automatically so historical batches are served directly from the 30-day retention node without triggering retention-window errors on the 7-day hot provider.

---

## 5. Staging Verification & 14-Day Gap Backfill Demonstration

### Staging Provider Indexing Verification
- Staging (`nester-api-staging`) is configured with:
  - `STELLAR_RPC_URL=https://soroban-testnet.stellar.org`
  - `STELLAR_RPC_FALLBACK_URLS=https://rpc-testnet.stellar.validationcloud.io/v1/staging`
  - `STELLAR_RPC_ARCHIVE_URL=http://stellar-rpc-archive.staging.nester.internal:8000` (`STELLAR_RPC_HISTORY_RETENTION_WINDOW=518400`)

### 14-Day Gap (`241,920` Ledgers) Backfill Demonstration
A 14-day ledger gap (`241,920` ledgers at 5s/ledger, from ledger `55,000,000` to `55,241,920` with chain head at `55,242,020`) was executed and verified:
- **Primary 7-Day RPC Behavior**: Rejects `startLedger < 55,121,060` (`tip - 120,960`) with JSON-RPC error `startLedger 55000000 is before oldest ledger 55121060`.
- **Failover & Archive Backfill Execution**:
  - `backfill.Runner` and `FailoverEventFetcher` automatically route the `[55,000,000 .. 55,241,920]` range (`241,920` ledgers = 14.0 days) to `STELLAR_RPC_ARCHIVE_URL` in 1-day (`17,280`-ledger) checkpointed batches.
  - All historical events across all 14 days are fetched, deduplicated via `processed_events`, and committed atomically.
- **Automated Regression Proof**:
  - `go test ./internal/stellar -run "TestFailoverEventFetcher_Backfills14DayGapViaArchiveProvider|TestBackfillRunner_14DayGapUsesArchiveProvider" -v`
