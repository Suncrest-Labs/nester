# Nester — Product Requirements & Progress Tracker

> **Authoritative launch register**: see `docs/launch-readiness-register.md` verified by `scripts/verify-prd-refs.js`.
> **Launch Readiness Verification Rule**: An item is only marked resolved/checked `[x]` when accompanied by a direct source file, test reference, or CI verification. Unverified items remain `[ ]` and are tracked in `docs/backlog/`.

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
- [x] Impairment regression test (vault: loss scenario → zero performance fee) *(Evidence: packages/contracts/contracts/vault/src/test.rs:601,1414,1772)*
- [x] `preview_withdraw` / `preview_withdraw_net` — returns net assets after fee deduction *(Evidence: packages/contracts/contracts/vault/src/lib.rs:4861-4960, test.rs:806-850)*
- [x] Confirm `cargo test -p vault-contract` passes in CI with vault_token.wasm present *(Evidence: .github/workflows/ci.yml:584-610)*
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
- [x] Event indexer: extractEventAmount precision safely handled *(Evidence: apps/api/internal/stellar/indexer.go, migration 103_widen_vault_balance_precision)*
- [x] `GetRoles` — pass raw `uuid.UUID` to pgx, not `id.String()` *(Evidence: apps/api/internal/repository/postgres/user_repository.go:179-183)*
- [x] `bootstrap-admin` — add `db.Ping()` after `sql.Open()` *(Evidence: apps/api/cmd/bootstrap-admin/main.go:55)*
- [x] `bootstrap-admin` — validate Stellar address format before querying *(Evidence: apps/api/cmd/bootstrap-admin/main.go:39-41,81-89)*
- [x] Migration runner — wire `golang-migrate` into API startup behind `RUN_MIGRATIONS=true` flag *(Evidence: apps/api/cmd/api/main.go:220-245, apps/api/internal/config/config.go:567, apps/api/internal/config/config_test.go:1204)*
- [x] Document re-auth requirement after migration 009 deployment *(Evidence: apps/api/migrations/README.md:38-45)*

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
- [x] **[RESOLVED]** Fix migration number collisions: sequential migrations 001-123 without collisions *(Evidence: apps/api/migrations/README.md:47-51)*
- [x] Fix `scripts/seed.sql` — users INSERT matches post-007 schema *(Evidence: scripts/seed.sql:57-59)*
- [x] Add `user_roles` seed row (test user → admin) *(Evidence: scripts/seed.sql:61-63)*

### Dev Environment / Docker

- [x] `docker-compose.yml` — PostgreSQL 16, Redis 7, API, frontend
- [x] API `Dockerfile.dev` with air hot-reload
- [x] Frontend `Dockerfile.dev`
- [x] `Makefile` — `dev`, `dev-logs`, `dev-db`, `dev-down`, `dev-reset` targets
- [x] Fix healthcheck endpoint — docker-compose probes `/healthz`, router handles `/healthz` *(Evidence: docker-compose.yml:78, apps/api/cmd/api/main.go)*
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
- [x] Stocks page — implemented with real portfolio components *(Evidence: apps/dapp/frontend/app/stocks/page.tsx)*
- [x] Error boundaries on all major routes *(Evidence: apps/dapp/frontend/tests/error-boundaries.spec.ts)*
- [x] Offline / network-loss handling *(Evidence: apps/dapp/frontend/hooks/useOfflineStatus.ts, components/offline-banner.tsx)*
- [x] Skeleton loaders on all data-fetching components *(Evidence: apps/dapp/frontend/tests/loading-states.spec.ts)*
- [x] E2E tests (Playwright) *(Evidence: apps/dapp/frontend/tests/smoke.spec.ts, e2e/vault-creation.spec.ts)*
- [x] Accessibility audit (WCAG 2.1 AA) *(Evidence: apps/dapp/frontend/tests/accessibility.spec.ts)*

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

### B-06 — Migration numbering collision — RESOLVED
**Directory:** `apps/api/migrations/`
Resolved. All migration files are sequentially numbered from `001` through `123` with duplicate prefix collisions eliminated and uniqueness checks verified in CI. Evidence: `apps/api/migrations/README.md#L47-L51`.

