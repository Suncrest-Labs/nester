# Nester — what works, what doesn't

Written 2026-09-03 after consolidating the pivot work. Every claim below was
checked against the running stack rather than inferred from code, and the
check is named so it can be repeated.

## The one-line summary

Nester is a working savings and yield-discovery app whose deposits do not yet
reach a yield protocol. Everything around that link is built; the link is not.

## Works today

| Area | State | How it was checked |
|---|---|---|
| Yield discovery | 7 live Stellar pools with APY, TVL and risk tiers, public to signed-out visitors | `GET /api/v1/yields` → 200, 7 pools |
| Allocation composer | Pick protocols, drill into pools, set weights, see blended APY | browser: 3 protocols, 50/50 split, 8.49% blended |
| Deposit | Real signed Soroban transaction, registered server-side so the position survives a cleared browser | deposit row persists in Postgres and returns from `GET /transactions` |
| Savings goals | Create, track, and attach recurring deposit schedules | goal + weekly schedule created against the live API |
| Portfolio | Positions, totals, per-protocol allocations, share price | `GET /portfolio/summary` → $15,000 → $15,384.81 |
| Auth | Wallet-signature login (SEP-53), non-custodial throughout | existing suite |
| Event indexer | Tracks testnet, cursor advancing | 0 poll failures in a 5-minute window (was ~10/min) |
| Routes | All 8 render without an error boundary | browser smoke test |

Tests: 331 frontend, 656 contract, full Go suite. All green on `dev`.

## Does not work

**Deposited funds never reach a yield protocol.** This is the gap that matters.

`deposit()` moves funds into the vault and mints shares. That is all it does.
Funds only reach a protocol when an operator calls `rebalance()`, which routes
through an adapter — and no adapter is deployed. So a deposit today earns
nothing: the APYs on `/yields` are real figures about what those protocols pay,
not what a Nester deposit currently earns.

Two adapters are written and tested but undeployed:

- **`adapter_blend`** speaks Blend's real `submit(from, spender, to, requests)`
  interface. Blocked on deployment: Blend's testnet has a pool *factory* but no
  reachable pool, and standing one up needs a price-oracle address we do not
  have.
- **`adapter_soroswap`** speaks Soroswap's router. Not blocked — Soroswap's
  testnet factory and router are live with 229 pairs holding real liquidity,
  and no oracle is required. This is the shortest path to a real yield-earning
  deposit.

The two pre-existing generic adapters (`adapter_lending`, `adapter_pool`)
cannot drive their target protocols: each invokes a function signature no
deployed contract implements, and each passed its tests because its mock
implemented the adapter's assumption rather than the protocol's interface.
Both new adapters carry an `interface_test` that puts the generic adapter in
front of a protocol-shaped mock and asserts it fails, so the gap cannot close
silently again.

## Known limitations, stated plainly

- **Per-user allocation is not on-chain.** The composer's percentages decide
  what the user deposits into, not an enforced split: `set_weights` is
  admin-only and `deposit()` takes no allocation. Splitting one deposit across
  N protocols with the contracts as they are means N vaults and N signatures.
- **On-chain balance reconciliation fails.** The deployed testnet vaults are
  from April and lack `total_assets`, which the current source has. The
  contracts need redeploying before chain balances sync.
- **AMM yield is not lending yield.** A Soroswap LP position carries
  impermanent loss and can be worth less than simply holding, fees included.
  If an AMM APY is ever shown beside a lending APY, that difference has to be
  visible.
- **`/stocks`** is a deliberate placeholder.
- **Swaps and fiat onramp** are v2/v3 and not built.

## Local development gotchas

Both of these cost real debugging time, so they are written down rather than
rediscovered:

- `/app/.next` is a **named Docker volume**, not part of the source bind
  mount. `pnpm build` on the host writes somewhere the container never reads,
  and `docker compose rm -v` does not clear it. To force a clean frontend:
  `docker rm -f nester-frontend-1`, remove that volume, then
  `docker compose up -d frontend`.
- The **service worker** used to register in development and cached hashed
  `/_next/` chunks. Deleting a component left the browser loading a chunk that
  still imported it, and a refresh would not clear it. Registration is now
  production-only, and any worker already registered tears itself down.

## The next honest step

Deploy `adapter_soroswap` against a live testnet pair, register it, and make
one real supply. If it works, Nester genuinely earns yield and the thesis is
proved end to end. If it fails, that is worth knowing before more UI is built
on top of it.
