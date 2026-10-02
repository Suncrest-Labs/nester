# Mainnet Launch Rollback Runbook

Operational runbook for a fast rollback or pause in the **first 24 hours after
mainnet launch**. This window is treated differently from a routine rollback
because user balances are real, the team is on high alert, and speed matters
more than perfection.

Related documents:
- `docs/DEPLOYMENT.md` — general rollback and build procedure
- `docs/database-backup-restore.md` — PITR and snapshot restore
- `docs/SUPPORT_TRIAGE_GUIDE.md` — user communication templates

---

## 1. Trigger criteria

Any of the following is sufficient to declare a **Critical Launch Incident**
and start this runbook without further deliberation:

| Severity | Example | Action |
|---|---|---|
| Money movement error | Deposit credited wrong amount; withdrawal produces wrong contract call | Full rollback (§4) |
| Contract reachability | >50% of deposit or withdrawal attempts fail with a chain error | Full rollback (§4) |
| Data integrity | Balance in the API differs from on-chain vault balance | Full rollback (§4) |
| Security signal | Unexpected admin transaction on the operator account; anomalous contract invocation | Full rollback (§4) + security escalation |
| Partial degradation | Elevated error rate (<50%) with no money movement error confirmed | Pause (§3) while investigating |

**Do not wait for a root cause before pausing**. Pause first, investigate with
the system stable.

---

## 2. Authority to trigger

| Role | May trigger pause (§3) | May trigger full rollback (§4) |
|---|---|---|
| On-call engineer | Yes | Yes, unilaterally, if they judge an unrecoverable situation |
| Engineering lead | Yes | Yes |
| CEO / CPO | Yes (by notifying on-call) | Yes (by notifying on-call) |
| Any team member | Escalate immediately to on-call | No — escalate to on-call |

**No approval gate blocks a pause**. Any team member who spots a P0 signal
should page the on-call immediately and can request a pause; the on-call
engineer executes it. Pausing while investigating is always the right call —
an unwarranted pause costs minutes of downtime; an unwarranted non-pause can
lose user funds.

---

## 3. Pause (fast path, <5 minutes)

Use when you need to stop money movement immediately but do not yet know
whether a full rollback is required.

### 3.1 Engage the global money-path pause

The global pause switch stops all deposits and withdrawals across every vault
without a redeploy:

```bash
# Pause deposits only
curl -X POST "$API_URL/api/v1/admin/money-path/pause" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"deposits_paused": true, "withdrawals_paused": false}'

# Pause everything
curl -X POST "$API_URL/api/v1/admin/money-path/pause" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"deposits_paused": true, "withdrawals_paused": true}'
```

The API returns `503` with `reason: "money_path_paused"` to affected callers.
In-flight requests that already reached the chain are not reversed — the pause
prevents new submissions only.

**Confirm** the pause is in effect:

```bash
curl "$API_URL/api/v1/admin/money-path/status" \
  -H "Authorization: Bearer $ADMIN_TOKEN"
# Expect: {"deposits_paused": true, "withdrawals_paused": true, ...}
```

### 3.2 Notify users

Post the prepared user-facing message (§6.1) to the status page and relevant
channels **within 2 minutes** of engaging the pause. Users attempting deposits
or withdrawals will get a 503; they need to know this is intentional.

### 3.3 Preserve state before any changes

Before restarting any service or changing any configuration, capture the
current state for the post-mortem:

```bash
# Capture recent error logs
docker logs nester-api --since 1h > /tmp/incident-api-$(date +%s).log 2>&1

# Capture current health endpoint state
curl "$API_URL/health/detailed" > /tmp/incident-health-$(date +%s).json

# Capture Prometheus metrics snapshot
curl "$METRICS_URL/metrics" > /tmp/incident-metrics-$(date +%s).txt
```

Store these files off the instance (upload to the incident S3 bucket or
attach to the incident ticket) before proceeding.

---

## 4. Full rollback

Execute only after a pause (§3) is already in place. Do not skip §3.

### Step 1 — Take a database snapshot

```bash
# Take a logical snapshot of the current state BEFORE the rollback
DATABASE_DSN="$PRODUCTION_DSN" BACKUP_DIR=/tmp/incident-pre-rollback \
  scripts/db-backup.sh
```

