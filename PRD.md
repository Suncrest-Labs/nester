# Nester — Product Requirements & Progress Tracker

> **Internal use only — not tracked in git.**
> Living document: check off items as they ship. Add new items as scope expands.

---

## Project Overview

**Nester** is a decentralized, crypto-first savings & yield investment protocol on Stellar. It automates DeFi yield via Soroban smart vaults, gives users a live portfolio over their positions, and grows deposits with on-chain recurring deposit mandates. The fiat offramp and the AI advisor layer were removed in the September 2026 pivot — the product is the savings/yield/investment core.

**Core value propositions:**
- Optimized yield (8–15% APY) across multiple DeFi protocols, one deposit
- Robinhood-grade portfolio visibility with on-chain truth
- Auto-invest: recurring deposits executed by on-chain mandates
- Non-custodial — users retain full asset ownership

---

## Architecture Layers

| Layer | Stack | Status |
|---|---|---|
| Smart Contracts | Rust / Soroban (Stellar) | Active |
| Backend API | Go + PostgreSQL + Redis | Active |
| Web DApp | Next.js 16 / React 19 / Stellar Freighter | Active |
| Marketing Website | Next.js + Three.js / GSAP | Active |

---

## Phase 1 — Core Savings Vaults

### Smart Contracts (Soroban / Rust)

