# Nester Savings Rate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the three-tier vault with one USDC Nester Savings Rate: a small Morpho-style savings core with on-chain valuation, caps, roles and timelocks, a locked Sky-style reserve, Blend and DeFindex adapters, API endpoints that publish the realized rate, and a dapp built around one rate.

**Architecture:** New `savings_core` Soroban contract holds USDC, issues SEP-41 savings shares itself, values itself as idle + Σ adapter `position_value()`, and moves funds to adapters only within caps. Risk-adding changes queue behind a timelock; a `savings_reserve` contract holds fee shares that only the core can burn (timelocked). The Go API snapshots share price hourly and serves the trailing rate; the Next.js dapp shows one rate, a deposit/withdraw panel and transparency cards.

**Tech Stack:** Rust + soroban-sdk 21.7.7 on Rust 1.82 (pinned); Go 1.26.10 stdlib `net/http` + Postgres; Next.js 16 + React Query + Stellar Wallets Kit + Vitest.

**Spec:** `docs/superpowers/specs/2026-10-30-savings-rate-design.md`

## Global Constraints

- One asset: USDC. No tiers in the UI; the old `vault` contract is deprecated, not migrated.
- Every new contract's optimized WASM ≤ **100 KB** (network limit **131,072 bytes**), checked in CI.
- No privileged path may set the share price; no `report_yield` equivalent.
- No role may block withdrawals while assets are liquid; the guardian can pause **deposits** only.
- Performance fee **10%** (1,000 bps) on new share-price highs only (high-water mark), hard cap **20%** (2,000 bps), minted as shares to the reserve.
- Timelocked (all roles, incl. owner): add adapter, raise cap, raise fee, shorten timelock, burn reserve shares. Instant: lower cap/fee, lengthen timelock, remove zero-balance adapter, pause deposits, revoke pending change. Default timelock **3 days** (259,200 s). After ETA, **anyone** may execute.
- If any adapter's `position_value()` fails: deposits revert; withdrawals price that adapter at zero.
- Reserve shares leave only by timelocked burn. No function transfers or withdraws them.
- Copy rule: never "guaranteed", "only goes up", "target rate" as earned; the rate is labelled realized/trailing.
- Rust: `cargo fmt`, `cargo clippy --all-targets -- -D warnings` clean. Go: `go vet`, staticcheck clean. Dapp: `lint:security` 0 errors.
- Commits as 0xDeon; no co-author lines (user rule).

## Review Focus

1. **Share inflation / first-depositor attack** — a tiny first deposit followed by a large direct USDC transfer must not steal later depositors' funds → test in Task 2 (`test_donation_does_not_steal_next_deposit`).
2. **Withdraw when an adapter is illiquid or failing** — user withdraws more than idle while one adapter reverts → pays from idle + healthy adapters or reverts with `InsufficientLiquidity`, never mis-prices → test in Task 3 (`test_withdraw_skips_failing_adapter_conservatively`).
3. **Rounding** — deposit/withdraw rounding always favours the vault (shares down on deposit, assets down on redeem) → Task 2 (`test_rounding_favors_vault`).
4. **Fee on recovery** — after a loss and partial recovery below the previous high, no fee is charged → Task 5 (`test_no_fee_below_high_water_mark`).
5. **Rate with sparse/no history** — API on a fresh deploy with <2 snapshots returns `null` rates (not 0%, not error) and the UI shows "Not enough history yet" → Task 12 (`TestTrailingRate_InsufficientHistory`) and Task 16 (`renders not-enough-history state`).

---

## File Structure

**Contracts** (`packages/contracts/`)
- `contracts/savings_core/` (new crate `savings-core`, lib name `savings_core`)
  - `src/lib.rs` — contract entrypoints only
  - `src/shares.rs` — SEP-41 share ledger + conversion math
  - `src/adapters.rs` — adapter list, caps, valuation, allocate/deallocate, withdraw pull
  - `src/governance.rs` — roles, pending changes, timelock
  - `src/fees.rs` — high-water-mark fee accrual
  - `src/test.rs`
