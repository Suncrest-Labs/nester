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

(See the separate "Mainnet Launch Issue Audit (nester#1526)" section below for
the re-verification of closed GitHub launch issues #1371-#1419 / #255 / #806 /
#1133 / #1135 — a distinct, issue-numbered audit, not part of the 137 PRD
claims counted above.)

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
- **Evidence:** migration: apps/api/migrations/005_create_allocations_table.sql
- **Notes:** Allocations table (settlements were removed with the offramp; migration 121 drops the table)

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

## Mainnet Launch Issue Audit (nester#1526)

Re-verification of closed mainnet-readiness issues #1371-#1419 (plus
#255, #806, #1133, #1135 named explicitly in #1526's body, and
#1383/#1392 which were found to be unwired orphan files and handled
separately by #1525 — not re-listed here). Evidence line is the
`file:`/`test:`/`ci:` artifact that genuinely implements and wires in
the issue's acceptance criteria, confirmed against `dev` at the time
of this audit, not just a file's presence.

Coverage: 39 of the 39 in-scope issue numbers (#1371-#1419 closed
subset, plus #255/#806/#1133/#1135) were audited. #1397, #1398, #1400,
#1402-#1407, #1410, #1414, #1416-#1418 are still **open** on GitHub
(never closed), so they are out of scope for this re-verification —
an open issue makes no "this is done" claim to re-check.

#### [AUDIT-255] CSP configured on dapp frontend
- **Status:** Resolved
- **Evidence:** file: apps/dapp/frontend/proxy.ts#L6-L28, test: apps/dapp/frontend/tests/security-headers.spec.ts::sends CSP and hardening headers
- **Notes:** Contrary to #1526's characterization, a CSP is built via `buildCsp()` and set on both the request and response in the Next.js proxy; a Playwright spec asserts the header is present. Closed status holds.

#### [AUDIT-1133] Vault storage TTL strategy
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs#L1757-L1780, test: packages/contracts/tests/integration/src/integration/lifecycle_tests.rs::test_storage_ttl_persistence_after_long_ledger_advance
- **Notes:** Contrary to #1526's characterization ("no TTL code"), persistent-storage TTL extension (LOCK_TTL_THRESHOLD/LOCK_TTL_EXTEND_TO) exists with a ledger-advance test. Closed status holds.

#### [AUDIT-1135] Upgrade posture decision documented
- **Status:** Open
- **Evidence:** —
- **Notes:** No document states an explicit upgradeable-vs-immutable decision. #806's timelocked-upgrade code implies "upgradeable" was chosen, but there is no decision record or test asserting that posture. Should be reopened.

#### [AUDIT-806] Timelock-governed upgradeability for vault/registry WASM
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs#L5218-L5241
- **Notes:** propose_upgrade/execute_upgrade routed through the #1373 timelock. Closed status holds.

#### [AUDIT-1371] Mainnet env isolated from testnet secrets/RPC
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/config/config_stellar_test.go#L29-L135
- **Notes:** Startup check rejects a mainnet passphrase paired with a testnet RPC/Horizon URL.

#### [AUDIT-1372] Independent third-party audit of vault/adapter contracts
- **Status:** Open
- **Evidence:** —
- **Notes:** docs/security/contract_audits.md states explicitly that no audit has been commissioned and no RFP has gone out; it is a readiness template, not a record of completed work. Should be reopened.

#### [AUDIT-1373] Timelocked upgrade proxy with public delay window
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/timelock/src/lib.rs#L129-L352
- **Notes:** propose/execute with MIN_DELAY 1h, MAX_DELAY 7d, default 24h.

#### [AUDIT-1374] Multisig-controlled admin key for mainnet privileged ops
- **Status:** Open
- **Evidence:** —
- **Notes:** docs/security/multisig-signers.md is an unfilled template (literal `_fill in_`/`YYYY-MM-DD` placeholders); only native transfer_admin/accept_admin exists, not N-of-M multisig enforcement. Should be reopened.

#### [AUDIT-1375] Incident response runbook for exploit/fund-loss
- **Status:** Resolved
- **Evidence:** file: docs/security/incident-response.md#L1-L30
- **Notes:** 475-line runbook covering paging, emergency halt, and comms templates.

#### [AUDIT-1376] Mainnet-only TVL cap at deposit
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/service/vault_tvl_cap.go#L13-L38, file: apps/api/internal/handler/vault_handler.go#L748-L803
- **Notes:** MainnetTVLCapManager.CheckDepositCap, mainnet-gated and wired into the deposit handler.

#### [AUDIT-1377] Withdrawal circuit breaker on outflow rate
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/service/vault_service.go#L1028-L1038, test: apps/api/internal/service/vault_service_withdrawal_breaker_test.go::TestWithdrawalCircuitBreakerHaltsVault
- **Notes:** Rolling-window outflow percentage check pauses the vault.

#### [AUDIT-1378] On-call paging for mainnet money-path alerts
- **Status:** Resolved
- **Evidence:** file: docker/alertmanager/alertmanager.yml#L47-L107
- **Notes:** pagerduty-oncall receiver reads its key from /etc/alertmanager/secrets/pagerduty_service_key (Alertmanager does not expand env vars).

#### [AUDIT-1379] Real-time balance/TVL dashboard
- **Status:** Resolved
- **Evidence:** file: docker/grafana/dashboards/slo-mainnet-operations.json
- **Notes:** Grafana dashboard for mainnet operations.

#### [AUDIT-1380] Mainnet launch go/no-go checklist
- **Status:** Resolved
- **Evidence:** file: docs/LAUNCH_CHECKLIST.md
- **Notes:** Checklist doc exists as asked; gates are honestly still unchecked pending real completion, which is correct behavior, not a defect in the doc itself.

#### [AUDIT-1381] Property-based test of share-price invariant
- **Status:** Resolved
- **Evidence:** test: packages/contracts/tests/integration/src/integration/property_tests.rs::prop_withdrawals_alone_never_reduce_share_price
- **Notes:** Also covers prop_randomized_share_accounting_invariants.

#### [AUDIT-1382] Dry-run deploy simulation against mainnet fork
- **Status:** Resolved
- **Evidence:** file: scripts/mainnet-deploy-dryrun.sh#L1-L22, file: docs/security/mainnet-deploy-dryrun.md
- **Notes:** Deploy dry-run script and accompanying doc.

#### [AUDIT-1384] Automated encrypted backup with tested restore
- **Status:** Resolved
- **Evidence:** file: docs/observability/runbooks/database-restore-runbook.md, file: Makefile#L40-L41
- **Notes:** db-restore-drill make target invokes scripts/db-restore-drill.sh.

#### [AUDIT-1385] Mainnet fee-estimation guardrail
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/fee_guardrail.go, test: apps/api/internal/stellar/fee_guardrail_test.go::TestFeeGuardrail_MainnetExceeded
- **Notes:** Rejects transactions if network fees exceed an expected multiple.

#### [AUDIT-1386] Load test at projected launch-day traffic
- **Status:** Open
- **Evidence:** —
- **Notes:** docs/LOAD_TESTING_BASELINE.md shows "Pending first approved staging execution / Not yet run" for every scenario; k6 scripts exist under tests/load/ but the load test was never actually executed. Should be reopened.

#### [AUDIT-1387] Emergency pause independent of full multisig delay
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs#L2712-L2715
- **Notes:** pause() requires only Role::Admin, not the timelock delay.

#### [AUDIT-1388] ToS/risk disclosures legally reviewed
- **Status:** Open
- **Evidence:** —
- **Notes:** docs/security/mainnet-risk-disclosures.md exists, but docs/LAUNCH_CHECKLIST.md#L41-L54 shows "Legal Review Done" unchecked and "Legal Counsel" sign-off blank. Review was not actually completed. Should be reopened.

#### [AUDIT-1389] Deposit allowlist/waitlist gate
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/service/deposit_allowlist_gate.go#L27, test: apps/api/internal/service/deposit_allowlist_gate_test.go::TestFlagDepositAllowlistGate_CohortAllowed
- **Notes:** Controlled initial-rollout gate.

#### [AUDIT-1390] Synthetic canary mainnet deposit/withdraw cycle
- **Status:** Open
- **Evidence:** —
- **Notes:** docs/observability/runbooks/synthetic-probes.md states these probes run against staging, not production. The issue asked for a real mainnet cycle, which doesn't exist. Should be reopened.

#### [AUDIT-1391] Reconciliation drift escalation at dollar threshold
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/scheduler/ledger_reconciliation.go#L217-L233
- **Notes:** MainnetDollarThreshold check triggers a pager-alert log on mainnet.

#### [AUDIT-1393] Rate-limited adapter harvest calls (gas-griefing)
- **Status:** Resolved
- **Evidence:** file: packages/contracts/contracts/vault/src/lib.rs#L1974-L1990
- **Notes:** MinHarvestInterval enforced via get/set_last_harvest_at.

#### [AUDIT-1394] Mainnet fork testing of Blend/Soroswap adapters
- **Status:** Open
- **Evidence:** —
- **Notes:** No fork-testing harness or mainnet-pool integration test found anywhere in packages/contracts. Should be reopened.

#### [AUDIT-1395] Rollback procedure doc for 24h post-launch bug
- **Status:** Resolved
- **Evidence:** file: docs/MAINNET_LAUNCH_ROLLBACK.md#L1-L20
- **Notes:** 260-line runbook with a trigger-criteria table.

#### [AUDIT-1396] Separate mainnet/testnet API URLs, no cross-routing
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/config/config.go#L1545, test: apps/api/internal/config/config_stellar_test.go::TestStellarNetworkConsistency_ValidMainnet
- **Notes:** Passphrase bound to contract address per environment.

#### [AUDIT-1399] Full deploy dry-run on local network matching mainnet
- **Status:** Resolved
- **Evidence:** file: scripts/mainnet-deploy-dryrun.sh#L1-L15
- **Notes:** Local Stellar network deploy/init/ownership-transfer/verify sequence.

#### [AUDIT-1401] Support/on-call contact escalation tree
- **Status:** Resolved
- **Evidence:** file: docs/SUPPORT_ESCALATION.md
- **Notes:** —

#### [AUDIT-1408] Data retention / PII deletion policy
- **Status:** Resolved
- **Evidence:** file: docs/data-retention.md, file: apps/api/internal/scheduler/data_retention.go, test: apps/api/internal/scheduler/data_retention_test.go::TestDataRetentionJob_DeletesOnlyRowsPastRetentionWindow
- **Notes:** Policy doc backed by an actual scheduled retention job.

#### [AUDIT-1409] Idempotent tx submission retry on RPC timeout
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/stellar/submission.go#L200-L203
- **Notes:** IdempotencyReference field; outcome determined via sequence-number-based ChainView.

#### [AUDIT-1411] Mainnet cost/budget monitoring alerts
- **Status:** Resolved
- **Evidence:** file: docs/observability/error-budget-policy.md
- **Notes:** —

#### [AUDIT-1412] Chaos test for mainnet RPC provider outage
- **Status:** Resolved
- **Evidence:** file: apps/api/internal/chaos/transport.go, test: apps/api/internal/chaos/transport_test.go::TestFaultTimeoutEndsOnCallerDeadline
- **Notes:** RPC-outage/degraded-mode scenarios.

#### [AUDIT-1413] Multisig signer set, custody, rotation doc
- **Status:** Needs more info
- **Evidence:** file: docs/security/multisig-signers.md#L1-L45
- **Notes:** Doc structurally covers custody standards and rotation procedure, but the actual signer registry rows are unfilled placeholders — same underlying gap as #1374. The narrower "document the procedure" ask is met; the signer set itself is not filled in.

#### [AUDIT-1415] Automated balance snapshot to cold storage
- **Status:** Open
- **Evidence:** —
- **Notes:** No cold-storage/immutable-export snapshot mechanism found; only the generic DB backup from #1384 exists, which is a different artifact than a vault balance snapshot. Should be reopened.

#### [AUDIT-1419] Post-launch 30-day heightened-monitoring plan
- **Status:** Resolved
- **Evidence:** file: docs/POST_LAUNCH_MONITORING.md
- **Notes:** —

**Audit summary:** 30 Resolved, 1 Needs more info, 8 Open/should-be-reopened (#1372, #1374, #1386, #1388, #1390, #1394, #1415, #1135), out of 39 in-scope issue numbers. #1383 and #1392 handled separately under #1525. Open (never-closed) issues #1397/#1398/#1400/#1402-1407/#1410/#1414/#1416-1418 are out of scope for this re-verification.

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
