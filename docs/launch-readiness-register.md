# Nester Launch Readiness Register

**Authoritative artifact replacing stale PRD.md claims**  
Last verified: 2026-10-18  
Issue: #1115, #1526

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

- **Resolved:** 95 claims verified with code/test/CI evidence
- **Open:** 42 claims without evidence (reopened or tracked in backlog issues)
- **Needs more info:** 0 claims needing clarification
- **Total:** 137 PRD & Launch Readiness claims

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

#### [SC-11] adapter_blend contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/adapter_blend/src/lib.rs
- **Notes:** Blend lending pool adapter

#### [SC-12] adapter_soroswap contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/adapter_soroswap/src/lib.rs
- **Notes:** Soroswap DEX liquidity adapter

#### [SC-13] adapter_lending contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/adapter_lending/src/lib.rs
- **Notes:** Generic lending adapter

#### [SC-14] adapter_pool contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/adapter_pool/src/lib.rs
- **Notes:** Pool strategy adapter

#### [SC-15] vault_factory contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault_factory/src/lib.rs
- **Notes:** Governed vault deployment factory

#### [SC-16] savings_goal contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/savings_goal/src/lib.rs
- **Notes:** Goal-based savings contract

#### [SC-17] goal_rewards contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/goal_rewards/src/lib.rs
- **Notes:** Savings goal rewards distribution

#### [SC-18] recurring_deposit contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/recurring_deposit/src/lib.rs
- **Notes:** Scheduled recurring vault deposits

#### [SC-19] referral contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/referral/src/lib.rs
- **Notes:** On-chain referral tracking

#### [SC-20] rent_escrow contract
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/rent_escrow/src/lib.rs
- **Notes:** Escrow management contract

#### [SC-21] Timelock-governed upgradeability (#806)
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs, file: packages/contracts/contracts/yield_registry/src/lib.rs
- **Notes:** Issue #806 verified on dev (`propose_upgrade`, `execute_upgrade`, `cancel_upgrade`)

#### [SC-22] Upgrade policy documentation (#1135)
- **Status:** Resolved
- **Evidence:** file: docs/security/upgrade-policy.md
- **Notes:** Issue #1135 verified on dev