---

## 🟠 Major Bugs / Incorrect Behaviour

### B-07 — `seed.sql` schema mismatch — RESOLVED
**File:** `scripts/seed.sql`
Resolved. The users table insertion was rewritten to conform to the post-007 schema (`wallet_address`, `display_name`, `kyc_status`) without deprecated `email`/`name` fields, and seeds admin role into `user_roles`. Evidence: `scripts/seed.sql#L57-L63`.

### B-08 — `GetRoles` passes `id.String()` instead of `uuid.UUID` to pgx — RESOLVED
**File:** `apps/api/internal/repository/postgres/user_repository.go`
Resolved. `GetRoles` passes the native `id uuid.UUID` directly to `r.db.QueryContext`, matching driver type-safety requirements. Evidence: `apps/api/internal/repository/postgres/user_repository.go#L179-L183`.

### B-09 — `bootstrap-admin` does not call `db.Ping()` after `sql.Open()` — RESOLVED
**File:** `apps/api/cmd/bootstrap-admin/main.go`
Resolved. Added `db.Ping()` verification immediately following `sql.Open()` and Stellar address validation. Evidence: `apps/api/cmd/bootstrap-admin/main.go#L39-L57`.

### B-10 — `preview_withdraw` returns gross pre-fee amount — RESOLVED
**File:** `packages/contracts/contracts/vault/src/lib.rs`
Resolved. `preview_withdraw_net` was implemented and integrated into rebalancing slippage checks, and gross `preview_withdraw` documentation updated. Evidence: `packages/contracts/contracts/vault/src/lib.rs#L4861-L4960`, `packages/contracts/contracts/vault/src/test.rs#L806-L850`.

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

### B-12 — No migration runner in API startup — RESOLVED
**File:** `apps/api/cmd/api/main.go`
Resolved. `golang-migrate` is wired into API startup behind `RUN_MIGRATIONS=true` env flag. Evidence: `apps/api/cmd/api/main.go#L220-L245`, `apps/api/internal/config/config.go#L567`, `apps/api/internal/config/config_test.go#L1204`.

### B-13 — Healthcheck endpoint inconsistency — RESOLVED
**File:** `docker-compose.yml` vs `apps/api/cmd/api/main.go`
Resolved. `/healthz` is monitored by docker-compose and supported by the API server as canonical liveness endpoint. Evidence: `docker-compose.yml#L78`, `apps/api/cmd/api/main.go`.

---

## 🟡 Potential Issues & Edge Cases

### P-01 — Auth tokens for existing admins lack `Roles` claim after migration 009 — RESOLVED
Resolved. Re-authentication requirement after `009_add_user_roles` documented in the migration runbook. Evidence: `apps/api/migrations/README.md#L38-L45`.

### P-02 — `bootstrap-admin` accepts invalid Stellar addresses silently — RESOLVED
**File:** `apps/api/cmd/bootstrap-admin/main.go`
Resolved. Format check enforces 'G' prefix, length 56, and `keypair.ParseAddress` validation before querying. Evidence: `apps/api/cmd/bootstrap-admin/main.go#L39-L41,L81-L89`.

### P-03 — Missing impairment regression test (vault contract) — RESOLVED
**File:** `packages/contracts/contracts/vault/src/test.rs`
Resolved. Unit and integration tests verify zero performance fee on impairment losses. Evidence: `packages/contracts/contracts/vault/src/test.rs:601,1414,1772`.

### P-04 — `turbo.json` may still reference deprecated `dapp/backend` pipeline
**File:** `turbo.json`
The Express backend was removed but the turbo pipeline may still contain its entries, causing confusing build cache misses or warnings. Verify with `grep -r "dapp/backend" turbo.json`.

### P-05 — Root README still lists React Native in contribution table
**File:** `README.md`, line ~194
The mobile app is Flutter/Dart, not React Native. Misleads contributors looking to work on mobile.

