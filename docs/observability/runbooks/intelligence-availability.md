# Runbook — Intelligence availability

**Alerts:** `IntelligenceAvailabilityFastBurn`, `IntelligenceAvailabilitySlowBurn` (both ticket)
**Dashboard:** [SLO — Intelligence](/d/nester-slo-intel/intelligence)
**SLO:** 99% availability, 28-day window

---

## What the alert means

Intelligence requests are erroring at a rate that exhausts the 28-day budget in
~50 hours (fast, 13.44%) or ~5 days (slow, 5.6%).

**What is counted:** `outcome="error"` only — upstream failures, timeouts,
unhandled exceptions.

**What is not counted, and this is the common confusion:**

- **Refusals are success.** A guardrail declining an off-topic question or
  grounding declining for missing data is the system working as designed. If
  refusals are the actual concern, this is the wrong runbook — see
  [intelligence-refusal](intelligence-refusal.md).
- **Client disconnects** (`cancelled`) are excluded from the denominator
  entirely. The service was never given the chance to succeed or fail.

**Why this tickets rather than pages:** the feature is advisory. A user who
cannot get coaching still has full access to their money, and there is no 3am
action that fixes a third-party model provider.

---

## First three actions

**1. Separate a provider outage from our own fault.** This is the decisive
question:

```promql
sum by (status_class) (rate(nester_outbound_requests_total{upstream="anthropic_relay"}[5m]))
sum by (kind) (rate(nester_outbound_errors_total{upstream="anthropic_relay"}[5m]))
```

Rising 5xx or transport errors to `anthropic_relay` → provider. Clean upstream
with errors still occurring → ours.

**2. Confirm errors, not refusals, are driving it.**

```promql
sum by (outcome) (rate(nester_intelligence_requests_total[5m]))
```

If `refused` is what moved and `error` is flat, this alert should not be
firing — treat that as an alerting bug and note it for the review.

**3. Check whether a deploy correlates.**

```bash
gh run list --repo Suncrest-Labs/nester --workflow ci.yml --limit 10
git log --oneline -15 origin/main -- apps/intelligence
```

---

## Dashboards

| Panel | Question it answers |
|---|---|
| **Error ratio** | How far over threshold |
| **Requests by outcome** | Errors vs refusals vs cancellations |
| **Model provider health** | Is the provider the cause |
| **Time to first token p95** | Is latency degrading alongside |

---

## Logs

```bash
kubectl logs -l app=nester-intelligence --since=30m | grep -iE "error|exception|traceback"

# The Go side's view of calling this service
kubectl logs -l app=nester-api --since=30m | grep -i intelligence | grep -i error
```

Locally: `docker compose logs intelligence --since 30m`.

Every log line carries `RequestId`, which matches the `X-Request-Id` header and
the trace, so a user-reported failure can be followed end to end.

---

## Traces

In [Jaeger](http://localhost:16686), service `nester-intelligence`, filter
`error=true`. The model-call span (from #1054) carries
`nester.llm.time_to_first_token_ms` and `nester.llm.duration_ms`, which
distinguishes "the provider never responded" from "the provider responded and
we failed to process it".

Cross-service traces link the Go API's span to this service's, so a failing
chat request can be followed from the front door.

---

## Three most likely causes

### Cause A — Model provider outage or rate limiting

**Distinguishing evidence:**

```promql
sum by (status_class) (rate(nester_outbound_requests_total{upstream="anthropic_relay"}[5m]))
```

`5xx` → provider outage. `4xx` → likely 429 rate limiting or an auth problem.
`kind="timeout"` → provider slow rather than down.

Check the provider's status page. This affects everyone using it, so
confirmation is usually quick.

**Mitigation:** for an outage, wait and monitor — there is no action available
to us. For rate limiting, check whether request volume spiked (a retry loop or
a traffic surge) and whether the cost governor is doing its job. For auth,
check the API key has not expired or been rotated.

### Cause B — Our own fault after a deploy

**Distinguishing evidence:** upstream clean, errors step up at a deploy
boundary, tracebacks in the logs. A Python traceback is conclusive — the
provider cannot cause one.

**Mitigation:** roll back.

### Cause C — Redis or conversation store failure

**Distinguishing evidence:**

```promql
sum(rate(nester_redis_errors_total[5m]))
```

The service stores conversation history in Redis. A Redis failure produces
errors that look like model failures until you check.

**Mitigation:** restore Redis. Conversation history is not durable state — the
feature degrades to stateless answers rather than failing outright once Redis
is healthy again.

---

## Immediate mitigation

1. **Roll back** if a deploy correlates.
2. **Restore Redis** if that is the cause.
3. **Wait and monitor** for a provider outage. Genuinely the correct action —
   escalating does not restore a third party's service.
4. **Disable the feature** in the frontend if it is failing loudly enough to
   damage user trust. The money paths are unaffected, so this is a clean
   degradation.

---

## Escalation

Escalate when:

- Error ratio above 50% for more than 30 minutes with the upstream healthy
  (points at our fault, not theirs).
- Errors accompanied by API availability failures — a shared cause, and the API
  matters more.
- A provider outage lasting more than two hours, so a decision can be made
  about disabling the feature and communicating.

Escalate to the repository maintainers (CODEOWNERS). This is business-hours
work unless it is a symptom of something larger.

---

## Recovery verification

1. `intelligence:availability:error_ratio_rate5m` below threshold for 15
   minutes.
2. Requests are actually occurring — a zero ratio with zero traffic is not
   recovery.
3. Intelligence probe passing:
   ```promql
   nester_probe_success{probe="intelligence"}
   ```
4. TTFT back to normal (`intelligence:ttft:p95_5m`) — a provider recovering
   often leaves latency elevated.

---

## Follow-up

**Postmortem required** when the cause was ours (not the provider) and the
incident lasted more than an hour, or when more than 25% of the 28-day budget
was consumed.

A provider outage does not need a full postmortem, but **is** worth recording:
repeated outages are an argument for a fallback path, and that argument needs
evidence.

If the budget is in warning or exhausted, apply
[the error-budget policy](../error-budget-policy.md).

Note whether this SLO's failures correlated with user complaints. If users did
not notice, the 99% target may be stricter than the feature's importance
warrants — a question for the next [SLO review](../slo-review.md).
