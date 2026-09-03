# Runbook — Monitoring is down

**Alerts:** `SLOTargetDown` (page), `SLOMetricsAbsent` (page)
**SLO:** none — this is the monitoring watching itself

---

## What the alert means

Prometheus cannot scrape a target, or core metrics have disappeared entirely.

**Every SLO computed from that target is now blind.** Its recorded ratios go
stale and its burn-rate alerts cannot fire. This is the most dangerous state
the observability stack has, because the symptom is *silence* — every dashboard
green, no alerts firing, and no way to tell that apart from a healthy system.

**Treat a silent SLO as unknown, never as healthy.**

| Alert | Meaning |
|---|---|
| `SLOTargetDown` | `up == 0` — the scrape target is unreachable for 5 minutes. |
| `SLOMetricsAbsent` | `nester_http_requests_total` has no series at all. |

The first question is always the same: **is the service down, or is only the
monitoring down?** These require completely different responses and the alert
cannot distinguish them.

---

## First three actions

**1. Establish whether the service itself is up.** Do not use Prometheus for
this — it is the thing that may be broken.

```bash
kubectl get pods -l app=nester-api

# Hit the service directly
curl -sS -o /dev/null -w '%{http_code}\n' https://<api-host>/healthz
```

- Service down → this is a **service outage**. Use
  [api-availability](api-availability.md); the monitoring alert is a symptom.
- Service up → this is a **monitoring outage**. Continue here.

**2. Check whether users are affected.** Independently of metrics: support
channels, the frontend in a browser, a manual API call. A monitoring outage
with a healthy service is urgent but not user-facing; the reverse is the
opposite.

**3. Check Prometheus itself.**

```bash
kubectl get pods -l app=prometheus
curl -sS http://<prometheus-host>/-/healthy
```

Then the target status page (`/targets`) for the error against the failing
target — it usually names the cause directly: connection refused, timeout,
DNS failure, 401.

---

## Dashboards

Dashboards are unreliable during this alert, which is the point. Use them only
to confirm the extent of the gap:

- A flat line ending abruptly indicates when data stopped.
- Other targets still reporting means the failure is scoped to one service.

The Prometheus **`/targets`** page is the authoritative view here, not Grafana.

---

## Logs

```bash
# Prometheus scrape errors
kubectl logs -l app=prometheus --since=30m | grep -iE "scrape|error"

# Did the API's metrics listener start?
kubectl logs -l app=nester-api --since=30m | grep -i "metrics listener"
```

The API logs `starting internal metrics listener` at boot. Its absence means
the listener never started — check `METRICS_ENABLED` and `METRICS_ADDR`.

The listener failing is logged and deliberately **non-fatal**: losing
observability must not take down the API. That design choice is why the service
can be perfectly healthy while its metrics are gone.

---

## Traces

If tracing is still working while metrics are not, Jaeger confirms the service
is serving traffic — useful evidence that this is a monitoring-only failure.

If both are gone, suspect the process or the network path rather than the
metrics subsystem specifically.

---

## Three most likely causes

### Cause A — Service is genuinely down

**Distinguishing evidence:** pods not running, health endpoint unreachable,
users affected.

**Mitigation:** this is a service outage. Go to
[api-availability](api-availability.md). The monitoring alert did its job.

### Cause B — Metrics listener not running or unreachable

**Distinguishing evidence:** service healthy on its public port, but the
metrics port is not answering.

```bash
kubectl exec -it <api-pod> -- wget -qO- http://127.0.0.1:9090/healthz
```

Sub-causes:

- `METRICS_ENABLED=false` in this environment.
- `METRICS_ADDR` bound to loopback while Prometheus scrapes across the network.
  The default is `127.0.0.1:9090`, which is correct for a sidecar scraper and
  wrong for a remote one.
- A network policy or firewall blocking the metrics port.

**Mitigation:** correct the configuration and restart.

### Cause C — Prometheus itself is broken

**Distinguishing evidence:** all targets down at once, or Prometheus not
running.

Sub-causes: pod crashed or OOM-killed, storage full, configuration rejected on
reload.

```bash
kubectl logs -l app=prometheus --since=1h | grep -iE "err|fatal|no space"
```

**Mitigation:** restart Prometheus. If storage is full, extend the volume or
reduce retention. If a config reload failed, Prometheus keeps running the old
config — check the reload endpoint's response before assuming the new rules are
live.

---

## Immediate mitigation

1. **Restore the service** if it is down — that is the real incident.
2. **Fix the metrics configuration** and restart if the listener is the problem.
3. **Restart Prometheus** if it is the problem.
4. **Monitor manually in the meantime.** With metrics blind, use logs, direct
   health checks, and support channels. Say explicitly in the incident channel
   that SLO alerting is not currently protecting the service, so nobody reads
   the silence as safety.

**Never** silence these alerts. They are the last line: the mechanism that
stops absent telemetry from looking like health.

---

## Escalation

Escalate when:

- Monitoring has been down more than 30 minutes. Every minute is unprotected.
- Monitoring is down **and** users are reporting problems — the outage is now
  invisible to alerting and must be handled manually.
- Prometheus cannot be restored quickly.

Escalate to the repository maintainers (CODEOWNERS). Page-severity: the cost of
a blind window is every other SLO in this document.

---

## Recovery verification

1. All targets up:
   ```promql
   up{job=~"nester-.*"}
   ```
2. Core metrics present:
   ```promql
   sum(rate(nester_http_requests_total[5m]))
   ```
3. Recording rules evaluating — check the `/rules` page for evaluation errors
   and recent evaluation times.
4. **Confirm no SLO breached during the blind window.** Once data resumes, the
   28-day figures include whatever happened. Check budget-remaining on each
   dashboard against its value before the outage.

Step 4 matters most. A service can degrade badly during a monitoring outage,
and the alerts that would have fired never did.

---

## Follow-up

**Postmortem required** for any monitoring outage longer than 15 minutes.

Cover:

- How long the blind window was.
- Whether anything broke during it that alerting would have caught.
- Why the monitoring failed, and whether the same cause can recur.

**Record the blind window explicitly in the next [SLO review](../slo-review.md).**
The 28-day attainment figures for that period are computed from incomplete
data, so they understate consumption. A budget that looks healthy after a long
blind window is not trustworthy evidence, and treating it as such is exactly
the false confidence this whole system is meant to prevent.
