# Runbook — Synthetic probes

**Alerts:** `SyntheticProbeFailing`, `SyntheticProbeStale`, `SyntheticProbeMetricsAbsent`, `SyntheticProbeSlow` (all ticket)
**Dashboard:** [SLO — Synthetic probes](/d/nester-slo-probes/synthetic-probes)
**Runs:** every 15 minutes against staging via
[`synthetic-probes.yml`](../../../.github/workflows/synthetic-probes.yml)

---

## What the alert means

A probe exercising a user flow on **staging** has failed, stopped running, or
become slow.

**These run against staging, not production.** A failure means a code path is
broken *before* it reaches users — which is valuable early warning, not an
active outage. That is why every probe alert is ticket severity: a staging page
at 3am trains the on-call to ignore the pager.

The probes exist to cover what real-traffic SLIs structurally cannot. Burn-rate
alerts are ratios over observed traffic, so an idle service produces no bad
events and no alert. The probes catch the case where a withdrawal path breaks
on Friday and the first person to find out is a user on Monday.

| Alert | Meaning |
|---|---|
| `SyntheticProbeFailing` | A probe failed twice consecutively. The path is broken. |
| `SyntheticProbeStale` | No result for 30 minutes. **The runner stopped — success gauges are frozen and must not be trusted.** |
| `SyntheticProbeMetricsAbsent` | No probe series at all. Synthetic coverage is zero. |
| `SyntheticProbeSlow` | A probe passes but takes over 60s. |

---

## First three actions

**1. Determine whether the probe or the service is at fault.**

```promql
nester_probe_success
nester_probe_last_reason
time() - nester_probe_last_run_timestamp_seconds
```

- `success == 0` with recent timestamps → the service under test is broken.
- Timestamps climbing → the **runner** stopped; the service may be fine.
- No series → nothing has ever reported, or the push path is broken.

**2. Read the failure reason.** It is a closed enum and points directly at the
class of problem:

| Reason | Meaning |
|---|---|
| `http_5xx` | The staging service returned a server error |
| `http_4xx` | Auth, a bad request, or a changed contract |
| `timeout` | The service did not respond in time |
| `connection` | Staging unreachable — DNS, network, or down |
| `assertion` | Responded, but the payload was wrong shape |
| `bad_payload` | Response was not valid JSON |
| `unknown` | Unclassified — read the workflow log |

**3. Open the workflow run.**

```bash
gh run list --repo Suncrest-Labs/nester --workflow synthetic-probes.yml --limit 10
gh run view <run-id> --log
```

The log carries the detail the metric labels deliberately omit — an exception
string can contain a vault ID, an amount, or an upstream URL, so it goes to the
log and only the enum becomes a label.

---

## Dashboards

| Panel | Question it answers |
|---|---|
| **Probes passing** | How many are healthy right now |
| **Oldest probe result** | Is the runner alive |
| **Probe outcome** | Which probe, and since when |
| **Failure reasons** | What class of failure |
| **Result age** | Sawtooth (healthy) vs climbing (runner stopped) |

---

## Logs

The probes run in GitHub Actions, so the workflow log is the primary source:

```bash
gh run view <run-id> --log | grep -A5 "probe:"
```

Then the staging service logs for the same window:

```bash
kubectl logs -l app=nester-api --context=staging --since=30m | grep -i error
kubectl logs -l app=nester-intelligence --context=staging --since=30m | grep -i error
```

---

## Traces

Probe requests carry no special marker, but they hit staging at predictable
15-minute intervals from GitHub Actions IP ranges. In Jaeger against staging,
filter the relevant route around the failure timestamp from
`nester_probe_last_run_timestamp_seconds`.

---

## Three most likely causes

### Cause A — Staging service genuinely broken

**Distinguishing evidence:** `success == 0`, recent timestamps, reason
`http_5xx` or `timeout`. Staging logs show errors.

This is the probes doing their job: a real break found before it reached
production.

**Mitigation:** treat it as a normal bug. Identify the change, fix or roll back
on staging. Use the corresponding production runbook —
[flow-success](flow-success.md), [api-availability](api-availability.md) — for
the diagnostic approach; the causes are the same.

**Check whether the same change is already in production.** If it is, this
alert has just told you about a production problem that the SLO alerts have not
caught yet — escalate accordingly.

### Cause B — Probe misconfiguration or credential expiry

**Distinguishing evidence:** reason `http_4xx` (especially 401/403) or
`assertion`, while staging looks healthy.

