# Nester Savings Rate — design

Date: 2026-10-30. Status: draft for approval.

## Intent

Rebuild Nester around two proven models:

- **Sky (sUSDS):** what the user sees. One product — the **Nester Savings Rate (NSR)** — that a saver deposits USDC into and holds as a single yield-bearing savings token whose value grows as yield accrues (and can fall if an integrated protocol takes a loss). A public, locked **reserve** sits behind it as a first-loss buffer.
- **Morpho (Vaults V2):** how it works underneath. A **small savings core** that never changes its accounting rules, **adapters** that each talk to one yield protocol and report what the position is actually worth, **caps** per adapter, and a **role split** where risk can be cut instantly but only added slowly behind a timelock.

Why (from the 2026-10-30 research reports `reports/Nester vs DeFi lending leaders.md` and `reports/Nester SCF grant readiness.md`, and the user's decisions in chat):

- Stellar's DeFi pool is small (~$218M; ~$256M USDC). One savings pool concentrates deposits instead of splitting them across three thin tiers.
- There are not enough safe Stellar yield sources for three genuinely different risk tiers; there are enough for one diversified savings product.
- Everyday savers understand one rate; wallets and anchors can embed one "Earn" product.
- The current vault (`packages/contracts/contracts/vault`, ~11.5K lines, ~110 entrypoints) optimizes to **183,090 bytes**, above Stellar's **131,072-byte** contract limit on testnet and mainnet, so it cannot be deployed anywhere. Its share price is set by a trusted `report_yield` call. A small core with on-chain valuation fixes both and shrinks the audit.

## Decisions already made (by the user)

1. Model Nester on Morpho (architecture) and Sky (product).
2. Launch with **one** savings product (the NSR) on USDC. The Conservative / Balanced / Growth tier menu is **retired at launch**; curated vaults may return later as an advanced option built on the same core.
3. Work spans contracts, backend and frontend; the dapp UI changes to fit.

## Goals

- **G1. Savings core contract** — one asset (USDC), deposit / withdraw / redeem with ERC-4626-style share math, `total_assets()` computed on-chain as idle balance + Σ adapter `position_value()`; no trusted yield reports.
- **G2. Fits the network with headroom** — every contract's optimized WASM ≤ 100 KB (limit 128 KB), enforced by a CI size check.
- **G3. Morpho role split and timelocks**
  - **Owner** (multisig): sets roles, adds/removes adapters, sets timelock length.
  - **Curator**: proposes cap increases and new adapters → applied only after a timelock (default 3 days). Cap **decreases** apply instantly.
  - **Allocator** (keeper): moves funds between idle and adapters, only within caps.
  - **Guardian**: can pause deposits, revoke a pending (timelocked) change, and force an adapter's cap to zero. Can never add risk. **Withdrawals are never paused by the guardian.**
  - **Timelocked for every role, including Owner** *(my call, following Morpho)*: adding an adapter, raising a cap, raising the performance fee, shortening the timelock, and burning reserve shares. Lengthening the timelock, lowering caps/fees, and removing an adapter with zero balance apply instantly.
  - Once a change's timelock expires, **anyone** may execute it *(my call)*, so a change cannot be held hostage; the Guardian may revoke it before then.
- **G4. Caps per adapter** — absolute cap (asset units) and relative cap (bps of total assets); allocator moves that would exceed either revert.
- **G5. Sky-style savings rate and reserve**
  - The NSR is the **realized** rate: the annualized growth of the savings token's value over a trailing window (7d and 30d), computed off-chain from on-chain share-price snapshots. It is not set by governance and is never promised.
  - **Performance fee: 10% of yield**, taken only on new highs in share price (high-water mark), minted as savings-token shares to the Reserve. Hard-capped at **20%** in code. Increases go through the timelock; decreases apply instantly. *(user decision)*
  - **The Reserve is a locked loss buffer**, not revenue. *(user decision)* It is a new, minimal `reserve` contract (not the existing `treasury`, whose instant `withdraw()` and `distribute()` would break the guarantee). Its shares can leave in exactly one way: an owner action behind the timelock that **burns** reserve shares to restore share value after a loss. Nothing can withdraw them to any address. The reserve balance is public.
  - **Losses are shared pro-rata.** *(user decision)* On-chain valuation means a loss in an integrated protocol lowers `total_assets()` and the share price immediately; the app shows this honestly. The owner may then burn reserve shares (timelocked) to restore value as far as the reserve allows. No UI copy may say the value "only goes up".
- **G6. Adapters** — keep the existing `YieldAdapter` trait (`deposit`, `withdraw`, `position_value`, `current_apy`, `underlying`).
  - **Blend adapter:** fix `position_value` to return underlying USDC (bToken × pool exchange rate), not raw pool units.
  - **DeFindex adapter:** new; deposits into a DeFindex vault (maintained at github.com/defindex-io) and values the position by its share price. Integrate a live DeFindex **testnet USDC vault** if one exists; otherwise ship against a mock with the same interface and swap the real vault in later. *(user decision)* Note that DeFindex vaults may themselves allocate to Blend, so caps should treat the combined Blend exposure as one risk.
  - **`adapter_lending` and `adapter_pool`** stay out of the savings core (their own code notes they revert against the real protocols).
  - **Soroswap adapter:** excluded from the savings core (manipulable spot-reserve valuation, missing nested-call auth, zero min-out). Stays testnet-only until hardened.
- **G7. Withdraw liquidity** — withdrawals pay from idle first, then pull from adapters in a fixed withdraw order set by the curator. If assets are not liquid, the call reverts with a clear error rather than paying partially.
- **G7b. An adapter that cannot be valued** *(my call)* — the core reads each adapter with a fallible call. If any adapter's `position_value()` fails, **deposits revert** (no minting against an unknown price), while **withdrawals continue** at a conservative price that counts that adapter as zero, paid from idle and the other adapters. An adapter whose cap the Guardian forced to zero is drained by the allocator as liquidity allows and is likewise excluded from new deposits.
- **G8. Backend** — index core events; serve savings endpoints (current rate, trailing 7d/30d, TVL, share price, per-adapter allocation and caps, reserve balance, pending timelocked changes, user position and history).
- **G9. Dapp** — the home/dashboard becomes the NSR: one rate, one deposit/withdraw panel, the user's savings-token balance and its USDC value, a "where your money is" allocation card with caps, a reserve card, and a "pending changes" card (timelocked curator actions). Savings goals and recurring deposits stay and deposit into the NSR.
- **G10. Website** — copy refers to the Nester Savings Rate; no tier menu.

## Non-goals (v1)

- Multiple vaults / tiers in the UI (the core supports more instances later; not exposed now).
- Lending, borrowing, leverage, looping.
- A governance-set rate, a native stablecoin, a token, buybacks.
- Locked positions with boosted yield, early-withdrawal penalties, the emergency queue and the penalty-distribution machinery from the current vault (not carried into the core; may return as separate add-on contracts later).
- Automatic loss absorption by the reserve.
- Mainnet deployment (needs the external audit first). Target is testnet.
- Fixing the indexer's RPC-retention stall (tracked separately).

## Anti-goals (would make this a failure even if it works)

- Any path where a privileged role can change the share price directly (no `report_yield`).
- Any role that can block withdrawals while assets are liquid.
- Showing a promised or "target" rate as if it were earned.
- A core that grows back toward the old vault's size; extras belong in separate contracts.

## Constraints

- Soroban, Rust, soroban-sdk 21.7.7 on the pinned Rust 1.82 toolchain (`packages/contracts/rust-toolchain.toml`); reproducible WASM.
- 128 KB contract limit; target ≤ 100 KB per contract.
- Existing SEP-41 share token (`vault_token`) is reused as the savings token if it fits the core's mint/burn needs; otherwise the core embeds share balances (builder's call, see below).
- Team of two; plan must be sliceable into SCF-sized tranches (core + Blend/DeFindex adapters + testnet = first deliverable).
- Backend is Go (stdlib `net/http`), Postgres, Redis; dapp is Next.js 16 with Stellar Wallets Kit.
- Base branch for this work: `feat/savings-rate`, off `chore/ci-five-checks` (PR #1675), because that branch is where all contracts compile.

## Left to the builder

- Whether the savings token is the existing `vault_token` contract or share balances inside the core (pick whichever keeps the core ≤ 100 KB and the token SEP-41 compatible for wallets).
- Exact event names and payloads (keep them documented in `docs/` and indexed by the API).
- Timelock default (3 days suggested) and the snapshot cadence used for the trailing rate.
- How `savings_goal` and `recurring_deposit` are re-pointed at the core (call-site changes only).
- Migration of the old vault: none on-chain (testnet only); mark it deprecated in code and docs.