- `contracts/savings_reserve/` (new crate, ~20 lines) — holds shares, exposes `core()` only
- `contracts/adapter_blend/src/lib.rs` — fix `position_value`
- `contracts/adapter_defindex/` (new crate) — DeFindex vault adapter
- `libs/test_utils/src/mocks/blend_pool.rs` — add `get_reserve`
- `libs/test_utils/src/mocks/defindex_vault.rs` (new mock)
- `tests/integration/src/integration/savings_tests.rs` (new end-to-end)
- `scripts/deploy-savings-testnet.sh` (new)
- `../../scripts/check-wasm-size.sh` (repo root, new) + step in `.github/workflows/ci.yml` Contracts job

**Backend** (`apps/api/`)
- `migrations/126_savings_snapshots.{up,down}.sql`
- `internal/domain/savings/savings.go` — types + `TrailingRate` pure function
- `internal/repository/postgres/savings_repository.go`
- `internal/stellar/savings_reader.go` — read core views via simulate
- `internal/service/savings_service.go` — snapshot job + read model
- `internal/handler/savings_handler.go`
- `cmd/api/main.go` — wire handler, job, config `SAVINGS_CORE_CONTRACT_ID`, `SAVINGS_RESERVE_CONTRACT_ID`

**Dapp** (`apps/dapp/frontend/`)
- `lib/api/savings.ts`, `hooks/useSavings.ts`, `lib/stellar/savings.ts`
- `components/savings-rate/` — `RateHero.tsx`, `SavePanel.tsx`, `PositionCard.tsx`, `AllocationCard.tsx`, `ReserveCard.tsx`, `PendingChangesCard.tsx` (+ `__tests__/`)
- `app/dashboard/page.tsx` — rebuilt around the components
- `app/vaults/page.tsx`, `components/app-shell.tsx`, `components/bottom-nav.tsx`, `components/navbar.tsx` — remove tier menu

---

## Contracts

### Task 1: CI WASM size guard

**Files:**
- Create: `scripts/check-wasm-size.sh`
- Modify: `.github/workflows/ci.yml` (Contracts job, after "Build deployable contracts…")
- Modify: `Makefile` (target `wasm-size`)

**Interfaces:**
- Produces: `scripts/check-wasm-size.sh <max_bytes> <wasm>...` exits 1 and prints `FAIL <file> <bytes> > <max>` for any file over the limit.

- [ ] **Step 1:** Write the script: for each argument run `stellar contract optimize --wasm <f> --wasm-out <tmp>` when the `stellar` CLI exists, else `wasm-opt -Oz`; compare `stat -c%s` against `$1`.
- [ ] **Step 2:** Run against the current vault: `scripts/check-wasm-size.sh 102400 packages/contracts/target/wasm32-unknown-unknown/release/vault_contract.wasm` → Expected: `FAIL … 183090 > 102400`, exit 1.
- [ ] **Step 3:** Add CI step checking `savings_core.wasm savings_reserve.wasm adapter_blend.wasm adapter_defindex.wasm` at 102400 (install `stellar-cli` via `cargo install --locked stellar-cli` cached, or `binaryen`). The old `vault` is excluded (deprecated).
- [ ] **Step 4:** Commit `ci: fail contracts over 100KB optimized`.

### Task 2: savings_core — shares, deposit, withdraw (idle only)

**Files:**
- Create: `contracts/savings_core/Cargo.toml` (deps: soroban-sdk, nester-common; dev: testutils, nester-test-utils; `build = "../../build-meta.rs"`), `src/lib.rs`, `src/shares.rs`, `src/test.rs`
- Modify: `packages/contracts/Cargo.toml` workspace members (+`contracts/savings_core`)

**Interfaces:**
- Produces (contract `SavingsCore`):
  - `initialize(env, owner: Address, asset: Address, reserve: Address, name: String, symbol: String)`
  - `deposit(env, from: Address, assets: i128, min_shares: i128) -> i128` (shares minted)
  - `withdraw(env, owner: Address, assets: i128, max_shares: i128) -> i128` (shares burned)
  - `redeem(env, owner: Address, shares: i128, min_assets: i128) -> i128` (assets paid)
  - views: `total_assets() -> i128`, `total_supply() -> i128`, `convert_to_shares(assets) -> i128`, `convert_to_assets(shares) -> i128`, `share_price() -> i128` (assets per 10^7 shares)
  - SEP-41: `balance, transfer, transfer_from, approve, allowance, decimals (=7), name, symbol`
  - events: topic `("deposit", user)` data map `{amount, shares}`; `("withdraw", user)` `{amount, shares}` (topic[0] is what `apps/api/internal/stellar/indexer.go` keys on)
  - errors (`#[contracterror] SavingsError`): `ZeroAmount=1, Slippage=2, InsufficientShares=3, InsufficientLiquidity=4, DepositsPaused=5, AdapterUnavailable=6, Unauthorized=7, CapExceeded=8, NotReady=9, NoSuchChange=10, FeeTooHigh=11`
