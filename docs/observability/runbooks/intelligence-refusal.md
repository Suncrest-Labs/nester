# Runbook — Intelligence refusal rate

**Alert:** `IntelligenceRefusalRateHigh` (ticket)
**Dashboard:** [SLO — Intelligence](/d/nester-slo-intel/intelligence)
**Threshold:** >30% of answerable requests refused, sustained over 6h

---

## What the alert means

The intelligence service is declining to answer a large share of requests it
was asked to answer.

**A refusal is not an error.** The service returned 200 and a polite sentence.
Every transport-level metric records it as a success, which is correct — and is
exactly why this alert exists, because a refusal wave is invisible to
availability monitoring while being extremely visible to users.

**This has no error budget and never pages.** Some baseline of refusals is
correct and healthy: users do ask off-topic questions, and grounding *should*
decline when account data is missing. There is no principled budget of correct
behaviour to burn. What this alert catches is a *departure from baseline*,
which almost always means a guardrail is misfiring or grounding has lost its
data source.

**Denominator:** `answered` + `refused`. Errors are excluded — a request that
failed was never given the chance to be refused, and including errors would
make this number move whenever availability moved.

**A caution on the threshold:** 30% is an estimate, not a measurement. It was
set without observed baseline data. If this alert fires and investigation finds
nothing wrong, that is evidence the threshold is too tight — record it for the
[SLO review](../slo-review.md) rather than assuming a problem exists.

**User impact:** users ask reasonable questions and are told the service cannot
help. They conclude the feature is broken and stop using it, which does not
recover when the underlying issue is fixed.

---

## First three actions

**1. Split by mechanism.** This single query determines the entire
investigation:

```promql
sum by (reason) (rate(nester_intelligence_refusals_total[30m]))
```

