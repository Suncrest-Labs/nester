# Nester Launch Readiness Register

**Authoritative artifact replacing stale PRD.md claims**  
Last verified: 2026-10-07  
Issue: #1115

---

## Register Rule

An item is only checked or marked Resolved when accompanied by a direct source file, test reference, migration, PR, or CI verification.

Evidence format:
- test: path/to/file.test.ts::TestName
- file: path/to/file.ts#L10-L50
- migration: path/to/001_migration.up.sql
- pr: #123
- ci: workflow_name::job_name

Status: Resolved | Open | Needs more info

---

## Quick Summary

- **Resolved:** 43 claims verified with code/test/CI evidence
- **Open:** 2 claims without evidence (backlog issues created)
- **Needs more info:** 0 claims needing clarification
- **Total:** 45 PRD claims

---

## Phase 1 - Core Savings Vaults

### Smart Contracts

#### [SC-01] vault contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs
- **Notes:** Deposit, withdraw, rebalance, yield accrual implemented

#### [SC-02] vault_token contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault_token/src/lib.rs
- **Notes:** ERC-4626-compatible shares token

#### [SC-03] allocation_strategy contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/allocation_strategy/src/lib.rs
- **Notes:** Auto-allocate across protocols

#### [SC-04] yield_registry contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/yield_registry/src/lib.rs
- **Notes:** Multi-protocol APY tracking (Aave, Blend, Compound)

#### [SC-05] access_control contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/access_control/src/lib.rs
- **Notes:** RBAC implementation

#### [SC-06] nester contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/nester/src/lib.rs
- **Notes:** Primary orchestrator and router

#### [SC-07] treasury contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/treasury/src/lib.rs
- **Notes:** Fund management and reserves

#### [SC-08] timelock contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/timelock/src/lib.rs
- **Notes:** Governance time-lock

#### [SC-09] common lib
- **Status:** Resolved
- **Evidence:** file: packages/contracts/libs/common/src/lib.rs
- **Notes:** Shared constants, errors, events

#### [SC-10] test_utils lib
- **Status:** Resolved
- **Evidence:** file: packages/contracts/libs/test_utils/src/lib.rs
- **Notes:** Shared test utilities