- Conversion uses a virtual offset: `shares = assets * (supply + 1) / (total_assets + 1)` rounded **down**; `assets = shares * (total_assets + 1) / (supply + 1)` rounded **down** for redeem, shares rounded **up** for `withdraw`.

- [ ] **Step 1: Failing tests** in `src/test.rs`:
  - `test_first_deposit_mints_one_to_one`: deposit 100_0000000 → shares == 100_0000000, `total_assets()==100_0000000`.
  - `test_second_depositor_proportional`: after 10% asset growth (mint USDC to core), second deposit 110 → shares == 100 (±1).
  - `test_rounding_favors_vault`: deposit then redeem all → assets_out ≤ deposited.
  - `test_donation_does_not_steal_next_deposit`: attacker deposits 1 stroop, transfers 1,000 USDC directly to core, victim deposits 1,000 USDC → victim redeem returns ≥ 999 USDC.
  - `test_slippage_guards`: `deposit(min_shares > preview)` and `redeem(min_assets > preview)` → `Slippage`.
  - `test_share_token_transfer`: transfer half of shares; both balances correct.
- [ ] **Step 2:** `cargo test -p savings-core` → FAIL (not implemented).
- [ ] **Step 3:** Implement `shares.rs` (balances, allowances, supply, conversion) and the entrypoints; `total_assets()` = USDC balance of the core for now.
- [ ] **Step 4:** `cargo test -p savings-core` → PASS; `cargo clippy -p savings-core --all-targets -- -D warnings` clean.
- [ ] **Step 5:** Commit `feat(savings_core): SEP-41 savings shares with deposit/withdraw/redeem`.

### Task 3: savings_core — adapters, caps, on-chain valuation, allocation

**Files:**
- Create: `contracts/savings_core/src/adapters.rs`
- Modify: `src/lib.rs`, `src/test.rs`

**Interfaces:**
- Consumes: `nester_common::adapters::YieldAdapter` client (`deposit, withdraw, position_value, current_apy, underlying`).
- Produces:
  - `struct AdapterConfig { adapter: Address, abs_cap: i128, rel_cap_bps: u32 }`
  - views: `adapters() -> Vec<AdapterConfig>`, `allocation(adapter: Address) -> i128` (live value), `withdraw_order() -> Vec<Address>`, `idle() -> i128`
  - `allocate(env, allocator: Address, adapter: Address, assets: i128)`; `deallocate(env, allocator: Address, adapter: Address, assets: i128)`
  - `total_assets()` becomes idle + Σ `try_position_value`; internal `valuation(strict: bool) -> Result<i128, SavingsError>` — strict (deposits) errors `AdapterUnavailable` on any failure; non-strict (withdraw/redeem) counts failures as 0.
  - Withdraw/redeem pull: idle first, then `withdraw_order` adapters (skipping failing ones) until covered, else `InsufficientLiquidity`.

- [ ] **Step 1: Failing tests** (use `nester_test_utils` mock adapters; `MockFailingAdapter` exists in `libs/test_utils/src/mocks/failing.rs`):
  - `test_total_assets_includes_adapter_value`: allocate 60 of 100 to mock; mock value grows to 66 → `total_assets()==106`.
  - `test_allocate_respects_abs_and_rel_caps`: abs_cap 50 → allocate 51 → `CapExceeded`; rel_cap 4000 bps of 100 → allocate 41 → `CapExceeded`.
  - `test_only_allocator_can_allocate` → other caller `Unauthorized`.
  - `test_deposit_reverts_when_adapter_unavailable`: failing adapter registered with balance → deposit → `AdapterUnavailable`.
  - `test_withdraw_skips_failing_adapter_conservatively`: idle 40, healthy 30, failing 30 (supply 100) → redeem 50 shares pays from idle+healthy at price excluding failing; redeem needing > 70 → `InsufficientLiquidity`.
  - `test_withdraw_pulls_in_withdraw_order`.