- `guardrail` up, `grounding` flat → the guardrail layer. Go to
  [Cause A](#cause-a--guardrail-regression).
- `grounding` up, `guardrail` flat → the data path. Go to
  [Cause B](#cause-b--grounding-data-unavailable).
- Both up → likely a deploy touching both. Go to
  [Cause C](#cause-c--deploy-or-prompt-change).

"Refusals are up" is not actionable. "Guardrail refusals are up and grounding
refusals are flat" points at one deploy.

**2. Check whether a deploy correlates.**

```bash
git log --oneline -20 origin/main -- apps/intelligence/app/services/guardrails.py apps/intelligence/app/services/grounding.py apps/intelligence/app/services/claude.py
```

Guardrail patterns, the grounding prompt, and the system prompt are the three
things whose change produces this symptom.

**3. Read actual refused requests.**

```bash
kubectl logs -l app=nester-intelligence --since=1h | grep -i "guardrail\|refus" | tail -50
```

The guardrail layer logs the matched category and the request ID, never the
prompt text. If the matched categories look wrong for ordinary questions, the
guardrail is the cause and you have the category.

---

## Dashboards

| Panel | Question it answers |
|---|---|
| **Refusal rate and reasons** | Rate, and which mechanism |
| **Requests by outcome** | Refusals vs answers vs errors |
| **Error ratio** | Is availability also affected |

---

## Logs

```bash
# Guardrail decisions with matched category
kubectl logs -l app=nester-intelligence --since=1h | grep -i guardrail

# Grounding validation failures
kubectl logs -l app=nester-intelligence --since=1h | grep -i "grounding\|not grounded"

# The Go API's view of the user-context call grounding depends on
kubectl logs -l app=nester-api --since=1h | grep -i "user context" | grep -i error
```

Locally: `docker compose logs intelligence --since 1h`.

Refusals are logged with the matched category and request ID, deliberately
without the prompt text — it is user financial data and does not belong in
logs any more than in metric labels.

---

## Traces

In [Jaeger](http://localhost:16686), service `nester-intelligence`, look at
spans for refused requests. The grounding retrieval span shows whether user
context was fetched successfully — an empty or failed retrieval is the usual
cause of a grounding refusal wave, and it is visible there before it is
obvious anywhere else.

---

## Three most likely causes

### Cause A — Guardrail regression

**Distinguishing evidence:** `reason="guardrail"` dominant. Logs show ordinary
questions matching guardrail categories.

A pattern change that is too broad refuses legitimate questions. A regex
intended to catch injection attempts can easily match normal financial
vocabulary.

**Mitigation:** roll back the guardrail change. Do not attempt to narrow the
pattern under incident pressure — a guardrail is security-relevant, and a
hastily narrowed pattern can open a real gap. Roll back, then fix with tests.

### Cause B — Grounding data unavailable

**Distinguishing evidence:** `reason="grounding"` dominant. The service is
answering "I don't have that information in your account data" because it
genuinely does not have it.

The grounding path fetches user context from the Go API. If that call fails or
returns empty, every answer becomes a refusal — and note that this is the
service behaving *correctly*: refusing rather than fabricating is the desired
behaviour. The fault is upstream.

Check:

```promql
sum by (status_class) (rate(nester_outbound_requests_total{upstream="intelligence"}[5m]))
nester_db_pool_acquired_connections / nester_db_pool_max_connections
```

Also check whether the API itself is degraded — see
[api-availability](api-availability.md).

**Mitigation:** fix the user-context path. The refusals stop on their own once
data is available again.

### Cause C — Deploy or prompt change

**Distinguishing evidence:** both reasons up; step change at a deploy boundary.
A system-prompt change can make the model refuse more readily without any
guardrail or grounding code changing.

**Mitigation:** roll back.

---

## Immediate mitigation

1. **Roll back** the guardrail, grounding, or prompt change if one correlates.
2. **Fix the upstream data path** if grounding refusals dominate — the refusals
   are a symptom, not the fault.
3. **Do nothing** if investigation finds ordinary traffic and correct
   behaviour. Record it as evidence the 30% threshold is too tight.

**Never** loosen guardrails to clear this alert without understanding why they
are firing. The guardrails exist to prevent prompt injection and off-topic
financial advice; weakening them to improve a metric trades a real safety
property for a graph.

---

## Escalation

Escalate when:

- Refusal rate above 60% — the feature is effectively non-functional.
- Guardrail refusals are high and rolling back is not straightforward. Guardrail
  changes are security-relevant and should not be modified ad hoc during an
  incident.
- Grounding refusals point at a broader API or database problem.

Escalate to the repository maintainers (CODEOWNERS). Business-hours work: no
funds are at risk.

---

## Recovery verification

1. `intelligence:refusal:error_ratio_rate30m` back to baseline (note what
   baseline actually is — this is the number the first SLO review must
   establish).
2. `sum by (reason) (rate(nester_intelligence_refusals_total[30m]))` — the
   mechanism that spiked is back to its usual share.
3. Intelligence probe passing:
   ```promql
   nester_probe_success{probe="intelligence"}
   ```
   Note the probe treats a refusal as success, so it will not catch a refusal
   wave. Use it only to confirm the service is responding.
4. Manually ask a question that should be answerable and confirm a real answer.

Step 4 is the one that matters. Metrics cannot tell you whether the answers are
good — only whether they exist.

---

## Follow-up

**Postmortem required** when a guardrail regression refused legitimate requests
for more than two hours. Users who conclude a feature is broken do not
re-evaluate it when it is fixed, so the cost outlasts the incident.

Add a test for the specific case that regressed. Guardrail changes are exactly
the class of change where a test is cheap and the failure mode is invisible
until users hit it.

This indicator has no error budget, so
[the error-budget policy](../error-budget-policy.md) does not apply
mechanically. A sustained breach is still grounds for prioritising the fix.

**Record the observed baseline refusal rate in the incident notes**, whatever
the outcome. Establishing that number is the first agenda item of the first
[SLO review](../slo-review.md), and incident investigations are one of the few
places it gets examined directly.
