# Nester — Post-OSS Campaign Cleanup Log

> **Purpose:** Running list of minor gaps, incomplete items, and polish work left behind by OSS contributors.
> OSS contributors are scoped to the issue — they ship what the issue asks for, not the surrounding system.
> This file is the maintainer's (0xDeon) personal queue to close after each campaign wave.
>
> **Key:** 🔴 Must fix before merge (blocking) | 🟠 Fix soon after merge | 🟡 Nice to have / polish

---

## How to Use This File

1. During PR review, drop items here instead of blocking the contributor on minor things.
2. After the OSS wave closes, work through these top-to-bottom.
3. Strike through items as they are resolved with the commit SHA.

---

## Docker Compose / Dev Environment (`feat/issue-154`) — PR #268

### 🔴 Blocking (fix before merge — request changes on PR)

- [ ] **seed.sql users table is based on migration 001, not the full chain.**
  Migration `007_update_users_table` drops `email`, renames `name` → `display_name`,
  adds `wallet_address TEXT NOT NULL`, adds `kyc_status TEXT NOT NULL DEFAULT 'pending'`.
  The current seed INSERT references `email` and `name` — both will error against the live schema.
  Fix: rewrite users block in `scripts/seed.sql` to match the post-007 schema and update the INSERT.

- [ ] **API healthcheck endpoint mismatch.**
  `docker-compose.yml` probes `/healthz` but the README, PR description, and issue all say `/health`.
  One of them is wrong. Verify the actual Go router and make everything consistent.

### 🟠 Fix Soon After Merge

- [ ] **Frontend `Dockerfile.dev` uses npm in a pnpm monorepo.**
  `apps/dapp/frontend/Dockerfile.dev` runs `npm ci` with `package-lock.json`.
  The workspace is managed by pnpm. Should use `pnpm install --frozen-lockfile` to guarantee
  the same dependency tree CI and root installs resolve.

- [ ] **`JWT_SECRET` (or equivalent auth secret) not set in compose API service.**
  The issue template included it. Without it, the API either panics on startup or falls back to
  an empty/default secret, making all dev tokens trivially forgeable. Add it to `docker-compose.yml`
  with a dev placeholder comment.

- [ ] **Go version pin drift.**
  `apps/api/go.mod` targets Go 1.25.0 but both Dockerfiles use `golang:1.24-alpine` (1.25 not yet
  on Docker Hub). Track this — bump Dockerfiles to `golang:1.25-alpine` once the image is published.

### 🟡 Polish