- [ ] **Step 2:** `cargo test -p savings-core` → new tests FAIL.
- [ ] **Step 3:** Implement `adapters.rs`; use `env.try_invoke_contract` for `position_value`. (Adding adapters/caps is wired through governance in Task 4; for this task expose a test-only `#[cfg(test)]` helper or set via initialize-time owner call that Task 4 replaces.)
- [ ] **Step 4:** Tests PASS; clippy clean.
- [ ] **Step 5:** Commit `feat(savings_core): on-chain valuation, caps and allocation`.

### Task 4: savings_core — roles and timelock

**Files:**
- Create: `contracts/savings_core/src/governance.rs`
- Modify: `src/lib.rs`, `src/test.rs`

**Interfaces:**
- Produces:
  - Roles: `set_curator(owner, a)`, `set_allocator(owner, a)`, `set_guardian(owner, a)`, `transfer_ownership(owner, new)` + `accept_ownership(new)`.
  - `enum Change { AddAdapter(Address, i128, u32), RaiseCap(Address, i128, u32), SetFee(u32), SetTimelock(u64), BurnReserve(i128) }`
  - `propose(env, caller: Address, change: Change) -> u32` (id; owner or curator; returns ETA via event) — risk-adding variants queue; risk-reducing variants passed here apply immediately.
  - `execute(env, id: u32)` — anyone, after `eta`; `revoke(env, guardian_or_owner: Address, id: u32)`.
  - Instant: `lower_cap(caller, adapter, abs, rel)`, `remove_adapter(owner, adapter)` (requires zero value), `set_withdraw_order(curator, Vec<Address>)`, `pause_deposits(guardian)`, `unpause_deposits(owner)`, `zero_cap(guardian, adapter)`.
  - views: `pending_changes() -> Vec<(u32, Change, u64)>`, `timelock() -> u64` (default 259_200).
  - events: `("change_proposed", id)`, `("change_executed", id)`, `("change_revoked", id)`.

- [ ] **Step 1: Failing tests:**
  - `test_add_adapter_waits_for_timelock`: propose AddAdapter → execute before ETA `NotReady`; after advancing 259_200 s → adapter listed.
  - `test_anyone_executes_after_eta`: random address executes successfully.
  - `test_guardian_revokes_pending_change` → execute → `NoSuchChange`.
  - `test_shorten_timelock_is_timelocked_lengthen_is_instant`.
  - `test_guardian_cannot_add_risk`: guardian propose AddAdapter → `Unauthorized`.
  - `test_pause_blocks_deposits_not_withdrawals`.
  - `test_lower_cap_and_zero_cap_instant`.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS + clippy.
- [ ] **Step 5:** Commit `feat(savings_core): owner/curator/allocator/guardian roles with timelock`.

### Task 5: Fees, reserve contract, loss handling

**Files:**
- Create: `contracts/savings_reserve/` (Cargo.toml, `src/lib.rs`): `initialize(env, core: Address)`, `core() -> Address`. No other functions.
- Create: `contracts/savings_core/src/fees.rs`
- Modify: workspace members; `savings_core` `src/lib.rs`, `src/test.rs`

**Interfaces:**
- Produces:
  - fee accrues on every deposit/withdraw/redeem/allocate and via `accrue(env)` (public): if `share_price > hwm`, mint `fee_shares = gain_assets * fee_bps / 10_000` converted to shares to `reserve`; set `hwm = share_price` after mint.
  - views: `fee_bps() -> u32` (default 1000), `high_water_mark() -> i128`, `reserve() -> Address`, `reserve_shares() -> i128`.
  - `Change::SetFee(bps)` > current → timelocked; ≤ current → instant; `> 2000` → `FeeTooHigh`.
  - `Change::BurnReserve(shares)` executes `burn(reserve, shares)` (raises share price for remaining holders); never transfers.
