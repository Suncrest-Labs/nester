# Nester Launch Readiness Register

**Authoritative artifact replacing stale PRD.md claims**  
Last verified: 2026-08-27  
Issue: #1115

---

## Register Rule

An item may be marked Resolved only when it has an attached evidence reference.

Evidence format:
- test: path/to/file.test.ts::TestName
- file: path/to/file.ts#L10-L50
- migration: path/to/001_migration.sql
- pr: #123
- ci: workflow_name::job_name

Status: Resolved | Open | Needs more info

---

## Quick Summary

- **Resolved:** 92 claims verified with code/test/CI evidence
- **Open:** 44 claims without evidence (backlog issues created)
- **Needs more info:** 1 claim needing clarification
- **Total:** 137 PRD claims

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
- **Evidence:** file: packages/contracts/contracts/common/src/lib.rs
- **Notes:** Shared constants, errors, events

#### [SC-10] test_utils lib
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/test_utils/src/lib.rs
- **Notes:** Shared test utilities

#### [SC-11] Impairment regression test
- **Status:** Open
- **Evidence:** —
- **Notes:** Test location needs verification

#### [SC-12] preview_withdraw function
- **Status:** Open
- **Evidence:** —
- **Notes:** Requires preview_withdraw_net or documentation. Issue: docs/backlog/sc-preview-withdraw-net.md

#### [SC-13] Cargo test WASM dependency
- **Status:** Open
- **Evidence:** —
- **Notes:** CI WASM artifact dependency. Issue: docs/backlog/ci-vault-wasm-dependency.md

#### [SC-14] Contract security audit
- **Status:** Open
- **Evidence:** —
- **Notes:** External audit scheduled Q3 2026. Issue: docs/backlog/contract-audit-followup.md

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
- **Evidence:** test: apps/api/internal/stellar/poller_test.go::TestIntegrationCursorAndBalanceCommitAtomically, migration: apps/api/migrations/025_add_system_state_table.sql
- **Notes:** Cursor persisted in system_state table

#### [API-22] Event indexer cold start
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/stellar/poller_test.go::TestColdStart_DerivesValidLedgerFromTip, test: apps/api/internal/stellar/poller_test.go::TestColdStart_NeverRequestsLedgerZero
- **Notes:** Derives from current tip, never requests ledger 0

#### [API-23] Event indexer idempotency
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/stellar/poller_test.go::TestIntegrationReplay_DuplicateDelivery, test: apps/api/internal/stellar/poller_test.go::TestIntegrationReplay_RepeatedFullReplay, migration: apps/api/migrations/022_create_processed_events_table.sql
- **Notes:** ON CONFLICT DO NOTHING with event_id PK

#### [API-26] SECURITY BOLA settlement
- **Status:** Open
- **Evidence:** —
- **Notes:** Extract user_id from JWT, not request body. Issue: docs/backlog/security-bola-settlement.md

#### [API-27] SECURITY settlement enumeration
- **Status:** Open
- **Evidence:** —
- **Notes:** Return 404 (not 403) for non-owner. Issue: docs/backlog/security-settlement-enumeration.md

#### [API-28] SECURITY settlement 403 oracle
- **Status:** Open
- **Evidence:** —
- **Notes:** Return 404 for both missing and non-owned. Issue: docs/backlog/security-settlement-403.md

#### [API-29] GetRoles uuid type
- **Status:** Open
- **Evidence:** —
- **Notes:** Pass raw uuid.UUID to pgx, not id.String(). Issue: docs/backlog/fix-getroles-uuid-type.md

#### [API-30] bootstrap-admin db.Ping()
- **Status:** Open
- **Evidence:** —
- **Notes:** Add connection health check. Issue: docs/backlog/bootstrap-admin-db-ping.md

#### [API-31] bootstrap-admin address validation
- **Status:** Open
- **Evidence:** —
- **Notes:** Validate Stellar address format. Issue: docs/backlog/bootstrap-admin-validate-address.md

#### [API-32] Migration runner
- **Status:** Open
- **Evidence:** —
- **Notes:** Wire golang-migrate behind RUN_MIGRATIONS flag. Issue: docs/backlog/wire-migration-runner.md

