# Runbook — Intelligence time to first token

**Alerts:** `IntelligenceTTFTFastBurn`, `IntelligenceTTFTSlowBurn` (both ticket)
**Dashboard:** [SLO — Intelligence](/d/nester-slo-intel/intelligence)
**SLO:** 95% of streams produce a first token within 3s, 28-day window

---

## What the alert means

Streamed answers are taking longer than 3s to produce their first token more
often than the budget permits.

**Time to first token, not total duration.** Perceived responsiveness of a
stream is dominated by when text starts appearing: a 40s answer that begins in
800ms feels fine, an 8s answer that begins at 7s feels broken. Total completion
time is recorded but carries no SLO, because it varies legitimately with answer
length and tool rounds — a target on it would be a target on how much users ask
for.

**Note the threshold size.** The fast-burn threshold is 67.2% of streams
exceeding 3s. That is not subtle degradation — it means the stream is
effectively broken for two users in three. The slow-burn threshold (28%) is the
one that catches ordinary degradation.

**User impact:** the chat feels frozen. Users retry or abandon, and a retry
costs another model call.

---

## First three actions

**1. Check whether the provider is slow.**

```promql
histogram_quantile(0.95, sum by (le) (rate(nester_outbound_request_duration_seconds_bucket{upstream="anthropic_relay"}[5m])))
sum by (kind) (rate(nester_outbound_errors_total{upstream="anthropic_relay"}[5m]))
```

Provider latency is the cause most of the time.

**2. Separate provider time from our own pre-token work.**

```promql
intelligence:ttft:p95_5m
```

Compare against the provider p95 above. TTFT much larger than provider latency
means the delay is on our side — before the model call is even made.

**3. Check whether errors are rising too.**

```promql
sum by (outcome) (rate(nester_intelligence_requests_total[5m]))
```

Latency degrading into errors means the provider is failing, not just slow —
use [intelligence-availability](intelligence-availability.md) instead.

---

## Dashboards

| Panel | Question it answers |
|---|---|
| **Time to first token p95** | How slow, against the 3s line |
| **Share slower than 3s** | The SLI itself |
| **Model provider health** | Is the provider the cause |
| **Requests by outcome** | Is this becoming an availability incident |

---

## Logs

```bash
kubectl logs -l app=nester-intelligence --since=30m | grep -E "Duration: [0-9]{2,}\."

# Cost governor throttling, which delays the call before it starts
kubectl logs -l app=nester-intelligence --since=30m | grep -i "budget\|governor\|throttle"
```

Locally: `docker compose logs intelligence --since 30m`.

---

## Traces

The most useful tool for this alert, because the question is *what happens
before the first token*.

In [Jaeger](http://localhost:16686), service `nester-intelligence`, sorted by
duration. The model-call span from #1054 carries
`nester.llm.time_to_first_token_ms`, and the spans preceding it show the
pre-call work: grounding retrieval, history loading, summarization, guardrail
screening.

Slow requests are retained regardless of sample ratio, so the slowest streams
are specifically the ones most likely to have a trace.

---

## Three most likely causes

### Cause A — Model provider queueing

**Distinguishing evidence:** provider p95 tracks TTFT closely; no unusual
pre-call work in traces; errors flat.

**Mitigation:** none available to us. Monitor. If it persists for hours,
consider whether the configured model is under unusual load and whether a
different one is appropriate — but that is a product decision, not an incident
action.

### Cause B — Pre-call work is slow (ours)

**Distinguishing evidence:** TTFT substantially exceeds provider latency. The
gap is our own work before the model call:

- **Grounding retrieval** — fetching user context from the Go API. Check
  `nester_outbound_*{upstream="intelligence"}` from the API side and Redis
  latency.
- **History summarization** — triggered when history exceeds
  `max_history_tokens`. This makes an *extra* model call before the real one,
  which can double TTFT. Traces show it clearly as a second model span.
- **Guardrail screening** — normally microseconds; slow only if pathological
  input hits a bad regex.

**Mitigation:** if summarization is the cause and is firing constantly,
`max_history_tokens` may be set too low for actual usage — that is a
configuration change, not an incident fix. If grounding retrieval is slow, the
Go API is the real problem.

### Cause C — Redis latency

**Distinguishing evidence:**

```promql
histogram_quantile(0.95, sum by (le) (rate(nester_redis_command_duration_seconds_bucket[5m])))
sum(rate(nester_redis_errors_total[5m]))
```

Conversation history is loaded from Redis before every call, so Redis latency
lands directly in TTFT.

**Mitigation:** restore Redis performance. Check memory pressure and eviction.

---

## Immediate mitigation

1. **Roll back** if a deploy correlates and the delay is on our side.
2. **Restore Redis** if it is the cause.
3. **Wait and monitor** for provider queueing — the honest answer most of the
   time.

Disabling the feature is rarely right here: slow answers are still answers, and
users who wanted coaching would rather wait than lose it. Reserve that for the
availability runbook.

---

## Escalation

Escalate when:

- p95 TTFT exceeds 15s, at which point the feature is effectively unusable.
- Degradation is on our side and persists more than two hours.
- Latency is turning into errors.

Escalate to the repository maintainers (CODEOWNERS). Business-hours work.

---

## Recovery verification

1. `intelligence:ttft:error_ratio_rate5m` below threshold for 15 minutes.
2. `intelligence:ttft:p95_5m` back under 3s.
3. Streams are actually occurring — the ratio is meaningless at zero volume.
4. Intelligence probe passing and not slow:
   ```promql
   nester_probe_duration_seconds{probe="intelligence"}
   ```

---

## Follow-up

**Postmortem required** only when the cause was ours and the degradation lasted
more than two hours.

Provider queueing does not warrant a postmortem, but recording occurrences
builds the case for a fallback or a different model configuration.

If the budget is in warning or exhausted, apply
[the error-budget policy](../error-budget-policy.md).

95% under 3s is the loosest target in the SLO set, chosen because TTFT depends
on provider queueing we cannot influence. If it is breached routinely without
user complaints, it may be measuring something users do not care about — bring
that to the next [SLO review](../slo-review.md) rather than quietly loosening
it further.