- [ ] **Step 1: Failing tests:**
  - `test_fee_minted_to_reserve_on_gain`: 100 deposited, value grows to 110 → `accrue` → reserve shares worth 1.0 USDC (±1 stroop).
  - `test_no_fee_below_high_water_mark`: gain to 110 (fee), drop to 100, recover to 105 → no new reserve shares.
  - `test_fee_cap_enforced`: propose SetFee(2001) → `FeeTooHigh`.
  - `test_loss_lowers_share_price`: adapter value drops 10% → `share_price` falls ~10%.
  - `test_burn_reserve_restores_value_timelocked`: after loss, propose BurnReserve(all reserve shares) → executes after ETA → share price rises; reserve shares 0.
  - `test_reserve_cannot_transfer_shares`: `transfer(reserve, x, …)` without reserve auth fails (reserve contract never authorizes).
- [ ] **Step 2–4:** FAIL → implement → PASS + clippy.
- [ ] **Step 5:** Commit `feat(savings): high-water-mark fee into a locked reserve`.

### Task 6: Blend adapter values positions in USDC

**Files:**
- Modify: `contracts/adapter_blend/src/lib.rs` (`position_value`), `libs/test_utils/src/mocks/blend_pool.rs` (add `get_reserve(asset) -> Reserve` with `data.b_rate`), `contracts/adapter_blend/src/test.rs`, `src/real_pool_test.rs`

**Interfaces:**
- Produces: `position_value(owner) = units * b_rate / 1_000_000_000_000` (Blend v2 `b_rate` scale 1e12), reading `PoolClient::get_reserve(underlying).data.b_rate`; `withdraw(units, …)` unchanged (units).
- [ ] **Step 1: Failing tests:** `test_position_value_in_underlying_after_rate_growth` (mock b_rate 1.05e12, units 100 → 105); real-pool test asserts value ≥ deposited after interest accrues.
- [ ] **Step 2–4:** FAIL → implement → `cargo test -p adapter-blend-contract` PASS.
- [ ] **Step 5:** Commit `fix(adapter_blend): report position value in underlying via b_rate`.

### Task 7: DeFindex adapter

**Files:**
- Create: `contracts/adapter_defindex/` (Cargo.toml, `src/lib.rs`, `src/test.rs`), `libs/test_utils/src/mocks/defindex_vault.rs` (+ `mod.rs` export)

**Interfaces:**
- Consumes: DeFindex vault interface — verify exact signatures against `github.com/defindex-io/stellar-contracts` (vault: `deposit(amounts_desired: Vec<i128>, amounts_min: Vec<i128>, from: Address, invest: bool)`, `withdraw(withdraw_shares: i128, min_amounts_out: Vec<i128>, from: Address)`, `get_asset_amounts_per_shares(vault_shares: i128) -> Vec<i128>`, SEP-41 `balance`). Mock implements exactly that subset.
- Produces: `YieldAdapter` impl: `initialize(env, core: Address, defindex_vault: Address, underlying: Address)`; `position_value` = `get_asset_amounts_per_shares(balance(self))[0]`; only `core` may call `deposit`/`withdraw`.
- [ ] **Step 1:** Look up a live DeFindex **testnet USDC** vault (DeFindex docs/app, `defindex-io` repos). Record its address in `scripts/deploy-savings-testnet.sh` comments, or note "none found → mock".
- [ ] **Step 2: Failing tests:** `test_deposit_and_value_track_vault_shares`, `test_only_core_can_move_funds`, `test_withdraw_returns_underlying`.
- [ ] **Step 3–4:** implement → PASS.
- [ ] **Step 5:** Commit `feat(adapter_defindex): DeFindex vault adapter`.

### Task 8: End-to-end integration test + testnet deploy script

**Files:**
- Create: `tests/integration/src/integration/savings_tests.rs` (+ register in `integration/mod.rs`), `packages/contracts/scripts/deploy-savings-testnet.sh`