#### [SC-11] Impairment regression test
- **Status:** Resolved
- **Evidence:** test: packages/contracts/contracts/vault/src/test.rs::impairment_charges_zero_performance_fee, test: packages/contracts/contracts/vault/src/test.rs::withdrawal_charges_no_perf_fee_on_impairment
- **Notes:** Zero performance fee invariant under loss/impairment covered by unit tests (PR #275, issue #451)

#### [SC-12] preview_withdraw function
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs#L4861-L4960, test: packages/contracts/contracts/vault/src/test.rs::preview_withdraw_net_le_preview_withdraw
- **Notes:** preview_withdraw_net implemented returning net assets after fees; gross preview docs clarify fee subtraction semantics

#### [SC-13] Cargo test WASM dependency
- **Status:** Resolved
- **Evidence:** ci: ci::contracts, file: .github/workflows/ci.yml#L584-L610
- **Notes:** CI workflow verifies vault_token.wasm exists before executing vault-contract test suite

#### [SC-14] Contract security audit
- **Status:** Open
- **Evidence:** —
- **Notes:** External audit scheduled prior to mainnet deployment. Issue: docs/backlog/contract-audit-followup.md

---

### Backend API (Go)

#### [API-01] Project scaffolding
- **Status:** Resolved
- **Evidence:** file: apps/api/cmd/api/main.go, file: apps/api/internal/config/config.go
- **Notes:** Chi router, slog logging, env config

#### [API-02] PostgreSQL pool (pgx/v5)
- **Status:** Resolved
- **Evidence:** file: apps/api/go.mod, file: apps/api/internal/config/config.go
- **Notes:** Connection pool configured

#### [API-03] Redis client
- **Status:** Resolved
- **Evidence:** file: apps/api/go.mod, file: apps/api/internal/service/auth_service.go
- **Notes:** Sessions and challenge store

#### [API-05] Auth challenge/verify
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/service/auth_service_test.go::TestAuthService_GenerateChallenge, test: apps/api/internal/service/auth_service_test.go::TestAuthService_VerifyAndIssue_Success
- **Notes:** Stellar signature verification and JWT issuance

#### [API-20] Event indexer implementation
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/poller.go, file: apps/api/internal/stellar/indexer.go
- **Notes:** EventPoller with deterministic replay harness

#### [API-21] Event indexer cursor persistence
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/stellar/poller_test.go::TestIntegrationCursorAndBalanceCommitAtomically, migration: apps/api/migrations/025_create_system_state_table.up.sql
- **Notes:** Cursor persisted in system_state table within the same transaction as balance updates

#### [API-22] Event indexer cold start
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/stellar/poller_test.go::TestColdStart_DerivesValidLedgerFromTip, test: apps/api/internal/stellar/poller_test.go::TestColdStart_NeverRequestsLedgerZero
- **Notes:** Derives from current tip, never requests ledger 0

#### [API-23] Event indexer idempotency
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/stellar/replay_harness_test.go::TestIntegrationReplay_DuplicateDelivery, test: apps/api/internal/stellar/replay_harness_test.go::TestIntegrationReplay_RepeatedFullReplay, migration: apps/api/migrations/028_create_processed_events_table.up.sql
- **Notes:** ON CONFLICT DO NOTHING with event_id PK ensures idempotent balance mutations

#### [API-29] GetRoles uuid type
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/repository/postgres/user_repository.go#L179-L183
- **Notes:** Passes native uuid.UUID directly to QueryContext without .String() conversion

#### [API-30] bootstrap-admin db.Ping()
- **Status:** Resolved
- **Evidence:** file: apps/api/cmd/bootstrap-admin/main.go#L55
- **Notes:** Calls db.Ping() immediately after sql.Open() to verify connection health

#### [API-31] bootstrap-admin address validation
- **Status:** Resolved
- **Evidence:** file: apps/api/cmd/bootstrap-admin/main.go#L39-L41
- **Notes:** Validates Stellar address starts with 'G', has 56 chars, and passes keypair.ParseAddress

#### [API-32] Migration runner
- **Status:** Resolved
- **Evidence:** file: apps/api/cmd/api/main.go#L220-L245, test: apps/api/internal/config/config_test.go::TestLoadRunMigrationsFlag
- **Notes:** Automatically runs pending migrations on startup when RUN_MIGRATIONS=true

#### [API-33] Reauth after migration 009
- **Status:** Resolved
- **Evidence:** file: apps/api/migrations/README.md#L38-L45
- **Notes:** Documented admin JWT re-authentication requirements after user_roles migration in migration runbook

---

### Database

#### [DB-01] Migrations 001-003
- **Status:** Resolved
- **Evidence:** migration: apps/api/migrations/001_create_users_table.up.sql, migration: apps/api/migrations/002_create_vaults_table.up.sql, migration: apps/api/migrations/003_create_transactions_table.up.sql
- **Notes:** Core database schema migrations present

#### [DB-02] Migrations 005-006
- **Status:** Resolved
- **Evidence:** migration: apps/api/migrations/005_create_allocations_table.up.sql
- **Notes:** Allocations table present; legacy settlements removed in protocol pivot

#### [DB-03] Migration number collisions
- **Status:** Resolved
- **Evidence:** file: apps/api/migrations/README.md#L47-L51
- **Notes:** All migration numbers sequentially ordered from 001 to 123; collision checks strictly enforced in CI

#### [DB-10] seed.sql schema mismatch
- **Status:** Resolved
- **Evidence:** file: scripts/seed.sql#L57-L59
- **Notes:** Seed script users table INSERT matches current schema (wallet_address, display_name)

#### [DB-11] user_roles seed row
- **Status:** Resolved
- **Evidence:** file: scripts/seed.sql#L61-L63
- **Notes:** Test user admin role seeded in user_roles table

---

### Web DApp

#### [DAPP-01] Stellar Freighter
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/lib/stellar/wallet-network.ts, file: apps/dapp/frontend/components/connect-wallet.tsx
- **Notes:** Stellar Freighter and Wallets Kit integration implemented

#### [DAPP-12] Stocks page
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/app/stocks/page.tsx
- **Notes:** Stocks portfolio page implemented with watchlist and quote components

#### [DAPP-19] Error boundaries
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/tests/error-boundaries.spec.ts
- **Notes:** Error boundaries protect major views and are validated with deterministic test scenarios

#### [DAPP-20] Offline handling
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/hooks/useOfflineStatus.ts, file: apps/dapp/frontend/components/offline-banner.tsx
- **Notes:** useOfflineStatus hook and OfflineBanner component handle network loss gracefully

#### [DAPP-21] Skeleton loaders
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/tests/loading-states.spec.ts
- **Notes:** Skeleton placeholders prevent layout shifts during async data loading

#### [DAPP-22] E2E tests
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/tests/smoke.spec.ts, file: apps/dapp/frontend/e2e/vault-creation.spec.ts
- **Notes:** End-to-end smoke test suite and vault creation flows gating deployments

#### [DAPP-23] Accessibility audit
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/tests/accessibility.spec.ts
- **Notes:** WCAG 2.1 AA automated scans via AxeBuilder covering dashboard, vaults, and savings pages

#### [DAPP-24] WebSocket heartbeat
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/tests/websocket-reconnect.spec.ts
- **Notes:** WebSocket client features application-level ping/pong keepalive and auto-reconnect backoff

---

### Security

#### [SEC-01] BOLA / Authorization matrix
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/middleware/authz_matrix_test.go::TestAuthorizationMatrix, file: apps/api/internal/middleware/authz_matrix_test.go
- **Notes:** Broken Object Level Authorization (BOLA) mitigated via strict route ownership middleware and comprehensive authorization matrix tests

---

### CI/CD & Tooling

#### [CI-01] GitHub Actions CI
- **Status:** Resolved
- **Evidence:** file: .github/workflows/ci.yml
- **Notes:** Turbo-based change detection, parallel jobs, and conditional test execution

#### [CI-06] gitleaks
- **Status:** Resolved
- **Evidence:** ci: security::gitleaks, file: .gitleaks.toml
- **Notes:** Secret detection active in security scanning workflow

#### [CI-07] govulncheck
- **Status:** Resolved
- **Evidence:** ci: security::govulncheck
- **Notes:** Go vulnerability scanning active with pinned scanner version and .vulnignore policy

#### [CI-17] TypeScript SAST
- **Status:** Open
- **Evidence:** —
- **Notes:** Static analysis security scanning for TypeScript/Next.js code. Issue: docs/backlog/typescript-sast.md

---

## Additional Resources

- **Verification script:** scripts/verify-prd-refs.js
- **Test suite:** tests/prd-register.test.js
- **Maintenance guide:** docs/launch-readiness-process.md
- **Backlog issues:** docs/backlog/*.md

---

## How to Maintain This Register

1. After merging a PR that resolves a claim, update the Evidence field
2. Use format: test: path::name, file: path#L1-L50, migration: path, pr: #123, or ci: workflow::job
3. Mark status: Resolved (with evidence), Open (no evidence), or Needs more info (partial)
4. Run verification: node scripts/verify-prd-refs.js
5. See docs/launch-readiness-process.md for full guidance

---

Status: 43 Resolved | 2 Open | 0 Needs more info | 45 Total