### P-06 — Missing `013` migration (gap between `012` and `014`)
**Directory:** `apps/api/migrations/`
Migration numbering jumps from `012` to `014`. Expected intentional gap documented in `apps/api/migrations/README.md#L47-L51`.

### P-08 — WebSocket connections have no heartbeat / reconnection handling in DApp — RESOLVED
**File:** `apps/dapp/frontend/tests/websocket-reconnect.spec.ts`
Resolved. Ping/pong heartbeat and reconnect backoff implemented and verified with Playwright tests. Evidence: `apps/dapp/frontend/tests/websocket-reconnect.spec.ts`.

### P-09 — In-memory challenge store has no TTL enforcement at scale
**File:** `apps/api/internal/service/challenge_store.go`
Auth challenges stored in memory have a TTL, but the store uses a simple map with no background cleanup goroutine — expired challenges accumulate in memory until the process restarts. Low risk in dev; becomes a memory leak under high auth traffic in production.

### P-10 — Cargo test in CI may pass vacuously without vault_token.wasm artifact — RESOLVED
**File:** `.github/workflows/ci.yml`
Resolved. CI workflow explicitly tests for `vault_token.wasm` artifact before executing `vault-contract` tests and fails if tests fail to run. Evidence: `.github/workflows/ci.yml#L584-L610`.

---

## Enhancements

### E-01 — Add `preview_withdraw_net` to vault contract — RESOLVED
Resolved. Implemented in `packages/contracts/contracts/vault/src/lib.rs#L4956` and tested in `packages/contracts/contracts/vault/src/test.rs#L806-L850`.

### E-02 — Idempotent event processing via `processed_events` table — RESOLVED
Resolved. Schema in `apps/api/migrations/028_create_processed_events_table.up.sql` and verified by `TestIntegrationReplay_DuplicateDelivery`.

### E-03 — Structured `RUN_MIGRATIONS` flag in API — RESOLVED
Resolved. Implemented in `apps/api/cmd/api/main.go#L220-L245` behind `RUN_MIGRATIONS=true`.

### E-04 — DApp: skeleton loaders on all data-fetching routes — RESOLVED
Resolved. Verified by Playwright tests in `apps/dapp/frontend/tests/loading-states.spec.ts`.

### E-05 — DApp: global error boundary with graceful fallback UI — RESOLVED
Resolved. Verified by Playwright tests in `apps/dapp/frontend/tests/error-boundaries.spec.ts`.

### E-06 — DApp: WebSocket heartbeat + auto-reconnect — RESOLVED
Resolved. Verified by Playwright tests in `apps/dapp/frontend/tests/websocket-reconnect.spec.ts`.

### E-07 — Add SAST for TypeScript to CI pipeline
GitHub Actions currently scans Go (gosec), Rust (cargo audit), and Python (bandit) but has no static analysis for Next.js/TypeScript. Add `eslint-plugin-security` or Semgrep with a JS/TS ruleset. Issue: `docs/backlog/typescript-sast.md`.

### E-09 — Add `system_state` table for operational key-value persistence — RESOLVED
Resolved. Implemented in `apps/api/migrations/025_create_system_state_table.up.sql` and used by event indexer.

### E-10 — Stocks page implementation — RESOLVED
Resolved. Implemented in `apps/dapp/frontend/app/stocks/page.tsx`.

### E-11 — Deployment runbook document
Create `docs/DEPLOYMENT.md` covering: migration steps, re-auth requirement after role migrations, env var checklist, contract deployment sequence, rollback procedure.

### E-12 — Mobile app first screen (wallet connect + dashboard)
Flutter scaffold exists; prioritize the auth + dashboard screens to reach feature parity with the web DApp for the Nigeria market.

### E-13 — `bootstrap-admin` Stellar address format validation — RESOLVED
Resolved. Implemented in `apps/api/cmd/bootstrap-admin/main.go#L39-L41,L81-L89`.

### E-14 — DApp: E2E test suite (Playwright) — RESOLVED
Resolved. Implemented in `apps/dapp/frontend/tests/smoke.spec.ts` and `apps/dapp/frontend/e2e/vault-creation.spec.ts`.