**Interfaces:**
- Consumes: Tasks 2–7.
- Produces: deploy script writing `NEXT_PUBLIC_SAVINGS_CORE_ID`, `NEXT_PUBLIC_SAVINGS_RESERVE_ID`, `NEXT_PUBLIC_ADAPTER_BLEND_ID`, `NEXT_PUBLIC_ADAPTER_DEFINDEX_ID` to `scripts/deployed-testnet.env` (keeps existing keys).
- [ ] **Step 1:** Test `test_savings_lifecycle`: two users deposit; allocator allocates 40% Blend mock / 40% DeFindex mock; yield accrues; `accrue` mints reserve shares; Blend mock loses 20%; share price drops; BurnReserve executes after timelock; both users redeem and receive ≥ their pro-rata share; `cargo test -p nester-integration-tests savings` PASS.
- [ ] **Step 2:** Deploy script: build, optimize, `scripts/check-wasm-size.sh 102400 …`, `stellar contract deploy` each, `initialize` core (owner = deployer, asset = testnet USDC SAC from existing script), reserve, adapters; propose AddAdapter ×2 with abs_cap 1,000 USDC, rel_cap 5000 bps; print the pending-change ids (executing them needs the 3-day timelock, or deploy with `SetTimelock` at a short testnet value first — script flag `--testnet-timelock 600`).
- [ ] **Step 3:** `bash -n scripts/deploy-savings-testnet.sh` passes. (Running it against testnet is a separate, user-approved action.)
- [ ] **Step 4:** Commit `test(savings): lifecycle integration test; add testnet deploy script`.

## Backend

### Task 9: Snapshots table + repository

**Files:**
- Create: `apps/api/migrations/126_savings_snapshots.up.sql` / `.down.sql`; `internal/domain/savings/savings.go`; `internal/repository/postgres/savings_repository.go` (+ `_test.go`, DB-gated like siblings)

**Interfaces:**
- Produces:
  - table `savings_snapshots(id bigserial pk, taken_at timestamptz not null, share_price numeric(38,0) not null, total_assets numeric(38,0) not null, total_supply numeric(38,0) not null, reserve_shares numeric(38,0) not null)` + index on `taken_at`.
  - `type Snapshot struct { TakenAt time.Time; SharePrice, TotalAssets, TotalSupply, ReserveShares decimal.Decimal }`
  - `type SavingsRepository interface { Insert(ctx, Snapshot) error; Since(ctx, t time.Time) ([]Snapshot, error); Latest(ctx) (Snapshot, error) }`
- [ ] **Step 1:** Failing test `TestSavingsRepository_InsertAndSince` (DB-gated by `TEST_DATABASE_DSN`). **Step 2:** FAIL. **Step 3:** implement. **Step 4:** `go test ./internal/repository/postgres -run Savings` PASS; migration check script passes (`go run -tags migrationsafety ./scripts/verify_migrations.go -dir migrations`).
- [ ] **Step 5:** Commit `feat(api): savings snapshots table and repository`.

### Task 10: Chain reader + trailing rate

**Files:**
- Create: `internal/stellar/savings_reader.go` (+ `_test.go` using `internal/chaintest/fakechain.go`); add `TrailingRate` to `internal/domain/savings/savings.go` (+ `savings_test.go`)

**Interfaces:**
- Produces:
  - `type SavingsReader interface { State(ctx) (domain.ChainState, error) }` where `ChainState{ SharePrice, TotalAssets, TotalSupply, Idle, ReserveShares decimal.Decimal; FeeBps uint32; Timelock time.Duration; Adapters []AdapterState{Address string; Value, AbsCap decimal.Decimal; RelCapBps uint32; Name string}; Pending []PendingChange{ID uint32; Kind string; ETA time.Time} }`, read via simulate of core views (`share_price, total_assets, total_supply, idle, reserve_shares, fee_bps, timelock, adapters, allocation, pending_changes`) using the existing `ContractInvoker` simulate path.
  - `func TrailingRate(snaps []Snapshot, window time.Duration, now time.Time) *decimal.Decimal` — annualized `(p_end/p_start)^(365d/elapsed) - 1` using the oldest snapshot within the window and the newest; returns `nil` if fewer than 2 snapshots or elapsed < 24h.
- [ ] **Step 1:** Failing tests: `TestTrailingRate_InsufficientHistory` (1 snapshot → nil; 2 snapshots 1h apart → nil), `TestTrailingRate_Annualizes` (price 1.0000→1.0010 over 7d → ≈ 5.35%), `TestSavingsReader_ParsesState` (fakechain returns fixed views).
- [ ] **Step 2–4:** FAIL → implement → PASS.
- [ ] **Step 5:** Commit `feat(api): savings chain reader and trailing-rate calculation`.