- [x] `vault` contract — deposit, withdraw, rebalance, yield accrual
- [x] `vault_token` contract — ERC-4626-compatible shares token
- [x] `allocation_strategy` contract — auto-allocate across protocols
- [x] `yield_registry` contract — multi-protocol APY tracking (Aave, Blend, Compound)
- [x] `access_control` contract — RBAC (grant/revoke roles, has_role)
- [x] `nester` contract — primary orchestrator/router (execute_deposit, execute_withdrawal)
- [x] `treasury` contract — fund management & reserves
- [x] `timelock` contract — governance time-lock (schedule/execute/cancel)
- [x] `rent_escrow` contract — escrow lock/release/refund
- [x] Shared `common` lib — constants, errors, events
- [x] Shared `test_utils` lib
- [ ] Impairment regression test (vault: loss scenario → zero performance fee) *(OSS_CLEANUP PR #275)*
- [ ] `preview_withdraw` — fix or document pre-fee gross return vs EIP-4626 `previewRedeem` semantics *(OSS_CLEANUP PR #277)*
- [ ] Confirm `cargo test -p vault-contract` passes in CI with vault_token.wasm present *(OSS_CLEANUP PR #277)*
- [ ] Contract security audit (pending — link when complete)

### Backend API (Go)

- [x] Project scaffolding — Chi router, structured logging (slog), config via env
- [x] PostgreSQL connection pool (pgx/v5)
- [x] Redis client (sessions, challenge store)
- [x] 16 database migrations
- [x] Auth — challenge/verify (Stellar wallet signature + JWT issuance)
- [x] Vault CRUD — create, get, list, allocations
- [x] Transaction queries — get by ID, list by vault
- [x] User profile — get, update
- [x] Admin service — role management, audit logs
- [x] Exchange rate oracle (Stellar Horizon)
- [x] Performance service — APY snapshot history
- [x] Soroban vault chain invoker — smart contract RPC calls
- [x] WebSocket hub — real-time vault balance updates
- [x] Health endpoints (`/health`, `/readyz`, `/health/detailed`)
- [x] CORS, request logging, auth middleware
- [x] `bootstrap-admin` CLI tool
- [x] Event indexer (first pass — unblocked by the deterministic replay harness, issue #1051; see `docs/event-indexer-replay.md`)
- [x] **[B-01]** Event indexer: persist last indexed ledger to DB — cursor is persisted in `system_state` and advanced in the SAME transaction as the balance mutation. Evidence: `TestIntegrationReplay_RestartMidStream`, `TestIntegrationCursorAndBalanceCommitAtomically` *(issue #1051)*
- [x] **[B-02]** Event indexer: seed `startLedger` from current tip, not 0 — cold start derives `tip - offset`, treating both an absent cursor and migration 025's seeded `'0'` as never-indexed. Evidence: `TestColdStart_DerivesValidLedgerFromTip`, `TestColdStart_NeverRequestsLedgerZero` *(issue #1051)*
- [x] **[B-03 / indexer idempotency]** Event indexer: balance updates are idempotent — `processed_events.event_id` PRIMARY KEY claimed via `ON CONFLICT DO NOTHING` inside the mutation transaction. Evidence: `TestIntegrationReplay_DuplicateDelivery`, `TestIntegrationReplay_RepeatedFullReplay` *(issue #1051)*
- [x] Event indexer: logic lives in `internal/stellar/EventPoller.PollEvents`; `startEventIndexer` retains only scheduling and telemetry *(issue #1051)*
- [ ] Event indexer: remove `float64` case in `extractEventAmount` (precision loss on large integers) *(OSS_CLEANUP PR #276)*
- [ ] Event indexer: unit tests for `applyIndexedEvent` and `extractEventAmount` *(OSS_CLEANUP PR #276)*
- [ ] `GetRoles` — pass raw `uuid.UUID` to pgx, not `id.String()` *(OSS_CLEANUP PR #270)*
- [ ] `bootstrap-admin` — add `db.Ping()` after `sql.Open()` *(OSS_CLEANUP PR #270)*
- [ ] `bootstrap-admin` — validate Stellar address format before querying *(OSS_CLEANUP PR #270)*
- [ ] Migration runner — wire `golang-migrate` (or equivalent) into API startup behind `RUN_MIGRATIONS=true` flag *(OSS_CLEANUP General)*
- [ ] Document re-auth requirement after migration 009 deployment (existing admin tokens lack Roles claim) *(OSS_CLEANUP PR #270)*

### Database / Migrations

- [x] `001` — users table
- [x] `002` — vaults table
- [x] `003` — transactions table
- [x] `005` — allocations table
- [x] `006` — settlements table *(dropped by 118 in the pivot)*
- [x] `007` — vault soft-delete (`deleted_at`)
- [x] `007` — users table update (wallet_address, kyc_status, rename name→display_name, drop email)
- [x] `008` — vault_transactions table
- [x] `009` — confirmed_at on transactions
- [x] `009` — user_roles table
- [x] `010` — audit_logs table
- [x] `010` — event indexer tables
- [x] `011` — sessions table
- [x] `012` — vault_transactions update
- [x] `014` — missing columns backfill
- [x] `015` — indices and constraints
- [x] `016` — vault_performance table
- [ ] **[BLOCKING]** Fix migration number collisions: two `007` files, two `009` files, two `010` files *(OSS_CLEANUP General)*
- [ ] Fix `scripts/seed.sql` — users INSERT still references old `email`/`name` columns (migration 007 removed them) *(OSS_CLEANUP PR #268)*
- [ ] Add `user_roles` seed row (test user → admin) after PR #270 merges *(OSS_CLEANUP PR #268)*

### Dev Environment / Docker

- [x] `docker-compose.yml` — PostgreSQL 16, Redis 7, API, frontend
- [x] API `Dockerfile.dev` with air hot-reload
- [x] Frontend `Dockerfile.dev`
- [x] `Makefile` — `dev`, `dev-logs`, `dev-db`, `dev-down`, `dev-reset` targets
- [ ] Fix healthcheck endpoint — compose probes `/healthz` but README/router uses `/health` *(OSS_CLEANUP PR #268)*
- [ ] Add `AUTH_JWT_SECRET` (dev placeholder) to compose API service environment block *(OSS_CLEANUP PR #268)*
- [ ] Frontend Dockerfile: switch from `npm ci` to `pnpm install --frozen-lockfile` *(OSS_CLEANUP PR #268)*
- [ ] Go version pin: bump Dockerfiles from `golang:1.24-alpine` to `golang:1.25-alpine` once image is on Docker Hub *(OSS_CLEANUP PR #268)*

---

## Phase 2 — Automated Rebalancing + Yield Depth

- [ ] Automated rebalancing engine — triggered by APY threshold drift
- [ ] Rebalancing scheduler in Go API
- [ ] Slippage protection integration with `preview_withdraw_net` (post-fee)
- [ ] Real protocol integrations behind `adapter_lending` / `adapter_pool`

---

## Phase 3 — In-App Swaps & Recurring Buys (v2)

- [ ] DEX swap routing via the `lp_aggregator` contract (path payments)
- [ ] Swap UI in the dApp (buy/sell assets from the portfolio)
- [ ] Recurring token buys on top of the `recurring_deposit` mandate engine
- [ ] Multi-hop routing and slippage caps surfaced in the UI

---

## Phase 4 — Mainstream Onboarding (v3)

- [ ] Third-party fiat onramp widget (MoonPay/Transak-style; provider handles KYC)
- [ ] Non-custodial MPC embedded wallets alongside external wallets
- [ ] Curated asset baskets (index-like allocations, one-click buy)

---

## Phase 5 — Expansion

- [ ] Multi-currency vault denomination
- [ ] Deeper protocol integrations (more yield sources per tier)
- [ ] Localization beyond en/fr

---

## Web DApp

- [x] Stellar Freighter wallet connection
- [x] Stellar Wallets Kit integration
- [x] Challenge/verify auth flow
- [x] Protected routes
- [x] Dashboard — portfolio stats, recent activity, vault positions table
- [x] Vault list page — create vault, drill-down
- [x] Vault detail — allocations, deposit modal, performance chart
- [x] Savings page
- [x] Portfolio page with Recharts visualizations
- [x] Notifications page
- [x] Stocks page (stub)
- [x] Animated balance display
- [x] Guided onboarding tour + welcome modal
- [x] Network selector (testnet / mainnet toggle)
- [x] Responsive layout — bottom nav for mobile
- [x] Dark mode (Tailwind)
- [x] React Query data fetching
- [ ] Stocks page — implement real content (currently stub)
- [ ] Error boundaries on all major routes
- [ ] Offline / network-loss handling
- [ ] Skeleton loaders on all data-fetching components
- [ ] E2E tests (Playwright or Cypress)
- [ ] Accessibility audit (WCAG 2.1 AA)

---

## Marketing Website

- [x] Landing page with 3D / Three.js animations
- [x] GSAP + Babylon.js scroll effects
- [x] Lenis smooth scrolling
- [x] Docs content file
- [ ] Audit docs for stale references to deprecated Express backend
- [ ] Add audit reports page (pending external audit)
- [ ] SEO metadata / Open Graph tags audit

---

## CI/CD & Security

- [x] GitHub Actions CI — change detection, conditional jobs
- [x] Go unit + integration tests (with PostgreSQL + Redis services)
- [x] Rust WASM build + cargo test
- [x] Python lint (ruff) + type-check (mypy) + pytest
- [x] Next.js build + npm audit
- [x] gitleaks — secret detection
- [x] govulncheck — Go dependency vulnerabilities
- [x] gosec — Go security issues (medium+)
- [x] cargo audit — Rust dependency vulnerabilities
- [x] pip-audit — Python dependency vulnerabilities
- [x] bandit — Python security issues
- [x] Dependabot (all ecosystems silenced with open-pull-requests-limit: 0)
- [x] CODEOWNERS — all code owned by @0xDeon
- [ ] Contract audit — external security review
- [ ] Load / stress testing plan
- [ ] Penetration test (money-path + auth endpoints)
- [ ] SAST integration for TypeScript/Next.js (currently no JS/TS security scanner in CI)

---

## Cross-Cutting / Technical Debt

- [ ] Remove remaining `dapp/backend` references from `turbo.json` (deprecated Express) *(OSS_CLEANUP PR #269)*
- [ ] Remove remaining `dapp/backend` references from root `README.md` *(OSS_CLEANUP PR #269)*
- [ ] README: update roadmap — Phases 3 & 4 are largely implemented, not "Planned" *(current state is outdated)*
- [ ] README: mention React Native → correct to Flutter in contribution table *(backend section lists React Native)*
- [ ] API: missing `013` migration (gap between `012` and `014` — verify intentional or accidental skip)

---

## Bugs & Potential Issues

See **Diagnosis** section below.

---

---

# Codebase Diagnosis

> Complete audit of bugs, security issues, and enhancement opportunities as of 2026-05-18.

---

## 🔴 Critical Bugs (Data Corruption / Security)

### B-01 — Event Indexer: in-memory cursor resets on restart (data corruption)
**File:** `apps/api/cmd/api/main.go` — `startEventIndexer` / `startLedger`
`startLedger` is a local `uint64` initialized to `0` on every restart. All balance update SQL is additive (`total_deposited + amount`). Any restart replays all historical events and **doubles every vault balance**. Fix: persist last indexed ledger sequence in a `system_state` DB table; read on startup.

### B-02 — Event Indexer: `startLedger = 0` triggers Stellar RPC error (indexer never starts)
**File:** `apps/api/cmd/api/main.go`
Stellar `getEvents` rejects ledger sequence `0`. On first boot with no persisted cursor, the indexer fails immediately. Fix: on first boot, seed from the current ledger tip.

### B-06 — Migration numbering collision (migration runner will corrupt schema)
**Directory:** `apps/api/migrations/`
Three pairs of files share the same numeric prefix (`007`, `009`, `010`). Any lexicographic migration runner applies them in undefined order and may apply wrong schema changes or skip others. Fix: renumber colliding migrations consistently before running in production.

---

## 🟠 Major Bugs / Incorrect Behaviour

### B-07 — `seed.sql` schema mismatch (dev environment setup fails)
**File:** `scripts/seed.sql`
The users INSERT still references `email` and `name` columns removed in migration `007_update_users_table`. Running `make dev` on a fresh clone will error at seeding. Fix: rewrite the users block to match the post-007 schema (`wallet_address`, `display_name`, `kyc_status`).

### B-08 — `GetRoles` passes `id.String()` instead of `uuid.UUID` to pgx
**File:** `apps/api/internal/repository/postgres/user_repository.go`
pgx v5 handles `uuid.UUID` natively; passing `.String()` forces implicit server-side cast and breaks the driver's type safety guarantee. Every other query in the repo passes raw UUIDs — this is an inconsistency that can surface as unexpected query errors under stricter pg configs.

### B-09 — `bootstrap-admin` does not call `db.Ping()` after `sql.Open()`
**File:** `apps/api/cmd/bootstrap-admin/main.go`
`sql.Open` only validates DSN syntax; it does not connect. A bad DSN or unreachable host surfaces as a confusing query error rather than a clear connection failure. Fix: add `db.Ping()` with a wrapped error.

### B-10 — `preview_withdraw` returns gross pre-fee amount (breaks slippage protection)
**File:** `packages/contracts/contracts/vault/src/lib.rs`
Returns `amount_for_shares(shares)` before management, early-withdrawal, and performance fees. Any DApp passing this directly as `min_assets_out` will hit `SlippageExceeded` on every fee-bearing withdrawal. Fix: either add `preview_withdraw_net` that applies fee estimates on-chain, or document explicitly and have the DApp subtract fees.

### B-11 — `float64` precision loss in `extractEventAmount` — RESOLVED
**File:** `apps/api/internal/stellar/indexer.go`

Resolved in two parts, by two separate changes:

**Parsing (pre-dates issue #1051).** Amounts decode via `UseNumber()` into
`decimal.Decimal`, so they arrive as `json.Number` and never pass through
`float64`. The `case float64` branch is a bounds check, not a pure guard: it
*rejects* values that are non-integral or exceed 2^53 (where precision is
already lost), and *converts* smaller values via `int64(v)`, which is exact in
that range. This branch only sees stray `float64` inputs, since the RPC path
yields `json.Number`.

**Persistence (issue #1051).** Migration `103` widens the vault balance columns
from `NUMERIC(20,8)` to `NUMERIC(48,8)`. This is the part that was still
broken: the old type allowed only 12 integer digits, so a 1e18 stroop deposit
raised `numeric field overflow` and the event was rejected outright. Parsing
had been correct; storage was not.

Evidence: `TestIntegrationLargeAmountRoundTripsExactly` (end-to-end round-trip
through the real schema), `TestExtractEventAmount_RejectsUnsafeFloat64`, and
`TestAmountPathHasNoFloat64` (source-level guard against reintroducing a
`float64` conversion).

Original finding:
A `float64` case is handled for Soroban event amounts. `float64` loses precision above 2^53 — Soroban amounts come as strings and can exceed this range. Fix: treat any non-string amount type as unparseable; only accept `string`.

### B-12 — No migration runner in API startup (new migrations silently skipped in dev)
**File:** `apps/api/` (no migration runner wired)
Migrations only apply via `scripts/seed.sql` on Docker `initdb`. New migrations added after initial setup require `make dev-reset` (wipes all data). Fix: wire `golang-migrate` behind a `RUN_MIGRATIONS=true` flag so incremental migrations apply without data loss.

### B-13 — Healthcheck endpoint inconsistency (`/healthz` vs `/health`)
**File:** `docker-compose.yml` vs `apps/api/cmd/api/main.go`
docker-compose probes `/healthz`; README and API docs say `/health`. One of them is wrong — if the Go router serves `/health` only, the compose healthcheck never passes and the API container never becomes `healthy`. Fix: verify the actual router and align everything.

---

## 🟡 Potential Issues & Edge Cases

### P-01 — Auth tokens for existing admins lack `Roles` claim after migration 009
After migration `009_add_user_roles` deploys, all active JWT tokens were issued without a `Roles` field. Admins using those tokens will hit authorization failures silently until they re-authenticate. Needs a deployment runbook entry.

### P-02 — `bootstrap-admin` accepts invalid Stellar addresses silently
**File:** `apps/api/cmd/bootstrap-admin/main.go`
A typo in the wallet address produces `no user found` rather than `invalid address format`. A basic format check (`G` prefix, 56 chars) would surface the error before hitting the DB.

### P-03 — Missing impairment regression test (vault contract)
**File:** `packages/contracts/contracts/vault/src/test.rs`
The vault handles losses correctly (`yield_part < 0` → fee skipped), but there is no test proving this. Without it, a future refactor can silently break the zero-fee invariant under impairment.

### P-04 — `turbo.json` may still reference deprecated `dapp/backend` pipeline
**File:** `turbo.json`
The Express backend was removed but the turbo pipeline may still contain its entries, causing confusing build cache misses or warnings. Verify with `grep -r "dapp/backend" turbo.json`.

### P-05 — Root README still lists React Native in contribution table
**File:** `README.md`, line ~194
The mobile app is Flutter/Dart, not React Native. Misleads contributors looking to work on mobile.

### P-06 — Missing `013` migration (gap between `012` and `014`)
**Directory:** `apps/api/migrations/`
Migration numbering jumps from `012` to `014`. If this was an intentional deletion, it should be documented. If accidental, the missing migration may have left a schema gap that `014_add_missing_columns` is patching around.

### P-08 — WebSocket connections have no heartbeat / reconnection handling in DApp
**File:** `apps/dapp/frontend/` (WebSocket client)
Real-time balance updates via WebSocket will silently fail after network interruptions with no automatic reconnect. Users would see stale balances until page refresh.

### P-09 — In-memory challenge store has no TTL enforcement at scale
**File:** `apps/api/internal/service/challenge_store.go`
Auth challenges stored in memory have a TTL, but the store uses a simple map with no background cleanup goroutine — expired challenges accumulate in memory until the process restarts. Low risk in dev; becomes a memory leak under high auth traffic in production.

### P-10 — Cargo test in CI may pass vacuously without vault_token.wasm artifact
**File:** `.github/workflows/ci.yml`
The contributor left "verify CI passes" unchecked in PR #277. Integration tests that depend on the compiled WASM artifact may skip silently if the artifact isn't built first, giving a false green.

---

## Enhancements

### E-01 — Add `preview_withdraw_net` to vault contract
Return net amount after all fees so the DApp can pass it directly as `min_assets_out` without manual fee math. Aligns with EIP-4626 `previewRedeem` semantics.

### E-02 — Idempotent event processing via `processed_events` table
Store `(event_id, ledger_sequence)` in a dedicated table. Check before applying any balance mutation. This is a prerequisite for safe event indexer operation (B-01 / B-02).

### E-03 — Structured `RUN_MIGRATIONS` flag in API
Add an env-flag-gated migration runner so `make dev` (or a fresh deployment) applies all pending migrations without wiping data. Removes the `make dev-reset` requirement for schema changes.

### E-04 — DApp: skeleton loaders on all data-fetching routes
Replace blank loading states with Tailwind skeleton placeholders to prevent layout shift and improve perceived performance.

### E-05 — DApp: global error boundary with graceful fallback UI
Wrap routes in React error boundaries so a single component crash doesn't take down the entire page.

### E-06 — DApp: WebSocket heartbeat + auto-reconnect
Add a `ping/pong` heartbeat on the WebSocket client, with exponential-backoff reconnect logic. Show a "reconnecting…" badge in the UI when the connection drops.

### E-07 — Add SAST for TypeScript to CI pipeline
GitHub Actions currently scans Go (gosec), Rust (cargo audit), and Python (bandit) but has no static analysis for Next.js/TypeScript. Add `eslint-plugin-security` or Semgrep with a JS/TS ruleset.

### E-09 — Add `system_state` table for operational key-value persistence
A generic `(key TEXT PRIMARY KEY, value TEXT, updated_at TIMESTAMPTZ)` table would solve both the event indexer cursor (B-01) and any future startup-state needs without adding one-off tables.

### E-10 — Stocks page implementation
The `/stocks` route is a stub kept as a teaser for the v2/v3 investing surface AI or integrate a public equities/crypto data feed.

### E-11 — Deployment runbook document
Create `docs/DEPLOYMENT.md` covering: migration steps, re-auth requirement after role migrations, env var checklist, contract deployment sequence, rollback procedure.

### E-12 — Mobile app first screen (wallet connect + dashboard)
Flutter scaffold exists; prioritize the auth + dashboard screens to reach feature parity with the web DApp for the Nigeria market.

### E-13 — `bootstrap-admin` Stellar address format validation
Before hitting the DB: `strings.HasPrefix(wallet, "G") && len(wallet) == 56`. Saves a round-trip on typo inputs.

### E-14 — DApp: E2E test suite (Playwright)
Add Playwright tests for the golden paths: wallet connect → create vault → deposit → view dashboard → initiate offramp.