- [ ] **seed.sql does not include `user_roles` table** (introduced in PR #270).
  After PR #270 merges, `scripts/seed.sql` needs a `CREATE TABLE IF NOT EXISTS user_roles` block
  and a seed row granting the test user an admin role so the full admin flow is exercisable locally.

---

## Remove Deprecated Express Backend (`chore/issue-215`) — PR #269

### 🟠 Fix Soon After Merge

- [ ] **Verify `turbo.json` has no remaining `dapp/backend` pipeline entries.**
  The issue called this out. The PR description doesn't confirm it was checked.
  Run: `grep -r "dapp/backend" turbo.json`

- [ ] **Verify root `README.md` has no remaining references to the old Express service.**
  The PR updated `apps/website/src/app/docs/content.ts` but the root README was not shown in the diff.
  Run: `grep -r "dapp/backend" README.md`

---

## JWT Roles / Admin RBAC Fix (`fix/issue-241`) — PR #270

### 🟠 Fix Soon After Merge

- [ ] **`GetRoles` passes `id.String()` instead of the raw `uuid.UUID` to pgx.**
  File: `apps/api/internal/repository/postgres/user_repository.go`
  pgx v5 handles `uuid.UUID` natively. Passing `.String()` forces string serialisation and an
  implicit server-side cast. The rest of the repo passes UUID values directly. Change to:
  ```go
  rows, err := r.db.QueryContext(ctx,
      `SELECT role FROM user_roles WHERE user_id = $1 ORDER BY role`,
      id,   // not id.String()
  )
  ```

- [ ] **`bootstrap-admin` does not call `db.Ping()` after `sql.Open()`.**
  File: `apps/api/cmd/bootstrap-admin/main.go`
  `sql.Open` only validates DSN format — it does not connect. A bad DSN or unreachable host surfaces
  as a confusing query error. Add:
  ```go
  if err := db.Ping(); err != nil {
      return fmt.Errorf("cannot reach database: %w", err)
  }
  ```

### 🟡 Polish

- [ ] **`bootstrap-admin` does not validate Stellar address format.**
  A typo (wrong prefix, wrong length) produces `no user found with wallet address` rather than
  `invalid Stellar address`. Low risk — the tool is operator-only — but a basic check
  (`strings.HasPrefix(*wallet, "G") && len(*wallet) == 56`) improves UX.

- [ ] **Document re-authentication requirement in migration runbook.**
  All currently active admin sessions have tokens with empty `Roles`. After deploying migration 009,
  admins must log out and back in. This is stated in the PR description but should be in the
  deployment runbook / `apps/api/migrations/README.md` (or equivalent) so it isn't missed during
  a future on-call deploy.

---

## Settlement Ownership / BOLA Fix (`fix/issue-239`) — PR #271

### 🟠 Fix Soon After Merge

- [ ] **`initiateSettlement` accepts `user_id` from the request body instead of the JWT.**
  File: `apps/api/internal/handler/settlement_handler.go` — `initiateSettlement` handler.
  Any authenticated caller can create a settlement on behalf of any other user by supplying
  a different `user_id` in the JSON body. The fix is the same pattern applied in this PR:
  extract caller from `auth.GetUserFromContext`, ignore the body `user_id` field entirely.
  This is a second BOLA vector on the creation endpoint — out of scope for #271 but directly
  referenced by issue #239's Step 3 ("audit every settlement endpoint").

- [ ] **`GET /api/v1/settlements/{id}` has no ownership check.**
  Any authenticated user can read any settlement by UUID. Read-only, so lower severity than
  a mutation, but it enables UUID enumeration which is the precondition for the BOLA attack
  fixed in this PR. Consider returning 404 (not 403) for settlements the caller doesn't own
  to avoid confirming resource existence.

### 🟡 Polish

- [ ] **403 on non-owner PATCH confirms settlement existence.**
  File: `apps/api/internal/service/settlement_service.go`
  Non-existent UUIDs return 404; non-owned UUIDs return 403. An attacker can distinguish
  between the two, using the 403 to confirm a UUID is a live settlement before targeting it.
  Return 404 for both cases (treat non-ownership as non-existence) to eliminate this signal.

---

## Performance Fee Fix (`fix/issue-236`) — PR #275

### 🟠 Fix Soon After Merge

- [ ] **Add impairment regression test to vault contract.**
  File: `packages/contracts/contracts/vault/src/test.rs`
  The issue explicitly required: "User deposits at rate 1.0, rate halves (impairment) → zero performance fee."
  The code handles it correctly (`yield_part < 0` → fee skipped) but the test proving it is missing.
  Add a test that simulates a loss scenario and asserts no performance fee is charged.

---

## Slippage Protection (`fix/issue-248`) — PR #277

### 🟠 Fix Soon After Merge

- [ ] **`preview_withdraw` returns gross pre-fee amount — document or fix for DApp.**
  File: `packages/contracts/contracts/vault/src/lib.rs`
  `preview_withdraw(shares)` returns `amount_for_shares(shares)` — the gross amount before management,
  early-withdrawal, and performance fees are deducted. EIP-4626's `previewRedeem` is supposed to
  include fees. A frontend that passes `preview_withdraw` output directly as `min_assets_out` will
  get `SlippageExceeded` on every fee-bearing withdrawal.
  Fix options: (a) add a `preview_withdraw_net` function that applies fee estimates on-chain, or
  (b) document explicitly in the function's doc comment that the return value is gross pre-fee and
  the DApp must subtract estimated fees before using it as `min_assets_out`.

### 🟡 Polish

- [ ] **Verify `cargo test -p vault-contract` passes in CI with vault_token.wasm artifact.**
  The contributor left this box unchecked in the PR test plan. Confirm integration tests
  are not silently passing vacuously due to missing WASM artifact.

---

## Event Indexer (`fix/issue-264`) — PR #276

> PR was **BLOCKED** — do not merge. Required fixes listed here for when the contributor resubmits.

### 🔴 Blocking (must be fixed in next submission)

- [ ] **Persist last indexed ledger to database — current in-memory cursor corrupts data on restart.**
  File: `apps/api/cmd/api/main.go` — `startEventIndexer` / `startLedger` variable.
  `startLedger` is a local `uint64` initialized to 0 on every API start. All balance update queries
  are additive (`total_deposited + amount`). Any restart replays all historical events and doubles
  every vault balance. Fix: add a `system_state` table (or key-value row) to persist the last
  successfully indexed ledger sequence; read it on startup and resume from there.

- [ ] **`startLedger = 0` triggers RPC error on first call — indexer never runs.**
  Stellar `getEvents` rejects ledger sequence 0. On first boot with no persisted cursor, start from
  the current ledger tip (not 0) to avoid replaying full chain history.

- [ ] **Make all balance updates idempotent.**
  Either (a) store processed event IDs in a `processed_events` table and skip duplicates, or
  (b) use absolute SET values derived from on-chain state rather than additive `+= amount`.
  Option (b) is safer and simpler if the on-chain state can be queried directly.

### 🟠 Fix Soon After Merge (architecture)

- [ ] **Move indexer logic into `internal/stellar/` — implement `EventPoller.PollEvents` properly.**
  The existing `EventPoller` in `internal/stellar/events.go` has `PollEvents` returning an empty
  stub. The PR added 247 lines of parallel logic in `main.go` instead of fixing the existing struct.
  Consolidate: implement `PollEvents` in the existing package so the logic is testable and reuses
  the repository layer instead of raw `*sql.DB`.

- [ ] **Remove `float64` case in `extractEventAmount`.**
  File: `apps/api/cmd/api/main.go`
  `float64` loses precision on large integer amounts (>2^53). Soroban event amounts come as strings.
  Treat any non-string amount type as unparseable and return `false`.

- [ ] **Add tests for `applyIndexedEvent` and `extractEventAmount`.**
  These functions write to the financial database based on external RPC input. They must have
  unit tests covering: deposit event, withdraw event, pause/unpause events, unknown event (no-op),
  missing amount field, malformed amount string.

---

## General / Cross-Cutting

- [ ] **Migration numbering collision (pre-existing debt).**
  Two files share the `007` prefix:
  - `007_add_vault_deleted_at.up.sql`
  - `007_update_users_table.up.sql`
  This will confuse any migration runner that applies files in lexicographic order. Rename one of
  them and renumber consistently. Needs care — check if the runner is order-sensitive before touching.

- [x] **No migration runner is wired into the Go API startup.**
  Resolved: `golang-migrate` runs on API startup when `RUN_MIGRATIONS=true` (set in
  `docker-compose.yml` for local dev). Pending migrations apply incrementally on `make dev`
  without requiring `make dev-reset`. See `apps/api/migrations/README.md`.

---

## Tracking

| Wave | PRs Reviewed | Items Added | Items Closed |
|---|---|---|---|
| OSS Wave 1 | #268, #269, #270, #271 | 17 | 0 |
| OSS Wave 2 | #275, #276, #277 | 11 | 0 |