### Task 11: Snapshot job + savings endpoints

**Files:**
- Create: `internal/service/savings_service.go` (+ test), `internal/handler/savings_handler.go` (+ test)
- Modify: `cmd/api/main.go` (config `SAVINGS_CORE_CONTRACT_ID`, `SAVINGS_RESERVE_CONTRACT_ID`; hourly job via existing scheduler; `savingsHandler.Register(mux)`), `docs/api-config.md` (regenerate with `go run ./cmd/config-doc`), `openapi.yaml` (regenerate with `go run ./cmd/openapi-gen`)

**Interfaces:**
- Produces:
  - `GET /api/v1/savings` (public) → `{ rate7d: string|null, rate30d: string|null, sharePrice, totalAssets, idle, feeBps, timelockSeconds, reserve: {shares, value}, adapters: [{address, name, value, absCap, relCapBps, share}], pending: [{id, kind, eta}] }`
  - `GET /api/v1/savings/history?period=7d|30d|90d` (public) → `[{takenAt, sharePrice}]`
  - `GET /api/v1/savings/position` (auth) → `{ shares, value, deposited }` (shares from core `balance(wallet)`, value via `convert_to_assets`).
  - Job `savings.snapshot`: every hour, `reader.State` → `repo.Insert`; skipped (logged) when contract ID unset.
- [ ] **Step 1:** Failing handler tests: `TestSavingsHandler_Summary_NullRatesOnFreshDeploy`, `TestSavingsHandler_History_RejectsBadPeriod` (400), `TestSavingsHandler_Position_RequiresAuth` (401).
- [ ] **Step 2–4:** FAIL → implement → `go test ./internal/handler -run Savings ./internal/service -run Savings` PASS; `go vet ./... && staticcheck ./...` clean; `go run ./cmd/openapi-gen -check openapi.yaml` and `go run ./cmd/config-doc -check ../../docs/api-config.md` pass after regeneration.
- [ ] **Step 5:** Commit `feat(api): savings rate endpoints and hourly snapshot job`.

### Task 12: Index core deposit/withdraw events

**Files:**
- Modify: `cmd/api/main.go` or startup seed — ensure a `vaults` row exists for `SAVINGS_CORE_CONTRACT_ID` (name "Nester Savings Rate", asset USDC) so the indexer's `loadVaultContractIDs` includes it; `internal/stellar/indexer_test.go`

**Interfaces:**
- Consumes: core events from Task 2 (topic[0] `deposit`/`withdraw`, data `{amount, shares}`), which the existing switch in `internal/stellar/indexer.go` already handles.
- [ ] **Step 1:** Failing test `TestIndexer_AppliesSavingsCoreDeposit` feeding a core-shaped event → vault `total_deposited` increases.
- [ ] **Step 2–4:** FAIL → implement (seed + any data-key mapping) → PASS.
- [ ] **Step 5:** Commit `feat(api): index savings core deposit/withdraw events`.

## Dapp

### Task 13: Savings API client, hook, and transactions

**Files:**
- Create: `apps/dapp/frontend/lib/api/savings.ts`, `hooks/useSavings.ts`, `lib/stellar/savings.ts` (+ `__tests__/savings.test.ts`)
- Modify: `.env.example` (+ `NEXT_PUBLIC_SAVINGS_CORE_ID`)

**Interfaces:**
- Produces:
  - `type SavingsSummary` mirroring `GET /api/v1/savings`; `fetchSavingsSummary(): Promise<SavingsSummary>`, `fetchSavingsHistory(period: "7d"|"30d"|"90d")`, `fetchSavingsPosition()`.
  - `useSavings()` → `{ summary, history, position, isLoading, error }` (React Query, 60 s stale).
  - `buildSavingsDeposit({ wallet, amount, minShares }): Promise<string /*xdr*/>`, `buildSavingsRedeem({ wallet, shares, minAssets })`, using the existing simulate/sign/submit helpers in `lib/stellar/transaction.ts` (`assertWalletOnDappNetwork`, `signWithExpiryRebuild`, `submitTransaction`) and core `deposit`/`redeem`.