Common sub-causes:

- `STAGING_PROBE_AUTH_TOKEN` expired or rotated.
- `STAGING_PROBE_VAULT_ID` points at a deleted vault.
- The API contract changed and the probe's shape assertion is now wrong.
- The probe vault has insufficient balance for the withdrawal probe.

**Mitigation:** refresh the secret or update the probe. If the API contract
changed legitimately, update the assertion in
[`probe.py`](../../../tests/probes/probe.py) and its test — the probe is
supposed to fail when the contract changes silently, so this is the mechanism
working.

**Do not disable the probe** to clear the alert. A disabled probe is
indistinguishable from a passing one and the coverage is silently lost.

### Cause C — Runner stopped

**Distinguishing evidence:** `SyntheticProbeStale` firing, result age climbing.
Success gauges are frozen at their last values — often all showing `1`, which
is the trap this alert exists for.

Sub-causes:

- The workflow is disabled, or scheduled workflows were auto-disabled after
  repository inactivity (GitHub does this after 60 days).
- `STAGING_PROBE_API_BASE_URL` is unset, so the workflow skips cleanly by
  design.
- The Pushgateway is unreachable, so results are produced but never delivered.

**Mitigation:**

```bash
gh workflow list --repo Suncrest-Labs/nester
gh workflow enable synthetic-probes.yml --repo Suncrest-Labs/nester
gh workflow run synthetic-probes.yml --repo Suncrest-Labs/nester
```

If secrets are unset, that is the expected state on a fresh deployment — see
[Not yet configured](#not-yet-configured).

---

## Not yet configured

`SyntheticProbeMetricsAbsent` firing with no probe run in history usually means
the probes have never been configured, which is their state on merge.

Required for the read-only probes:

| Secret | Purpose |
|---|---|
| `STAGING_PROBE_API_BASE_URL` | Staging API base URL. Unset means the workflow skips. |
| `STAGING_PROBE_INTELLIGENCE_BASE_URL` | Staging intelligence base URL |
| `STAGING_PROBE_AUTH_TOKEN` | Probe account credentials |
| `STAGING_PROBE_VAULT_ID` | The probe account's staging vault |
| `STAGING_PUSHGATEWAY_URL` | Where results are pushed |

Additionally, for deposit and withdrawal probes, set the repository variable
`STAGING_PROBE_ALLOW_MUTATIONS=true` for the staging environment.

**That variable is a deliberate act.** The probes create real transactions.
They refuse to run against anything whose environment name or URL looks like
production, but the mutation opt-in is the guard that means merging this
workflow cannot start moving funds by itself.

Until configured, probe coverage is zero and the absent alert is correctly
reporting that.

---

## Immediate mitigation

1. **Fix the underlying service** if the failure is real (Cause A).
2. **Refresh credentials or update the probe** if it is misconfiguration
   (Cause B).
3. **Re-enable and re-run the workflow** if the runner stopped (Cause C).
4. **Configure the secrets** if the probes have never run.

---

## Escalation

Escalate when:

- A probe failure reveals a break that is **also in production** — that is a
  production incident, not a staging one.
- Probes have been stale for more than 24 hours: coverage has been silently
  zero for a day.
- The deposit or withdrawal probe fails repeatedly, since those exercise the
  money paths and a staging break there predicts a production break.

Escalate to the repository maintainers (CODEOWNERS).

---

## Recovery verification

1. All probes reporting success:
   ```promql
   nester_probe_success
   ```
2. Result age sawtoothing, not climbing:
   ```promql
   time() - nester_probe_last_run_timestamp_seconds
   ```
   This proves the runner is alive rather than frozen on a healthy-looking
   value.
3. Durations back to normal (`nester_probe_duration_seconds`).
4. At least two consecutive successful runs — one is not evidence of recovery
   for something that alerts on two consecutive failures.

---

## Follow-up

**Postmortem required** when a probe caught a break that reached production
undetected by the SLO alerts. That is a gap in the real-traffic monitoring, and
the probes should not be the only thing standing between a break and users.

No postmortem needed for probe misconfiguration, but **fix the probe so the
same false alarm cannot recur**. A probe that cries wolf gets ignored, and then
it is not covering anything.

Bring to the next [SLO review](../slo-review.md):

- Did the probes catch anything real traffic missed? That is the measure of
  whether they are earning their cost.
- Is a flow now worth probing that is not covered?
- Were there false alarms, and what caused them?