Upload the resulting dump to the incident S3 bucket immediately. This
snapshot is the forensic record; it must exist before any database change.

**If a bad migration was deployed**, also capture:

```bash
# Record current migration version
psql "$PRODUCTION_DSN" -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 5;"
```

### Step 2 — Identify the rollback target

```bash
# What is currently deployed?
curl "$API_URL/health/detailed" | jq '{version, commit}'
```

Compare against the release list to find the last known-good image tag and
its commit hash. The rollback target is the most recent tag that:
- Passed staging smoke tests without issues, AND
- Was not the one promoted to mainnet in this launch

### Step 3 — Redeploy the previous image

```bash
# Update the image tag to the rollback target and redeploy
docker compose -f docker-compose.yml up -d --no-deps \
  --pull never api
# or via the deployment pipeline:
#   make deploy VERSION=<previous-tag>
```

**Do not run migrations during a rollback** unless the rollback target
requires them (i.e. the bad deploy added a migration that must be
reversed). If a migration must be reversed, consult `docs/database-backup-restore.md`
§3 — a migration that declares itself irreversible requires restoring from
the snapshot taken in Step 1 rather than running its `.down.sql`.

### Step 4 — Confirm rollback succeeded

```bash
curl "$API_URL/health/detailed" | jq '{version, commit, status}'
# Expect: version and commit match the rollback target; status is "ok"
```

All four dependency checks (database, Redis, Horizon, Soroban RPC) must
report healthy before proceeding.

### Step 5 — Run the smoke test suite

```bash
# Against production with the pause still engaged (no real money movement)
cd apps/dapp && npm run test:smoke:readonly
```

A smoke test in read-only mode verifies the API responds correctly to reads
without submitting chain transactions. Only run the full round-trip smoke
tests (deposits and withdrawals) after manually verifying the issue is
resolved and deliberately choosing to do so.

### Step 6 — Communicate status

Post the rollback-complete message (§6.2) to the status page and channels.
Confirm whether the issue is resolved or whether the service will remain
paused while root-cause analysis continues.

---

## 5. Lifting the pause

**Do not lift the pause until you have a working hypothesis for what caused
the incident, even if you cannot yet confirm it.**

Minimum requirements before resuming deposits and withdrawals:

1. Rollback target is deployed and `/health/detailed` is fully healthy.
2. At least one manual test deposit and withdrawal succeed on a test account
   via a direct API call (not through the dApp UI).
3. The on-call engineer and engineering lead both agree the risk is
   acceptable.

```bash
curl -X POST "$API_URL/api/v1/admin/money-path/pause" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"deposits_paused": false, "withdrawals_paused": false}'
```

Monitor the error rate, SLI metrics, and chain events for at least 15
minutes after lifting the pause before declaring the incident closed.

---

## 6. User communication templates

### 6.1 Pause announcement

> **Service interruption — deposits and withdrawals temporarily paused.**
>
> We have paused deposits and withdrawals while we investigate an issue that
> was detected shortly after our mainnet launch. No user funds are at risk.
> Your balances are safe and the underlying protocol is operating normally.
>
> We are working to resolve this and will provide an update within 30 minutes.
> We apologise for the disruption.

### 6.2 Rollback / resolution

> **Update — service restored / deposits and withdrawals re-enabled.**
>
> We have resolved the issue that caused us to pause the service earlier.
> [brief description of what happened, if safe to share]
>
> Deposits and withdrawals are now available again. We are continuing to
> monitor the service closely.
>
> A full post-mortem will be published within 48 hours.

---

## 7. Post-mortem requirements

A written post-mortem is mandatory for any incident that triggered this
runbook. It must be opened within 24 hours and published within 5 business
days. Minimum contents:

- Timeline (UTC, to the minute) from first signal to full resolution.
- The evidence that triggered the decision to pause/rollback.
- Root cause (or best hypothesis, if not yet confirmed).
- What state was preserved and where it is stored.
- Impact assessment: which users were affected, what they experienced,
  whether any transaction needs manual reconciliation.
- Corrective actions with owners and due dates.

Store the post-mortem in `docs/incidents/` using the filename
`YYYY-MM-DD-<slug>.md`.