- [ ] **Step 1:** Failing tests: `fetchSavingsSummary maps null rates`, `buildSavingsDeposit calls core.deposit with min_shares`.
- [ ] **Step 2–4:** `pnpm --filter @nester/dapp exec vitest run lib/__tests__/savings.test.ts` FAIL → implement → PASS.
- [ ] **Step 5:** Commit `feat(dapp): savings API client, hook and transactions`.

### Task 14: Savings-rate components

**Files:**
- Create: `components/savings-rate/{RateHero,SavePanel,PositionCard,AllocationCard,ReserveCard,PendingChangesCard}.tsx` + `__tests__/savings-rate.test.tsx`

**Interfaces:**
- Consumes: `useSavings`, `buildSavingsDeposit/Redeem`.
- Produces components with props from `SavingsSummary`:
  - `RateHero` — big 7-day rate, 30-day beneath, label "Realized rate, last 7 days — not guaranteed"; null → "Not enough history yet".
  - `SavePanel` — Deposit / Withdraw tabs, amount input, preview (shares or USDC), slippage via existing `slippage-control.tsx`; disabled with reason when deposits paused or an adapter is unavailable.
  - `PositionCard` — shares, current USDC value, change since deposit (can be negative; red when negative).
  - `AllocationCard` — idle + each adapter: value, % of total, cap usage bar.
  - `ReserveCard` — reserve value and % of total assets, text "Locked. Can only be used to cover saver losses, after a public timelock."
  - `PendingChangesCard` — each pending change with kind and countdown to ETA; hidden when empty.
- [ ] **Step 1:** Failing tests: `renders not-enough-history state`, `shows negative change in red`, `disables deposit when paused`, `copy never says guaranteed` (assert no `/guarantee|only goes up/i` in rendered text except the "not guaranteed" label).
- [ ] **Step 2–4:** FAIL → implement (existing `components/ui` primitives, Tailwind) → PASS.
- [ ] **Step 5:** Commit `feat(dapp): savings-rate components`.

### Task 15: Rebuild dashboard; retire tier menu

**Files:**
- Modify: `app/dashboard/page.tsx` (compose RateHero, SavePanel, PositionCard, AllocationCard, ReserveCard, PendingChangesCard; keep savings goals + recurring-deposit entry points), `app/vaults/page.tsx` (redirect to `/dashboard`), `components/app-shell.tsx`, `components/bottom-nav.tsx`, `components/navbar.tsx` (nav: Savings Rate `/dashboard`, Goals `/savings`, Portfolio `/portfolio`, Settings; remove Markets/Vaults/Stocks), hide `components/vault/CreateVaultWizard.tsx` entry points
- Modify: savings goals / recurring deposit flows to target `NEXT_PUBLIC_SAVINGS_CORE_ID` instead of tier vault IDs (`lib/vault-contracts.ts`)

- [ ] **Step 1:** Update/replace affected dashboard tests; add `dashboard renders savings rate layout` and `vaults route redirects`.
- [ ] **Step 2–4:** FAIL → implement → `pnpm --filter @nester/dapp run test:coverage` PASS; `pnpm --filter @nester/dapp run lint:security` 0 errors; `npx tsc --noEmit -p apps/dapp/frontend` clean; `pnpm --filter @nester/dapp build` succeeds.
- [ ] **Step 5:** Run locally (docker stack + dapp on :3002), screenshot `/dashboard` with Playwright, check the layout renders with API data or the empty states.
- [ ] **Step 6:** Commit `feat(dapp): single Nester Savings Rate dashboard; retire tier menu`.

### Task 16: Website and docs copy

**Files:**
- Modify (after rebasing onto `chore/scf-credibility-fixes`): `apps/website/src/components/how-it-works.tsx`, `faq.tsx`, `app/docs/content.ts` (Savings Layer → Nester Savings Rate; remove tier table), `README.md` (Smart Vaults section → Nester Savings Rate)

- [ ] **Step 1:** Replace tier references; describe realized rate, reserve and timelocks; keep testnet/unaudited statements.
- [ ] **Step 2:** `pnpm --filter @nester/website lint` (≤ existing 2 warnings) and `build` pass; grep finds no `Conservative|Balanced|Growth` tier copy outside historical docs.
- [ ] **Step 3:** Commit `docs(website): describe the Nester Savings Rate`.
