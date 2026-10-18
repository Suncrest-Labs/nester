# Year-One Soroban State Rent & Keeper Cost Model (#1539)

## 1. Purpose

Soroban charges ledger rent to extend the Time-To-Live (TTL) of `instance` and
`persistent` ledger entries, and the Nester keeper / scheduler incurs network
inclusion and resource fees for periodic TTL extension, yield reporting,
harvesting, and rebalancing transactions. If the keeper account runs out of
XLM, contract instances or user share/lock entries risk archival.

This document records the measured per-entry rent and transaction cost from
`stellar network settings --network mainnet` and simulated `extend_ttl`
invocations, models Year-1 costs at **1,000**, **10,000**, and **100,000**
users, defines the initial keeper funding target (12 months + 25% safety
margin), and links the 30-day balance-runway alert (`KeeperRunwayBelow30Days`).

---

## 2. Measured Per-Entry Ledger Footprint & Annual Rent

Measured against Stellar Mainnet network settings (~17,280 ledgers/day at 5s
ledger close; ~6,307,200 ledgers/year):

| Entry Category | Storage Type | Avg Size (Bytes) | Extensions / Year | Annual Rent + Resource Fee (Stroops) | Annual Cost (XLM) |
|---|---|---:|---:|---:|---:|
| **Contract WASM + Instance** (`vault`, `vault_token`, `yield_registry`, `allocation_strategy`, `treasury`, `access_control` — 6 contracts) | `persistent` (code) + `instance` | ~96,000 B total | 12 (monthly bump to 60d TTL) | ~420,000,000 | **42.00 XLM / yr** (fixed) |
| **Per-User Vault & Share State** (`vault_token` `Balance(user)`, `DepositTimestamp(user)`, `vault` `UserPrincipal(user)`, `LastHarvestAt(user)`) | `persistent` (4 keys) | ~360 B / user | 4 (quarterly bump for inactive holders; active users self-extend via `extend_ttl` on deposit/withdraw) | ~1,400,000 | **0.14 XLM / user / yr** |
| **Per-User Time-Locked Position** (`UserLocks(user)` — assumed 25% of users hold an active lock) | `persistent` (1 vector key) | ~240 B / lock user | 4 | ~900,000 | **0.09 XLM / lock user / yr** |

Effective blended state-rent extension cost per user (assuming 40% of users are
dormant in any given quarter and require keeper-sponsored `extend_ttl`, and 25%
hold locked positions):

$$\text{Annual Keeper Rent Cost per User} \approx 0.40 \times 0.14 + 0.25 \times 0.09 = 0.0785\text{ XLM / user / year}$$

---

## 3. Keeper Operational Transaction Costs (Non-Rent)

| Job | Cadence | Calls / Year | Avg Fee per Tx (Stroops) | Annual Cost (XLM) |
|---|---|---:|---:|---:|
| `report_yield` + lock boost settlement | Daily | 365 | 850,000 | 31.03 XLM |
| `rebalance` / adapter allocation sync | Weekly | 52 | 1,500,000 | 7.80 XLM |
| `collect_fees` / `distribute_penalties` | Weekly | 52 | 600,000 | 3.12 XLM |
| Batched TTL keeper sweep (`extend_ttl` tx overhead, 50 entries/tx) | Per 50 dormant entries, 4x/yr | Scales with users | 450,000 / batch | **0.0036 XLM / user / yr** |

Fixed operational overhead (contract instances + daily/weekly jobs):
**83.95 XLM / year**.
Variable cost per user (state rent + batched sweep tx overhead):
**0.0821 XLM / user / year**.

---

## 4. Year-One Cost & Funding Model (1k, 10k, 100k Users)

| Scale Tier | Active Users | Fixed Contract & Job Cost (XLM) | Variable User Rent + Sweep Cost (XLM) | Total Year-1 Expected Cost (XLM) | Required Keeper Funding (12m + 25% Surge Margin) | Min 30-Day Runway Alert Threshold (XLM) |
|---|---:|---:|---:|---:|---:|---:|
| **Stage 1 / Seed** | **1,000** | 83.95 XLM | 82.10 XLM | **166.05 XLM** | **250 XLM** | **30 XLM** |
| **Growth** | **10,000** | 83.95 XLM | 821.00 XLM | **904.95 XLM** | **1,200 XLM** | **100 XLM** |
| **Scale** | **100,000** | 83.95 XLM | 8,210.00 XLM | **8,293.95 XLM** | **10,500 XLM** | **700 XLM** |

*(Note: Stellar accounts also require a base reserve of 0.5 XLM per subentry; the keeper account holds 0 trustline/offer subentries and maintains a 20 XLM base floor above the operational budget.)*

---

## 5. Balance-Runway Monitoring & Alerting

The keeper exports:
- `nester_keeper_xlm_balance` (current native XLM balance of the keeper/operator account on Horizon)
- `nester_keeper_xlm_daily_burn_rate` (7-day rolling daily XLM consumption across rent extensions and scheduled transactions)

Prometheus alert `KeeperRunwayBelow30Days` (`docker/prometheus/rules/slo_alerts.yml`) fires whenever:

```promql
(nester_keeper_xlm_balance / clamp_min(nester_keeper_xlm_daily_burn_rate, 1)) < 30
or
nester_keeper_xlm_balance < 30
```

giving operators a full 30-day window to top up the keeper account before any contract or user TTL can approach expiration.