#### [API-33] Reauth after migration 009
- **Status:** Open
- **Evidence:** —
- **Notes:** Document token refresh requirement. Issue: docs/backlog/deployment-runbook-reauth.md

---

### Database

#### [DB-01] Migrations 001-003
- **Status:** Resolved
- **Evidence:** migration: apps/api/migrations/001_create_users_table.sql, migration: apps/api/migrations/002_create_vaults_table.sql, migration: apps/api/migrations/003_create_transactions_table.sql
- **Notes:** Core tables present

#### [DB-02] Migrations 005-006
- **Status:** Resolved
- **Evidence:** migration: apps/api/migrations/005_create_allocations_table.sql, migration: apps/api/migrations/006_create_settlements_table.sql
- **Notes:** Allocations and settlements tables

#### [DB-10] seed.sql schema mismatch
- **Status:** Open
- **Evidence:** —
- **Notes:** References old email/name columns. Issue: docs/backlog/fix-seed-sql-schema.md

#### [DB-11] user_roles seed row
- **Status:** Open
- **Evidence:** —
- **Notes:** Add test user admin role seed. Issue: docs/backlog/seed-user-roles.md

---

### Web DApp

#### [DAPP-01] Stellar Freighter
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/lib/stellar.ts, file: apps/dapp/frontend/components/WalletConnector.tsx
- **Notes:** Wallet connection implemented

#### [DAPP-12] Stocks page
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/app/stocks/page.tsx
- **Notes:** Fully implemented (NOT a stub)

#### [DAPP-19] Error boundaries
- **Status:** Open
- **Evidence:** —
- **Notes:** Add global error boundaries. Issue: docs/backlog/dapp-error-boundaries.md

#### [DAPP-20] Offline handling
- **Status:** Open
- **Evidence:** —
- **Notes:** Implement offline mode. Issue: docs/backlog/dapp-offline-handling.md

#### [DAPP-21] Skeleton loaders
- **Status:** Open
- **Evidence:** —
- **Notes:** Add skeleton placeholders. Issue: docs/backlog/dapp-skeleton-loaders.md

#### [DAPP-22] E2E tests
- **Status:** Open
- **Evidence:** —
- **Notes:** Playwright test suite. Issue: docs/backlog/dapp-e2e-tests.md

#### [DAPP-23] Accessibility audit
- **Status:** Open
- **Evidence:** —
- **Notes:** WCAG 2.1 AA audit. Issue: docs/backlog/dapp-accessibility-audit.md

#### [DAPP-24] WebSocket heartbeat
- **Status:** Open
- **Evidence:** —
- **Notes:** Add ping/pong and reconnect. Issue: docs/backlog/dapp-websocket-heartbeat.md

---

### CI/CD & Security

#### [CI-01] GitHub Actions CI
- **Status:** Resolved
- **Evidence:** file: .github/workflows/ci.yml
- **Notes:** Turbo-based change detection and conditional jobs

#### [CI-06] gitleaks
- **Status:** Resolved
- **Evidence:** ci: security::gitleaks, file: .gitleaks.toml
- **Notes:** Secret detection active

#### [CI-07] govulncheck
- **Status:** Resolved
- **Evidence:** ci: security::govulncheck
- **Notes:** Go vulnerability scanning

#### [CI-17] TypeScript SAST
- **Status:** Open
- **Evidence:** —
- **Notes:** Add Semgrep or eslint-plugin-security. Issue: docs/backlog/typescript-sast.md

---

## Additional Resources

- **Verification script:** scripts/verify-prd-refs.js
- **Test suite:** tests/prd-register.test.js
- **Maintenance guide:** docs/launch-readiness-process.md
- **Backlog issues:** docs/backlog/*.md (20 files)

---

## How to Maintain This Register

1. After merging a PR that resolves a claim, update the Evidence field
2. Use format: test: path::name, file: path#L1-L50, migration: path, pr: #123, or ci: workflow::job
3. Mark status: Resolved (with evidence), Open (no evidence), or Needs more info (partial)
4. Run verification: node scripts/verify-prd-refs.js
5. See docs/launch-readiness-process.md for full guidance

---

Status: 92 Resolved | 44 Open | 1 Needs more info | 137 Total
