# Money-path launch readiness

Working notes for the testnet launch. Covers the reproducible dev
environment, the global pause switch, first-run onboarding, and the
pre-launch security review of the deposit/withdraw path.

Paths below are relative to the repo root. The API lives in `apps/api`, the
dapp in `apps/dapp/frontend`, contracts in `packages/contracts`.

Contents:

1. [Deterministic seed data](#1-deterministic-seed-data-1122)
2. [Global pause switch](#2-global-pause-switch-1120)
3. [First-run onboarding](#3-first-run-onboarding-1127)
4. [Pre-launch security review](#4-pre-launch-security-review-1118)

---

## 1. Deterministic seed data (#1122)

**Problem.** `scripts/seed.sql` references columns that later migrations
removed, so a fresh dev database does not come up. Every new contributor
loses their first day to it and bug reports are not reproducible.

**Target state.**

- `scripts/seed.sql` applies cleanly on top of a fully migrated database
  (`apps/api/migrations` through the latest numbered pair).
- It produces a *known* fixture set — a fixed list of users, vaults, and
  positions with stable UUIDs — so tests and manual repro can assume them.
- Re-runnable: guarded by `ON CONFLICT DO NOTHING` / `TRUNCATE ... RESTART
  IDENTITY` at the top, so applying it twice is a no-op rather than an error.
- No dependency on wall-clock time — timestamps are literals or offsets from
  a fixed anchor.

**How to regenerate after a schema change.**

```bash
# from apps/api
make migrate-up                       # apply every migration to a scratch DB
psql "$DATABASE_URL" -f ../../scripts/seed.sql   # must exit 0
```

**CI gate.** Add a job that, on a clean Postgres service container, runs
migrations then loads the seed and fails on any error. This keeps the seed
and the schema from drifting again:

```yaml
# .github/workflows — sketch
- run: make -C apps/api migrate-up
- run: psql "$DATABASE_URL" -f scripts/seed.sql
- run: psql "$DATABASE_URL" -c "select count(*) from vaults" | grep -q 3
```

**Acceptance criteria**

- [ ] Seed script matches the current schema and applies cleanly to a fresh DB.
- [ ] Produces a known set of users, vaults and positions.
- [ ] A CI job applies migrations plus the seed and fails on drift.

---

## 2. Global pause switch (#1120)

Today pausing is per-vault only (`AdminHandler.PauseVault` /
`UnpauseVault`, `POST /api/v1/admin/vaults/{id}/pause`). There is no way to
stop the whole money path in one action. Commenting out code and redeploying
is not an incident response.

**Design.**

- A single global switch with two independent flags: `deposits_paused` and
  `withdrawals_paused`. Either can be engaged without the other.
- Backed by a one-row table (e.g. `system_flags`) so it survives a process
  restart, plus an in-memory cache refreshed on a short interval and on an
  explicit admin write, so the check adds no per-request DB hit.
- Enforced in the service layer at the entry of every deposit and withdraw
  path (not just the HTTP handler) so background workers and retries respect
  it too. Rejected calls return a typed `ErrMoneyPathPaused` mapped to
  `503` with a machine-readable `reason`.
- Every engage/release writes an entry to `audit_logs` (migration `011`)
  with the actor, the flag, the previous and new value, and a free-text
  reason.
- New admin endpoints alongside the existing per-vault ones:
  `POST /api/v1/admin/pause` and `POST /api/v1/admin/unpause` with body
  `{ "deposits": true, "withdrawals": true, "reason": "..." }`.

**Frontend.** When either flag is on, deposit/withdraw actions in
`apps/dapp/frontend` are disabled with an honest banner ("Deposits are
temporarily paused for maintenance") rather than a generic error.

**Acceptance criteria**

- [ ] A switch that halts deposits and withdrawals independently.
- [ ] Effective within seconds and surviving a restart.
- [ ] Engaging or releasing it is audit-logged.
- [ ] The UI explains the pause honestly rather than showing a broken flow.

---

## 3. First-run onboarding (#1127)

A testnet launch brings users with no wallet, no testnet XLM, and no idea
what to do. The drop-off there wastes the launch.

**Guided path** (a stepper in `apps/dapp/frontend`, one step visible at a
time):

1. **Install a wallet** — detect an injected Stellar wallet
   (`window.freighter` / provider API). Advance automatically once present.
2. **Switch to testnet** — read the wallet network; if it is not testnet,
   show the switch instruction and poll until it reports testnet.
3. **Fund via friendbot** — call
   `https://friendbot.stellar.org/?addr=<pubkey>`, then poll Horizon for a
   non-zero XLM balance before advancing.
4. **First deposit** — deep-link into the existing deposit flow, pre-filled;
   completion is detected from the deposit confirmation the app already
   receives.

**Resumability.** Persist `{ step, startedAt }` to `localStorage` under a
versioned key. On load, re-run each completed step's detection (wallet
present? testnet? funded? has a vault position?) and jump to the first
unsatisfied step, so a reload mid-flow does not restart onboarding.

**Testing.** An end-to-end test from a clean profile: no wallet → funded
vault, asserting each step auto-advances and that a reload at each step
resumes in place.

**Acceptance criteria**

- [ ] Guided path: install wallet, switch to testnet, fund via friendbot, first deposit.
- [ ] Each step detects completion and advances automatically.
- [ ] A user can resume mid-flow after a reload.
- [ ] Tested end to end from a clean profile.

---

## 4. Pre-launch security review (#1118)

Before real value moves on testnet with real users, someone tries to break
the deposit/withdraw path deliberately rather than trusting that the tests
cover it. This section is the living record of that review; findings are
tracked as linked issues.

### Threat model — the money path

| Asset | What an attacker targets | Cost if compromised |
| --- | --- | --- |
| User funds in a vault | Withdraw to an address they control; replay a withdraw | Direct loss of user funds |
| Deposit accounting | Credit a deposit that never settled on-chain | Protocol insolvency, socialized loss |
| Admin surface | Reach `/api/v1/admin/*` without authorization | Pause/unpause, vault takeover, audit-log gaps |
| Auth tokens / sessions | Forge or replay a session; privilege escalation to admin | Act as any user or operator |
| On-chain indexer | Feed a reorged or spoofed event to mark funds moved | Desync between ledger truth and app state |
| Idempotency / retries | Double-spend via retried deposit or withdraw requests | Loss proportional to retry window |

### Review checklist

- [ ] **Deposit** — settlement is confirmed on-chain (`confirmed_at`,
  migration `009`) before credit; reorg-safe indexer (`migration 100`)
  handles rollbacks; amount and asset validated against the on-chain event.
- [ ] **Withdraw** — destination is the authenticated user's; per-request
  idempotency key; balance check and debit are one transaction; no
  negative-amount or overflow path.
- [ ] **Auth** — session tokens expire and cannot be replayed after
  logout; no user-controlled field selects the acting user id.
- [ ] **Admin** — every `/api/v1/admin/*` route requires the admin role;
  pause/unpause and vault actions are audit-logged with the actor.
- [ ] **Rate limiting** on deposit, withdraw, and auth endpoints.
- [ ] **Secrets** — no signing keys or DB credentials in the repo, images,
  or logs (`scripts/contract-audit.sh`, `SECURITY.md`).

### Process

- Findings are filed as issues labeled `security`, severity-rated
  (`severity:high` / etc.), and linked back here.
- High/critical findings block the launch tag.
- This document and `SECURITY.md` are updated when a finding is resolved.

**Acceptance criteria**

- [ ] A written threat model of the money path.
- [ ] Adversarial review of deposit, withdraw, auth and admin flows.
- [ ] Findings triaged, severity-rated, and tracked to closure.