#### [SC-23] Storage TTL extension strategy (#1133)
- **Status:** Open
- **Evidence:** —
- **Notes:** Re-verification (#1526) found `STORAGE_TTL_STRATEGY.md` exists without `extend_ttl` calls in contract code; tracked in #1469 and #1485

#### [SC-24] Impairment regression test
- **Status:** Open
- **Evidence:** —
- **Notes:** Test location needs verification

#### [SC-25] preview_withdraw function
- **Status:** Open
- **Evidence:** —
- **Notes:** Requires preview_withdraw_net or documentation. Issue: docs/backlog/sc-preview-withdraw-net.md

#### [SC-26] Cargo test WASM dependency
- **Status:** Open
- **Evidence:** —
- **Notes:** CI WASM artifact dependency. Issue: docs/backlog/ci-vault-wasm-dependency.md

#### [SC-27] Contract security audit
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

#### [API-04] Health and readiness endpoints
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/health.go, file: apps/api/internal/stellar/health_test.go
- **Notes:** Liveness and readiness probes

#### [API-05] Auth challenge/verify
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/service/auth_service_test.go::TestAuthService_GenerateChallenge, test: apps/api/internal/service/auth_service_test.go::TestAuthService_VerifyAndIssue_Success
- **Notes:** Stellar signature verification and JWT issuance

#### [API-06] Isolated transaction signer service
- **Status:** Resolved
- **Evidence:** file: apps/api/cmd/signer/main.go, file: apps/api/internal/signing/signer.go
- **Notes:** Separate signer binary with policy, kill switch, and replay guard

#### [API-07] Signing policy enforcement
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/signing/policy.go, file: apps/api/internal/signing/policy_test.go
- **Notes:** Contract allowlist, operation allowlist, and amount cap

#### [API-08] Signer kill switch
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/signing/killswitch.go, file: apps/api/internal/signing/gameday_test.go
- **Notes:** File-backed emergency signing halt

#### [API-09] Tamper-evident audit log
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/signing/audit.go
- **Notes:** Structured audit log sink and hash-chained audit verification

#### [API-10] Envelope encryption at rest
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/crypto/account_cipher.go, file: apps/api/internal/crypto/account_cipher_test.go
- **Notes:** Versioned AES-256-GCM encryption and blind indexes

#### [API-11] Rate limiting middleware
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/ratelimit.go, file: apps/api/internal/middleware/ratelimit_test.go
- **Notes:** Per-IP and per-route token bucket rate limiting

#### [API-12] Idempotency middleware
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/idempotency.go, test: apps/api/internal/middleware/idempotency_test.go::TestIdempotency
- **Notes:** Prevents duplicate mutating operations

#### [API-13] Auth lockout middleware
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/authlockout.go, test: apps/api/internal/middleware/authlockout_test.go::TestAuthLockout
- **Notes:** Brute-force protection on authentication endpoints

#### [API-14] Authorization matrix coverage
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/authz_rules.go, file: apps/api/internal/middleware/authz_matrix_test.go
- **Notes:** Role-based access rules verified across routes

#### [API-15] Wallet binding verification
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/wallet_binding.go, test: apps/api/internal/middleware/wallet_binding_test.go::TestWalletBinding
- **Notes:** Ensures authenticated user owns target wallet

#### [API-16] Maintenance mode middleware
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/maintenance.go, test: apps/api/internal/middleware/maintenance_test.go::TestMaintenance
- **Notes:** Global and money-path maintenance switches

#### [API-17] CORS security policy
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/cors.go, test: apps/api/internal/middleware/cors_test.go::TestCORS
- **Notes:** Strict origin validation

#### [API-18] OpenTelemetry request tracing
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/tracing.go, test: apps/api/internal/middleware/tracing_test.go::TestTracing
- **Notes:** Distributed trace propagation

#### [API-19] Freshness headers middleware
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/middleware/freshness.go, file: apps/api/internal/middleware/freshness_test.go
- **Notes:** Data staleness signaling

#### [API-20] Event indexer implementation
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/poller.go, file: apps/api/internal/stellar/indexer.go
- **Notes:** EventPoller with deterministic replay harness

#### [API-21] Event indexer cursor persistence
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/stellar/poller_test.go::TestIntegrationCursorAndBalanceCommitAtomically, migration: apps/api/migrations/025_create_system_state_table.up.sql
- **Notes:** Cursor persisted in system_state table

#### [API-22] Event indexer cold start
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/stellar/poller_test.go::TestColdStart_DerivesValidLedgerFromTip, test: apps/api/internal/stellar/poller_test.go::TestColdStart_NeverRequestsLedgerZero
- **Notes:** Derives from current tip, never requests ledger 0

#### [API-23] Event indexer idempotency
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/stellar/poller_test.go::TestIntegrationPollStopsAtFailedEvent, migration: apps/api/migrations/022_create_event_indexer_tables.up.sql
- **Notes:** ON CONFLICT DO NOTHING with event_id PK

#### [API-24] Backfill service
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/backfill.go, file: apps/api/internal/stellar/backfill_test.go
- **Notes:** Historical ledger event backfill

#### [API-25] Balance ownership verification
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/balance_ownership.go, file: apps/api/internal/stellar/balance_ownership_test.go
- **Notes:** Verifies on-chain share ownership

#### [API-26] Chain event verifier
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/chain_event_verifier.go, file: apps/api/internal/stellar/chain_event_verifier_test.go
- **Notes:** Validates deposit/withdrawal events against RPC

#### [API-27] Deposit observer
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/deposit_observer.go, file: apps/api/internal/stellar/deposit_observer_test.go
- **Notes:** Monitors incoming vault deposits

#### [API-28] Slippage guard in stellar invoker
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/slippage.go, file: apps/api/internal/stellar/slippage_test.go
- **Notes:** Slippage bounds calculation

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
- **Evidence:** migration: apps/api/migrations/001_create_users_table.up.sql, migration: apps/api/migrations/002_create_vaults_table.up.sql, migration: apps/api/migrations/003_create_transactions_table.up.sql
- **Notes:** Core tables present

#### [DB-02] Migrations 005-006
- **Status:** Resolved
- **Evidence:** migration: apps/api/migrations/005_create_allocations_table.up.sql
- **Notes:** Allocations table

#### [DB-03] Vaults and transactions schema
- **Status:** Resolved
- **Evidence:** migration: apps/api/migrations/002_create_vaults_table.up.sql, migration: apps/api/migrations/003_create_transactions_table.up.sql
- **Notes:** Core financial ledger tables

#### [DB-04] Event indexer tables
- **Status:** Resolved
- **Evidence:** migration: apps/api/migrations/022_create_event_indexer_tables.up.sql
- **Notes:** Indexed events and cursor tracking

#### [DB-05] System state table
- **Status:** Resolved
- **Evidence:** migration: apps/api/migrations/025_create_system_state_table.up.sql
- **Notes:** Key-value system state persistence

#### [DB-10] seed.sql schema mismatch
- **Status:** Open
- **Evidence:** —
- **Notes:** References old email/name columns. Issue: docs/backlog/fix-seed-sql-schema.md

#### [DB-11] user_roles seed row
- **Status:** Open
- **Evidence:** —
- **Notes:** Add test user admin role seed. Issue: docs/backlog/seed-user-roles.md

---

### Web DApp & Marketing Website

#### [DAPP-01] Stellar Wallet Integration
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/lib/stellar/transaction.ts, file: apps/dapp/frontend/components/wallet-provider.tsx
- **Notes:** Wallet connection implemented

#### [DAPP-02] Network configuration & selector
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/lib/networks.ts, file: apps/dapp/frontend/components/network/NetworkSelector.tsx
- **Notes:** Testnet and Mainnet configuration

#### [DAPP-03] Vault harvest & action modals
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/components/vault/HarvestModal.tsx
- **Notes:** Vault modal flow UI

#### [DAPP-04] Portfolio dashboard
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/app/dashboard/page.tsx
- **Notes:** User portfolio overview

#### [DAPP-05] Vaults explorer page
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/app/vaults/page.tsx
- **Notes:** Vault listing and APY breakdown

#### [DAPP-06] Savings goals page
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/app/savings/page.tsx
- **Notes:** Savings goals management

#### [DAPP-07] Settings page
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/app/settings/page.tsx
- **Notes:** User preferences and wallet settings

#### [DAPP-08] Global styles and layout
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/app/globals.css
- **Notes:** Frontend styling system

#### [DAPP-09] Lighthouse CI configuration
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/lighthouserc.json
- **Notes:** Performance and accessibility budgets

#### [DAPP-10] Frontend package manifest
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/package.json
- **Notes:** Frontend dependencies and test scripts

#### [DAPP-11] Soroban transaction builder
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/lib/stellar/transaction.ts
- **Notes:** Soroban XDR construction and simulation

#### [DAPP-12] Stocks page
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/app/stocks/page.tsx
- **Notes:** Fully implemented (NOT a stub)

#### [DAPP-13] Website landing page
- **Status:** Resolved
- **Evidence:** file: apps/website/src/app/page.tsx
- **Notes:** Public marketing site

#### [DAPP-14] Website security & audit page
- **Status:** Resolved
- **Evidence:** file: apps/website/src/app/security/page.tsx
- **Notes:** Public security posture and bug bounty page

#### [DAPP-15] Content-Security-Policy headers (#255)
- **Status:** Open
- **Evidence:** —
- **Notes:** Re-verification (#1526) found CSP missing in `apps/dapp/frontend/next.config.ts`; tracked as regression in #1504

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

#### [CI-02] Security workflow
- **Status:** Resolved
- **Evidence:** file: .github/workflows/security.yml
- **Notes:** Automated vulnerability and secret scanning

#### [CI-03] Staging deployment workflow
- **Status:** Resolved
- **Evidence:** file: .github/workflows/deploy-staging.yml
- **Notes:** Staging deployment pipeline

#### [CI-04] Synthetic probes workflow
- **Status:** Resolved
- **Evidence:** file: .github/workflows/synthetic-probes.yml
- **Notes:** Scheduled endpoint health probes

#### [CI-05] Vulnerability waiver list
- **Status:** Resolved
- **Evidence:** file: .vulnignore
- **Notes:** Time-bounded vulnerability exceptions

#### [CI-06] gitleaks
- **Status:** Resolved
- **Evidence:** ci: security::gitleaks, file: .gitleaks.toml
- **Notes:** Secret detection active

#### [CI-07] govulncheck
- **Status:** Resolved
- **Evidence:** ci: security::govulncheck
- **Notes:** Go vulnerability scanning

#### [CI-08] Security policy documentation
- **Status:** Resolved
- **Evidence:** file: SECURITY.md
- **Notes:** Responsible disclosure, RBAC, and encryption docs

#### [CI-09] Audit threat model
- **Status:** Resolved
- **Evidence:** file: docs/security/threat-model.md, file: docs/security/contract-invariant-threat-model.md
- **Notes:** Smart contract and protocol threat model

#### [CI-10] Signing isolation architecture
- **Status:** Resolved
- **Evidence:** file: docs/security/signing-isolation.md
- **Notes:** Isolated transaction signer documentation

#### [CI-17] TypeScript SAST
- **Status:** Open
- **Evidence:** —
- **Notes:** Add Semgrep or eslint-plugin-security. Issue: docs/backlog/typescript-sast.md

---

## Re-Verification of Closed Mainnet-Readiness Issues #1371–#1419 (Issue #1526)

#### [LNCH-1371] Separate mainnet environment config (#1371)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/config/config.go, file: apps/api/internal/config/config_stellar_test.go
- **Notes:** Separate Mainnet vs Testnet environment validation

#### [LNCH-1372] Independent third-party audit tracking (#1372)
- **Status:** Resolved
- **Evidence:** file: docs/security/contract_audits.md
- **Notes:** Audit scope and findings tracker documented; external audit completion still gated by `LAUNCH_CHECKLIST.md`

#### [LNCH-1373] Timelocked upgrade proxy with public delay window (#1373)
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/timelock/src/lib.rs, file: packages/contracts/contracts/vault/src/lib.rs
- **Notes:** Timelock delay enforced on vault upgrades

#### [LNCH-1374] Multisig-controlled admin key (#1374)
- **Status:** Resolved
- **Evidence:** file: docs/security/multisig-signers.md
- **Notes:** Multisig governance model documented

#### [LNCH-1375] Incident response runbook & game-day rehearsal (#1375)
- **Status:** Resolved
- **Evidence:** file: docs/security/incident-response.md, file: docs/security/game-day.md
- **Notes:** Runbook and game-day drills recorded

#### [LNCH-1376] Mainnet-only hard cap on TVL per vault (#1376)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/domain/caps/model.go, file: apps/api/internal/domain/caps/model_test.go
- **Notes:** Per-vault TVL cap model and tests

#### [LNCH-1377] Withdrawal circuit breaker on rolling outflow rate (#1377)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/domain/vault/breaker.go, test: apps/api/internal/service/vault_service_withdrawal_breaker_test.go::TestWithdrawalCircuitBreaker
- **Notes:** Outflow velocity circuit breaker verified

#### [LNCH-1378] On-call rotation and paging setup for money-path alerts (#1378)
- **Status:** Resolved
- **Evidence:** file: deploy/observability/oncall-rotation.md, file: docker/alertmanager/alertmanager.yml
- **Notes:** Alertmanager routing and on-call schedule

#### [LNCH-1379] Real-time balance/TVL dashboard for mainnet operations (#1379)
- **Status:** Resolved
- **Evidence:** file: docker/grafana/dashboards/slo-mainnet-operations.json
- **Notes:** Grafana operations dashboard

#### [LNCH-1380] Mainnet launch checklist gating go/no-go decision (#1380)
- **Status:** Resolved
- **Evidence:** file: docs/LAUNCH_CHECKLIST.md
- **Notes:** Mandatory Go/No-Go checklist

#### [LNCH-1381] Property-based testing of share-price invariant (#1381)
- **Status:** Resolved
- **Evidence:** file: packages/contracts/tests/integration/src/integration/property_tests.rs
- **Notes:** Proptest suite covering share-price invariants

#### [LNCH-1382] Dry-run/simulation mode for mainnet deploy scripts (#1382)
- **Status:** Resolved
- **Evidence:** file: apps/api/scripts/simulate_mainnet_deploy.go
- **Notes:** Mainnet deployment simulation utility

#### [LNCH-1383] Rotate and vault all mainnet secrets in secrets manager (#1383)
- **Status:** Open
- **Evidence:** —
- **Notes:** Re-verification (#1526) found `src/secrets/vault.ts` is an un-imported root-level orphan file; tracked in #1525 and #1531

#### [LNCH-1384] Automated daily encrypted database backup and restore (#1384)
- **Status:** Resolved
- **Evidence:** file: scripts/db-backup.sh, file: scripts/db-restore.sh, file: docs/observability/runbooks/database-restore-runbook.md
- **Notes:** Encrypted backup/restore scripts and runbook

#### [LNCH-1385] Mainnet fee-estimation guardrail (#1385)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/fee_guardrail.go, test: apps/api/internal/stellar/fee_guardrail_test.go::TestFeeGuardrail
- **Notes:** Rejects anomalous network fee spikes

#### [LNCH-1386] Load test API and job queue at projected launch traffic (#1386)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/service/launch_load_test.go, file: docs/LOAD_TESTING_BASELINE.md
- **Notes:** Launch traffic load tests and baseline report

#### [LNCH-1387] Emergency pause function independently callable (#1387)
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs, file: packages/contracts/contracts/access_control/src/lib.rs
- **Notes:** Soroban `Guardian` role can pause vault immediately (`contracts/Vault.sol` from PR #1427 was an orphan Solidity file, superseded by Soroban `Guardian` pause in `packages/contracts/contracts/vault/src/lib.rs`)

#### [LNCH-1388] Terms of service and risk disclosures before mainnet deposit (#1388)
- **Status:** Resolved
- **Evidence:** file: docs/security/mainnet-risk-disclosures.md
- **Notes:** Mainnet risk disclosures documented

#### [LNCH-1389] Mainnet deposit allowlist/waitlist gate (#1389)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/service/deposit_allowlist_gate.go, file: apps/api/internal/service/deposit_allowlist_gate_test.go
- **Notes:** Controlled rollout deposit allowlist gate

#### [LNCH-1390] Synthetic monitoring canary for deposit/withdraw cycle (#1390)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/scheduler/canary.go, test: apps/api/internal/scheduler/canary_test.go::TestCanary
- **Notes:** Synthetic canary scheduler and metrics

#### [LNCH-1391] Reconciliation alert escalation on ledger-vs-chain drift (#1391)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/scheduler/ledger_reconciliation.go, test: apps/api/internal/scheduler/ledger_reconciliation_test.go::TestLedgerReconciliation
- **Notes:** Drift threshold escalation verified

#### [LNCH-1392] Stellar mainnet account reserve monitoring (#1392)
- **Status:** Open
- **Evidence:** —
- **Notes:** Re-verification (#1526) found `monitoring/alerts/stellar_reserve.yml` is not wired into `docker/prometheus/prometheus.yml` `rule_files`; tracked in #1525

#### [LNCH-1393] Rate-limit protocol adapter harvest calls (#1393)
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs
- **Notes:** Harvest cooldown/rate-limit enforcement in vault

#### [LNCH-1394] Adapter integration testing (#1394)
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/adapter_blend/src/lib.rs, file: packages/contracts/contracts/adapter_soroswap/src/lib.rs
- **Notes:** Blend and Soroswap adapter tests

#### [LNCH-1395] Mainnet 24h rollback runbook (#1395)
- **Status:** Resolved
- **Evidence:** file: docs/MAINNET_LAUNCH_ROLLBACK.md
- **Notes:** Rollback runbook within 24h of launch

#### [LNCH-1396] Separate mainnet and testnet API base URLs and routing guard (#1396)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/config/config.go, file: apps/api/internal/config/config_stellar_test.go
- **Notes:** Cross-environment routing prevention

#### [LNCH-1397] Mainnet structured logging retention & audit log storage (#1397)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1397 pending implementation

#### [LNCH-1398] Configurable max single-tx deposit/withdrawal limit (#1398)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1398 pending implementation

#### [LNCH-1399] Full mainnet contract deploy dry-run (#1399)
- **Status:** Resolved
- **Evidence:** file: docs/security/mainnet-deploy-dryrun.md, file: scripts/mainnet-deploy-dryrun.sh
- **Notes:** Local Stellar network deploy dry-run script and report

#### [LNCH-1400] Post-deploy bytecode hash verification job (#1400)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1400 pending implementation

#### [LNCH-1401] Mainnet support/on-call escalation tree (#1401)
- **Status:** Resolved
- **Evidence:** file: docs/SUPPORT_ESCALATION.md, file: docs/SUPPORT_TRIAGE_GUIDE.md, file: docs/LAUNCH_GO_NO_GO.md
- **Notes:** Support escalation tree and triage guide

#### [LNCH-1402] Mainnet DB connection pool and read-replica sizing (#1402)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1402 pending implementation

#### [LNCH-1403] Admin dashboard view for paused vaults (#1403)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1403 pending implementation

#### [LNCH-1404] Public API surface penetration test (#1404)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1404 pending external/pre-launch execution (`docs/security/pentest-report-v1.md` baseline exists)

#### [LNCH-1405] Mainnet stricter session and MFA for admin accounts (#1405)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1405 pending implementation

#### [LNCH-1406] Confirm Dependabot and CI security gates enabled (#1406)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1406 and #794 pending implementation

#### [LNCH-1407] Staging environment mirroring mainnet topology (#1407)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1407 pending implementation

#### [LNCH-1408] Data retention and user data deletion policy review (#1408)
- **Status:** Resolved
- **Evidence:** file: docs/data-retention.md
- **Notes:** Data retention and GDPR deletion policy documented

#### [LNCH-1409] Mainnet transaction submission retry policy with idempotency (#1409)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/submission.go, file: apps/api/internal/stellar/submission_test.go
- **Notes:** Idempotent transaction submission store and retry policy

#### [LNCH-1410] Explicit max slippage/price-impact guard on adapter swaps (#1410)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1410 pending implementation

#### [LNCH-1411] Mainnet cost monitoring and budget alerts (#1411)
- **Status:** Resolved
- **Evidence:** file: docker/prometheus/rules/slo_alerts.yml
- **Notes:** Infrastructure and SLO budget alerts

#### [LNCH-1412] Chaos test simulating mainnet RPC provider outage (#1412)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/oracle/chaos_scenario_test.go
- **Notes:** RPC outage graceful degradation test

#### [LNCH-1413] Multisig signer set, key custody, and rotation procedure (#1413)
- **Status:** Resolved
- **Evidence:** file: docs/security/multisig-signers.md, file: docs/security/key-rotation.md
- **Notes:** Signer registry and rotation runbook

#### [LNCH-1414] Mainnet webhook endpoint allowlisting against SSRF (#1414)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1414 pending implementation

#### [LNCH-1415] Automated mainnet vault balance snapshot to cold storage (#1415)
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/scheduler/ledger_reconciliation.go
- **Notes:** Vault balance reconciliation and snapshot scheduling

#### [LNCH-1416] Rate-limited audited support-tooling balance debug endpoint (#1416)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1416 pending implementation

#### [LNCH-1417] Verify Stellar mainnet RPC SLA and failover provider (#1417)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1417 pending implementation

#### [LNCH-1418] Launch-day feature flag to disable new deposits (#1418)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1418 pending implementation

#### [LNCH-1419] Post-launch 30-day heightened-monitoring plan (#1419)
- **Status:** Resolved
- **Evidence:** file: docs/POST_LAUNCH_MONITORING.md
- **Notes:** 30-day daily manual review checklist and rota

#### [LNCH-1420] Confirm contract admin ownership cannot be renounced (#1420)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1420 pending implementation

#### [LNCH-1421] Mainnet error budget and SLO definition for API availability (#1421)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1421 pending implementation

#### [LNCH-1469] Contract storage TTL extension across all contracts (#1469)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1469 pending implementation

#### [LNCH-1470] Move vault_token balances to persistent storage (#1470)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1470 pending implementation

#### [LNCH-1471] Move referral per-user state to persistent storage (#1471)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1471 pending implementation

#### [LNCH-1472] Move savings_goal per-user state to persistent storage (#1472)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1472 pending implementation

#### [LNCH-1473] Move goal_rewards state to persistent storage (#1473)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1473 pending implementation

#### [LNCH-1474] Require timelock on nester::upgrade (#1474)
- **Status:** Open
- **Evidence:** —
- **Notes:** Open issue #1474 pending implementation

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
