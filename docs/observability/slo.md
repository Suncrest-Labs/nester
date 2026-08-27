# Service level objectives, error budgets, and burn-rate alerting

Implements [nester#1056](https://github.com/Suncrest-Labs/nester/issues/1056).
Builds on the metrics from #1043 and the tracing from #1054.

This document describes what is implemented, not what is planned. Where
something is deliberately deferred or not yet calibrated, it says so.

---

## Contents

- [What this exists to solve](#what-this-exists-to-solve)
- [SLI definitions](#sli-definitions)
- [SLO targets and their justification](#slo-targets-and-their-justification)
- [Error budgets](#error-budgets)
- [Burn-rate alerting: the arithmetic](#burn-rate-alerting-the-arithmetic)
- [Alert inventory](#alert-inventory)
- [Synthetic probes](#synthetic-probes)
- [Dashboards](#dashboards)
- [Cardinality and privacy](#cardinality-and-privacy)
- [Running it locally](#running-it-locally)
- [Open items](#open-items)

---

## What this exists to solve

Metrics and traces produce telemetry. Neither says what *good* means.

Without SLOs the failure-detection mechanism is a user noticing and telling
someone, which works while the user count is small and stops working exactly
when it matters. Without an error budget there is no principled way to choose
between shipping features and fixing reliability, so that decision gets made by
whoever argues most persuasively at the time.

Two properties of this design are worth stating up front because they drive
most of the specific choices below.

**Alerting is on burn rate, not on instantaneous thresholds.** "Error rate
above 1%" pages at 3am for a 30-second blip and stays silent through a week of
0.9% degradation that consumes the entire quarter's budget. Multi-window
multi-burn-rate alerting fixes both directions at once.

**Exclusions are defined precisely and applied to both halves of every ratio.**
A vague SLI produces arguments during an incident. An exclusion applied only to
the numerator quietly reports a better SLI than reality.

---

## SLI definitions

Every SLI below states its numerator, denominator, source, and exclusions. The
recording rules that implement them are in
[`docker/prometheus/rules/slo_recording.yml`](../../docker/prometheus/rules/slo_recording.yml).

All ratios are recorded as **error ratios** (bad ÷ eligible) rather than
success ratios, so a burn-rate threshold reads against them directly without an
inversion at each use.

### 1. API availability

| | |
|---|---|
| **Numerator** | Requests returning 5xx |
| **Denominator** | Requests that matched a registered route, minus 4xx |
| **Source** | `nester_http_requests_total{route,method,status_class}` |
| **Window** | 5m / 30m / 1h / 6h for alerting, 28d for attainment |
| **Aggregation** | `sum(rate(...))` across all routes and instances |
| **Recorded as** | `api:availability:error_ratio_rate{5m,30m,1h,6h,28d}` |

**Excluded, and why:**

- **4xx**, from *both* numerator and denominator. From the numerator because a
  400 or 403 is the API correctly rejecting a bad request. From the denominator
  too, because leaving it there lets a flood of 404s dilute a real 5xx spike —
  the ratio falls while the absolute number of failed requests climbs.
- **`route="other"`** (unmatched paths: scanners, probes, typos), entirely.
  These are not a service anyone is promised.
- **429** is 4xx and therefore excluded. A rate limiter shedding load is the
  system defending itself. A *misconfigured* limiter shedding legitimate
  traffic shows up as a collapse in eligible volume, which is why the
  dashboards plot volume beside the ratio.

The 4xx exclusion is what stops a client from manufacturing an availability
breach by looping invalid requests. This is verified by a test
(`4xx flood does not breach availability SLO`).

**Failure semantics:** a request with no response (client disconnect before the
handler returns) is recorded by the deferred middleware with whatever status
was written, defaulting to 200. Panics are recorded as 5xx by the recover
middleware before this metric observes them.

### 2. Deposit success rate

| | |
|---|---|
| **Numerator** | `failed_chain` + `failed_internal` |
| **Denominator** | All attempts except `rejected` and `cancelled` |
| **Source** | `nester_flow_attempts_total{flow="deposit",outcome}` |
| **Recorded as** | `flow:success:error_ratio_rate*{flow="deposit"}` |

Outcomes are classified in
[`apps/api/internal/service/vault_flow_metrics.go`](../../apps/api/internal/service/vault_flow_metrics.go):

| Outcome | Meaning | In denominator? | Counts as failure? |
|---|---|---|---|
| `succeeded` | Chain call returned **and** ledger row written | yes | no |
| `rejected` | Service refused before reaching the chain: invalid amount, excess precision, closed vault, insufficient balance, unknown vault, below contract minimum | **no** | — |
| `cancelled` | User declined the wallet signature or abandoned the attempt | **no** | — |
| `failed_chain` | Soroban invocation errored, timed out, or the transaction failed on-chain | yes | **yes** |
| `failed_internal` | Database write failure, panic, unhandled path | yes | **yes** |

**Attempted** means the service accepted the request and began carrying it
toward the chain. **Successful** requires *both* the chain call and the ledger
write — a chain success whose database write failed is not a success, because
the user's balance does not reflect their money.

**`failed_chain` is deliberately not excluded** even though Soroban RPC is not
ours. A user whose deposit did not land does not care which side of the
boundary broke, and an SLI that excused unowned infrastructure would report
perfect health through a total chain outage.

`ErrBelowMinDeposit` is the one genuinely ambiguous case. It is classified as
`rejected`, because counting it as a chain failure would let a UI bug that
permits sub-minimum deposits burn the deposit budget.

An unclassified error defaults to `failed_internal`. That direction is
deliberate: a new failure mode shows up as a burn rather than disappearing.

### 3. Withdrawal success rate

Identical treatment to deposits, on `flow="withdrawal"`.

Deposits and withdrawals carry **separate budgets** rather than one combined
"transaction" SLO. Averaging them lets healthy deposit volume hide a withdrawal
outage, and being unable to withdraw is the worse failure. Verified by the test
`a withdrawal outage is not hidden by healthy deposit volume`.

### 4. Deposit and withdrawal latency

| | |
|---|---|
| **Numerator** | Successful settlements taking longer than **30s** |
| **Denominator** | Successful settlements |
| **Source** | `nester_flow_duration_seconds_bucket{outcome="succeeded"}` |
| **Recorded as** | `flow:latency:error_ratio_rate*` |

**Start timestamp:** when the service accepts the request (`RecordDeposit` /
`RecordWithdrawal` entry). **Confirmation timestamp:** when the chain call
returns and the ledger row is written.

**This is a ratio of slow events, not a percentile**, and that is the one
non-obvious choice here. Alerting on "p95 > threshold" cannot be converted into
an error budget: a percentile is not a count of bad events, so there is nothing
to burn. Counting requests slower than a threshold yields a bad/eligible ratio
with the same shape as every other SLI, so the same burn-rate arithmetic
applies unchanged. A p95 is still recorded (`flow:latency:p95_5m`) for the
dashboards, because operators think in percentiles.

**Why 30s:** Stellar ledgers close at roughly 5s, so a confirmed settlement
cannot beat ~5s and a normal one lands in 5–15s. 30s is ~2–6 ledgers:
comfortably above a healthy confirmation, tight enough that a user watching a
spinner is counted as badly served. The histogram has an explicit 30s boundary,
so no interpolation is involved.

**Abandoned and cancelled attempts** never reach the histogram: they terminate
before the chain call. **Rejected attempts** are explicitly not observed —
they complete in microseconds and would drag every percentile toward zero,
making the latency SLI report health during a chain stall.

**Failures are excluded** from the latency denominator. A failure's duration
measures how long the failure took to surface, which is a different question;
folding it in would let a fast-failing outage look like a latency improvement.

**Chain and indexer delays** are inside the measurement by design. The user
experiences them, so the SLI does too.

### 5. Balance freshness

| | |
|---|---|
| **Measurement** | network ledger tip − last successfully indexed ledger |
| **Source** | `nester_indexer_lag_ledgers` |
| **Acceptable** | ≤ 60 ledgers (~5 minutes) |
| **Stale threshold** | lag sample older than 300s |

Sampled inside the event-indexer loop from the tip that `fetchSorobanEvents`
already returns, so it costs no extra RPC call. Measured in ledgers rather than
seconds because the indexer's own unit is the ledger sequence; converting to
time would bake in an assumed close interval that varies.

**Missing-data behaviour is the important part of this SLI.** A lag gauge alone
cannot distinguish "lag is 0" from "the sampler died and the value is frozen at
its last write" — and the second silently reports perfect health, which is the
worst possible failure for a freshness signal. Three mechanisms prevent it:

1. `nester_indexer_lag_last_sample_age_seconds` ages between successful
   samples, so `IndexerLagStale` fires when the signal itself goes stale.
2. `nester_indexer_lag_sample_errors_total` counts failed samples rather than
   writing a sentinel lag, which would be indistinguishable from a real stall.
3. `IndexerMetricsAbsent` fires on `absent(...)`, so deleting or breaking the
   metric cannot buy silence.

**Cold start:** a zero cursor (never indexed, or wiped `system_state`) is
skipped rather than published, because `tip − 0` would report the entire ledger
history as lag and page on every fresh deploy. The staleness alert then fires
instead, which is the honest signal for "this indexer has never run".

**No burn rate.** This is a gauge, not a ratio of events. Forcing it into
burn-rate shape would produce a number that cannot be reasoned about during an
incident.

### 6. Intelligence availability

| | |
|---|---|
| **Numerator** | `outcome="error"` |
| **Denominator** | `answered` + `refused` + `error` |
| **Source** | `nester_intelligence_requests_total{outcome}` |
| **Recorded as** | `intelligence:availability:error_ratio_rate*` |

Measured inside the intelligence service rather than from the Go API's outbound
view, because only this process can tell a refusal (a 200 carrying a polite
sentence) apart from an answer. The outbound metrics remain the
dependency-health signal the runbook uses.

- **Request failures, dependency failures, and timeouts** all count as `error`.
- **`cancelled`** (client disconnected mid-stream) is **excluded** from the
  denominator: the service was never given the chance to succeed or fail.
- **Model refusal is availability *success*.** The service worked exactly as
  designed. Refusal is tracked as a separate product-quality indicator (§8)
  with its own target — this is the explicit answer to the issue's question
  about whether refusal is an availability failure.

### 7. Intelligence latency (time to first token)

| | |
|---|---|
| **Numerator** | Streams whose first token took longer than **3s** |
| **Denominator** | Streams that produced a first token |
| **Source** | `nester_intelligence_time_to_first_token_seconds_bucket` |

**TTFT is the SLI, not total duration**, because perceived responsiveness of a
stream is dominated by when text starts appearing. A 40s answer that begins in
800ms feels fine; an 8s answer that begins at 7s feels broken.

Total completion latency is recorded as
`nester_intelligence_request_duration_seconds{outcome}` for the dashboards, but
carries no SLO: it varies legitimately with answer length and tool rounds, so a
target on it would be a target on how much users ask for.

TTFT already existed as a **span attribute** (`nester.llm.time_to_first_token_ms`,
from #1054). It is re-recorded as a histogram because spans are sampled, and an
error budget computed from a 10% sample is not an error budget — a rare failure
could vanish entirely. The metric sits alongside the span, not instead of it.

**Why 3s:** below ~1s reads as instant, beyond ~5s users abandon. 3s is where
the experience has measurably degraded but the answer is still worth waiting
for. The histogram has an explicit 3s boundary.

### 8. Intelligence refusal rate

| | |
|---|---|
| **Numerator** | `outcome="refused"` |
| **Denominator** | `answered` + `refused` |
| **Source** | `nester_intelligence_requests_total`, split by reason in `nester_intelligence_refusals_total{reason}` |

**Errors are excluded from the denominator.** A request that failed was never
given the chance to be refused; including errors would make the refusal rate
move whenever availability moved, reporting two independent problems as one
number.

`reason` is a closed set of two: `guardrail` (declined by the guardrail layer)
and `grounding` (no account data to answer from). Split because "refusals are
up" is not actionable, while "guardrail refusals are up and grounding refusals
are flat" points at one deploy.

**This is a product-quality guardrail, not an availability signal, and it has
no error budget.** Some baseline of refusals is correct and healthy: users do
ask off-topic questions, and grounding *should* decline when data is missing.
There is no principled budget of correct behaviour to burn. What matters is
departure from baseline, so it alerts on an absolute threshold over a long
window and never pages.

Detection is on the emitted text, which is where both mechanisms converge; a
flag threaded out of each refusal site would miss any site added later.

### 9. Intelligence request latency (total)

Recorded as `nester_intelligence_request_duration_seconds{outcome}` for
dashboards and incident triage. **No SLO**, for the reason given in §7.

---

## SLO targets and their justification

Rolling **28-day** window throughout. 28 days rather than 30 because it is a
whole number of weeks, so the window always contains exactly four of each
weekday and the attainment figure does not shift with which day you read it.

| SLI | Target | Budget | Time equivalent per 28d | Severity |
|---|---|---|---|---|
| API availability | 99.9% | 0.1% | ~40 min | page |
| Deposit success | 99.5% | 0.5% | ~3h 22m | page |
| Withdrawal success | 99.5% | 0.5% | ~3h 22m | page |
| Deposit/withdrawal latency | 99% under 30s | 1% | ~6h 43m | ticket |
| Balance freshness | ≤60 ledgers | n/a (gauge) | n/a | page |
| Intelligence availability | 99% | 1% | ~6h 43m | ticket |
| Intelligence TTFT | 95% under 3s | 5% | ~33h 36m | ticket |
| Intelligence refusal | <30% over 6h | n/a (guardrail) | n/a | ticket |

Time equivalents are `budget × 28 days × 24 h`, i.e. the total outage duration
the budget permits if the service were fully down for that period.

### API availability — 99.9%

**Target:** 99.9%, allowing ~40 minutes of full unavailability per 28 days.

**Justification:** this is the front door; every user-facing action passes
through it, so its failures are maximally visible. 40 minutes is a realistic
budget for the current deployment topology — a single region with no automated
failover — where a bad deploy plus rollback consumes 5–15 minutes and the
budget therefore tolerates two or three such events per month without
escalation.

**Why not stricter:** 99.95% (~20 min) would be consumed by two bad deploys.
With no multi-region failover and no automated rollback there is no mechanism
that could deliver it, and a target the deployment cannot meet produces alerts
nobody can act on.

**Why not weaker:** 99.5% permits ~3.4 hours per 28 days. A savings product
unreachable for three hours during business hours is a support and trust
problem, not a statistic.

### Deposit and withdrawal success — 99.5%

**Target:** 99.5% each, ~1 failed attempt in 200.

**Justification:** looser than API availability, and deliberately so. This SLI
spans on-chain settlement, where Soroban RPC and network conditions are outside
our control *and* outside our ability to repair during an incident. A target
that treats an unavoidable chain hiccup as a budget emergency trains the
on-call to ignore the alert, which costs more reliability than the looser
number does.

**Why not stricter:** 99.9% would mean 1 failure in 1000 including every
upstream RPC blip. Observed Soroban reliability does not support that, so the
budget would be exhausted by conditions we cannot fix.

**Why not weaker:** 99% is 1 failure in 100. For a savings product, one in a
hundred deposits silently not landing is a trust failure — users do not retry
money movements with confidence.

**Why paging:** funds not moving is the most acute user-visible failure this
product has.

### Deposit and withdrawal latency — 99% under 30s

**Target:** 99% of successful settlements complete within 30s.

**Justification:** the tail is dominated by ledger close timing, which is
genuinely variable and not ours. Slow is also strictly better than failed — the
money still arrives.

**Why not stricter:** a two-ledger confirmation already lands near 11s; normal
variance in close timing would breach a tighter target without anything being
wrong.

**Why not weaker:** beyond 30s users start opening support tickets and
retrying, and a retried deposit is a support burden even when both eventually
succeed.

**Why ticket, not page:** if settlement has stopped entirely, the *success* SLO
is already paging. This alert covers "working but slow", which has no 3am
action.

### Intelligence availability — 99%

**Target:** 99%.

**Justification:** depends on a third-party model API whose availability we
neither control nor can repair. The feature is advisory — a user who cannot get
coaching still has full access to their money — so the user impact of failure
is materially lower than for the API or the money paths.

**Why not stricter:** 99.9% would make us accountable for a provider's uptime
without any mechanism to influence it.

**Why not weaker:** below 99% the feature stops being dependable enough to
build product surface on.

**Why ticket:** no funds are at risk, and there is no 3am action that fixes an
upstream provider.

### Intelligence TTFT — 95% under 3s

**Target:** 95% of streams produce a first token within 3s.

**Justification:** the loosest target here, deliberately. TTFT depends on
provider queueing we cannot influence, and the consequence of a slow first
token is impatience, not lost money.

Note the fast-burn threshold works out to 67.2% of requests exceeding 3s. That
is not a subtle degradation — it is the stream being effectively broken — which
is the correct bar for this indicator.

**Why not stricter:** a tighter target would generate tickets nobody can
action, since the fix is "wait for the provider".

**Why not weaker:** below 95% the chat feels unreliable often enough that users
stop using it.

### Intelligence refusal — <30% over 6h

**Threshold:** 30% of answerable requests refused, sustained over 6h.

**This number is a starting estimate, not a measurement.** It is set high
enough that ordinary off-topic traffic and legitimate data-missing refusals do
not reach it, and low enough that a guardrail regression refusing a large share
of valid questions is caught the same day. Recalibrating it against observed
baseline is the first agenda item of the first SLO review — see
[Open items](#open-items).

---

## Error budgets

For a ratio-based SLO:

```
error_budget       = 1 - SLO_target
budget_consumed    = observed_error_ratio_28d / error_budget
budget_remaining   = 1 - budget_consumed
```

`budget_remaining` is deliberately allowed to go negative and is **not** clamped
at zero: "how far past" is what the policy escalation in
[error-budget-policy.md](error-budget-policy.md) depends on.

Worked example, API availability at 99.9% with an observed 28-day 5xx ratio of
0.04%:

```
error_budget     = 1 - 0.999      = 0.001   (0.1%)
budget_consumed  = 0.0004 / 0.001 = 0.40    (40% spent)
budget_remaining = 1 - 0.40       = 0.60    (60% left)
time equivalent  = 0.001 × 28d    = 40.3 min allowed
                   0.0004 × 28d   = 16.1 min consumed
```

Each dashboard shows attainment, budget remaining, and both burn rates as its
first row of panels, computed from the `*_rate28d` recorded series.

**Balance freshness and refusal rate have no error budget.** One is a gauge and
the other measures correct behaviour. Applying the formula to either would
produce a number with no meaning, so neither is given one rather than inventing
a synthetic event count to force the shape.

---

## Burn-rate alerting: the arithmetic

**Burn rate** is how fast the budget is being consumed relative to the rate
that would exhaust it exactly at the end of the window:

```
burn_rate = observed_error_ratio / error_budget
```

Burn rate 1 exhausts the budget in exactly 28 days. Burn rate *N* exhausts it
in 28/*N* days.

A 28-day window contains **28 × 24 = 672 hours**. The two policies the issue
requires convert to burn rates as follows.

### Fast burn — 2% of the budget in 1 hour

```
burn_rate = 0.02 × (672 h / 1 h) = 13.44
```

At 13.44× the whole budget is gone in `1 / 0.02 = 50 hours` (~2.1 days).

- **Observation window:** 1h — long enough for the ratio to be statistically
  meaningful at realistic traffic.
- **Confirmation window:** 5m — must also exceed the threshold.
- **`for:`** 2m.
- **Severity:** page, where the SLO warrants it.

### Slow burn — 5% of the budget in 6 hours

```
burn_rate = 0.05 × (672 h / 6 h) = 5.6
```

At 5.6× the budget is gone in `6 / 0.05 = 120 hours` (5 days).

- **Observation window:** 6h.
- **Confirmation window:** 30m.
- **`for:`** 15m.
- **Severity:** ticket.

### Per-SLO thresholds

The multipliers are SLO-independent. What changes per SLO is the absolute error
ratio each represents:

| SLO target | Budget | Fast threshold (13.44×) | Slow threshold (5.6×) |
|---|---|---|---|
| 99.9% | 0.1% | 1.344% | 0.560% |
| 99.5% | 0.5% | 6.720% | 2.800% |
| 99% | 1.0% | 13.440% | 5.600% |
| 95% | 5.0% | 67.200% | 28.000% |

### Why each alert requires two windows

The long window defines the burn policy. On its own it has a tail problem: a
5-minute outage keeps a 1-hour window elevated for a full hour after recovery,
so the page persists long after the incident ends and the on-call learns to
ignore it.

The short window is a **recovery detector**, not a second opinion. Once the
incident stops, the short window falls below threshold within minutes and the
alert resolves, while the long window still carries the history that justified
firing. It also suppresses single-scrape spikes, since one bad scrape cannot
hold a 5-minute rate above 13.44×.

This behaviour is verified by the test
`API availability fast burn resolves once the burst stops`.

The 12:1 window ratios (1h/5m, 6h/30m) match the Google SRE workbook, used here
because that ratio makes the recovery detector responsive without being
twitchy — not because it is conventional.

### Tuning

To change a target, edit the threshold in
[`slo_alerts.yml`](../../docker/prometheus/rules/slo_alerts.yml) — each group
carries its own arithmetic in a comment — then update the expected values in
[`slo_alerts_test.yml`](../../docker/prometheus/rules/slo_alerts_test.yml) and
the tables above. CI runs the tests against the real rule files, so a threshold
changed without updating its arithmetic fails the build.

---

## Alert inventory

| Alert | Severity | Fires when | Runbook |
|---|---|---|---|
| `APIAvailabilityFastBurn` | page | 5xx ratio >1.344% on 1h **and** 5m | [api-availability](runbooks/api-availability.md) |
| `APIAvailabilitySlowBurn` | ticket | >0.56% on 6h **and** 30m | [api-availability](runbooks/api-availability.md) |
| `FlowSuccessFastBurn` | page | Flow failure >6.72% on 1h **and** 5m | [flow-success](runbooks/flow-success.md) |
| `FlowSuccessSlowBurn` | ticket | >2.8% on 6h **and** 30m | [flow-success](runbooks/flow-success.md) |
| `FlowLatencyFastBurn` | ticket | >13.44% slower than 30s | [flow-latency](runbooks/flow-latency.md) |
| `FlowLatencySlowBurn` | ticket | >5.6% slower than 30s over 6h | [flow-latency](runbooks/flow-latency.md) |
| `IntelligenceAvailabilityFastBurn` | ticket | Error ratio >13.44% | [intelligence-availability](runbooks/intelligence-availability.md) |
| `IntelligenceAvailabilitySlowBurn` | ticket | >5.6% over 6h | [intelligence-availability](runbooks/intelligence-availability.md) |
| `IntelligenceTTFTFastBurn` | ticket | >67.2% of streams over 3s | [intelligence-latency](runbooks/intelligence-latency.md) |
| `IntelligenceTTFTSlowBurn` | ticket | >28% over 6h | [intelligence-latency](runbooks/intelligence-latency.md) |
| `IntelligenceRefusalRateHigh` | ticket | >30% refusals over 6h | [intelligence-refusal](runbooks/intelligence-refusal.md) |
| `IndexerLagHigh` | page | Lag >60 ledgers for 5m | [balance-freshness](runbooks/balance-freshness.md) |
| `IndexerLagStale` | page | Lag sample older than 5m | [balance-freshness](runbooks/balance-freshness.md) |
| `IndexerMetricsAbsent` | page | Freshness series absent | [balance-freshness](runbooks/balance-freshness.md) |
| `SLOTargetDown` | page | Scrape target down 5m | [monitoring-down](runbooks/monitoring-down.md) |
| `SLOMetricsAbsent` | page | Core request metrics absent | [monitoring-down](runbooks/monitoring-down.md) |
| `SyntheticProbeFailing` | ticket | Probe failing 10m | [synthetic-probes](runbooks/synthetic-probes.md) |
| `SyntheticProbeStale` | ticket | No probe result for 30m | [synthetic-probes](runbooks/synthetic-probes.md) |
| `SyntheticProbeMetricsAbsent` | ticket | No probe series | [synthetic-probes](runbooks/synthetic-probes.md) |
| `SyntheticProbeSlow` | ticket | Probe >60s | [synthetic-probes](runbooks/synthetic-probes.md) |

Every alert carries `summary`, `description`, `dashboard`, and `runbook`
annotations, and the burn alerts additionally carry `budget_remaining`. No
alert says only "SLO breached".

### Routing

[`docker/alertmanager/alertmanager.yml`](../../docker/alertmanager/alertmanager.yml)
groups by `(slo, burn, service)` so a broad incident produces one notification
per affected SLO rather than one per firing series. Pages repeat hourly,
tickets daily.

Two inhibition rules prevent one incident producing several notifications:

- A **fast burn suppresses the slow burn** on the same SLO — the fast alert is
  strictly more urgent and describes the same incident.
- **`SLOTargetDown` suppresses everything** for that service. The target being
  down is the actionable fact; the derived alerts are noise on top of it.

Receivers are intentionally empty in the committed config. A local stack must
never be able to page a human, and a webhook URL in a committed file is a
credential leak. Deployments supply destinations from their own environment.

---

## Synthetic probes

Real-traffic SLIs cannot detect a flow that is broken while nobody is using it.
Burn-rate alerts are ratios over observed traffic, so an idle service produces
no bad events and no alert — correct, but it leaves exactly the gap where a
withdrawal path breaks on Friday and the first person to notice is a user on
Monday.

[`tests/probes/probe.py`](../../tests/probes/probe.py) exercises four flows
against staging every 15 minutes via
[`.github/workflows/synthetic-probes.yml`](../../.github/workflows/synthetic-probes.yml):

| Probe | Mutating? | Checks |
|---|---|---|
| `balance` | no | Vault read returns a well-formed response |
| `intelligence` | no | A grounded question returns a response (a refusal counts as success) |
| `deposit` | **yes** | A minimal deposit settles |
| `withdrawal` | **yes** | The same amount withdraws back out |

Deposit runs before withdrawal so a cycle is balance-neutral. Withdrawing first
would fail on an empty vault and report an outage that is really probe
sequencing.

### Preventing financial side effects

Three independent guards:

1. **No default target.** `PROBE_API_BASE_URL` unset aborts. There is no
   fallback to localhost and none to a deployed host.
2. **Environment and URL are checked separately.** `PROBE_ENVIRONMENT` is
   matched against a deny list (`production`, `prod`, `live`, `mainnet`) *and*
   the target URL is checked for production markers. The two are configured
   independently, so the dangerous case is one of them being wrong.
3. **Mutations are opt-in.** Deposit and withdrawal require
   `PROBE_ALLOW_MUTATIONS=true`. A default run is read-only, and naming a
   mutating probe on the command line is not consent to move funds.

A misconfigured probe exits **2**; a failed probe exits **1**. A bad config is
never mistaken for an outage.

Probes use ordinary API credentials for a dedicated staging account against
testnet funds. They are not privileged and cannot reach another user's vault.

### Probe output

Each probe reports success, latency, a bounded failure reason, and a timestamp.
The reason is a closed enum (`timeout`, `connection`, `http_4xx`, `http_5xx`,
`bad_payload`, `assertion`, `unknown`) — an exception string can contain a vault
ID, an amount, or an upstream URL, so the detail goes to the workflow log and
only the enum becomes a label.

---

## Dashboards

Five dashboards in the **SLO** folder, generated by
[`scripts/build_slo_dashboards.py`](../../scripts/build_slo_dashboards.py) and
committed. CI fails if the committed JSON differs from what the generator
produces.

| Dashboard | UID | Covers |
|---|---|---|
| SLO — API availability | `nester-slo-api` | Availability, status classes, slow routes, dependency health |
| SLO — Deposits and withdrawals | `nester-slo-flow` | Both flows' success and latency, outcomes, Soroban health |
| SLO — Intelligence | `nester-slo-intel` | Availability, TTFT, refusals by reason, provider health |
| SLO — Balance freshness | `nester-slo-balance` | Indexer lag, sample age, sample errors |
| SLO — Synthetic probes | `nester-slo-probes` | Probe outcome, duration, reasons, result age |

Every SLO dashboard opens with the same four panels — **attainment, budget
remaining, 1h burn rate, 6h burn rate** — then the error ratio over time
against its burn thresholds, then the supporting signals an operator needs
next. Each carries its SLI definition, exclusions, and runbook path in a header
panel.

**Every panel reads the recorded series the alerts fire on**, never a raw
counter. That is what stops a dashboard disagreeing with the pager during an
incident, which is precisely when nobody has time to work out which one is
lying.

---

## Cardinality and privacy

The metrics inherit the cardinality policy from #1043 (see
[metrics.md](metrics.md)) and the financial domain makes it sharper.

**Never a label:** user ID, wallet address, transaction hash, vault ID, amount,
prompt text, model answer, raw error string, request path, request ID.

Every label added by this work is a closed constant set fixed at compile time:

| Label | Values | Bound |
|---|---|---|
| `flow` | deposit, withdrawal | 2 |
| `outcome` (flow) | succeeded, rejected, cancelled, failed_chain, failed_internal | 5 |
| `outcome` (intelligence) | answered, refused, error, cancelled | 4 |
| `reason` | guardrail, grounding | 2 |
| `probe` | balance, intelligence, deposit, withdrawal | 4 |
| `probe reason` | ok, timeout, connection, http_4xx, http_5xx, bad_payload, assertion, unknown | 8 |

Total added series is bounded at roughly `2×5 + 2×5×11 buckets + 4 + 2 + gauges`
— a few hundred, fixed, and unable to grow with traffic or be influenced by a
caller.

**Specific protections:**

- Flow outcome classification maps errors to a fixed enum. An unclassified
  error becomes `failed_internal`, never its message.
- Probe failure detail is truncated to 200 characters and goes to the log only.
  A test asserts no probe label falls outside `{probe, reason, environment}`.
- Refusal tracking records only *that* a refusal happened and by which
  mechanism. No prompt or answer text is recorded.
- No amount appears anywhere in metrics or alert annotations.

**Alert-driven cardinality:** every alert expression aggregates with `sum()` or
`max()` before comparison, so a malformed metric cannot produce a per-series
alert storm. Alertmanager groups by `(slo, burn, service)`, all bounded.

**Monitoring failures do not break the application:**

- `RecordFlowAttempt` and the lag setters are nil-safe; a service constructed
  without metrics behaves exactly as before.
- Indexer lag sampling is inside the existing loop with a nil-checked recorder;
  it cannot stop the indexer from indexing.
- The Go metrics listener failing is logged and non-fatal.
- The Prometheus handler uses `ContinueOnError`, so a broken collector degrades
  the scrape rather than 500ing the endpoint and blinding every other metric.

**Missing telemetry fails visibly, not silently.** `IndexerMetricsAbsent`,
`SLOMetricsAbsent`, `SLOTargetDown`, and `SyntheticProbeMetricsAbsent` exist so
that absent data alerts rather than reading as health. The `or vector(0)`
guards in the recording rules exist for the same reason — see
[Open items](#open-items) for the bug that motivated them.

---

## Running it locally

```bash
docker compose --profile observability up
```

| Service | URL |
|---|---|
| Prometheus | http://localhost:9091 |
| Alertmanager | http://localhost:9093 |
| Grafana | http://localhost:3002 |
| Jaeger (#1054) | http://localhost:16686 |

Validate and test the rules the way CI does:

```bash
promtool check config docker/prometheus/prometheus.yml
amtool check-config docker/alertmanager/alertmanager.yml
cd docker/prometheus/rules && promtool test rules slo_alerts_test.yml slo_probe_alerts_test.yml
cd tests/probes && python -m pytest -q
python scripts/build_slo_dashboards.py   # then check git diff is empty
```

All of this runs in the **SLO rules** CI job on any change under
`docker/prometheus/`, `docker/alertmanager/`, `docker/grafana/`, or
`tests/probes/`.

---

## Open items

Recorded here rather than presented as complete.

1. **The first SLO review has not been held.** The cadence, agenda, and
   participants are defined in [slo-review.md](slo-review.md); the first review
   is scheduled but has not occurred. No review has taken place as of this
   change.

2. **Targets are set from reasoning, not from observed history.** Every
   justification above argues from deployment topology and user impact, because
   there is no 28-day history of these SLIs yet — the telemetry for five of
   them is introduced by this change. The first review after 28 days of data is
   the point at which they can be validated, and some will likely move.

3. **The refusal threshold (30%) is uncalibrated.** It is an estimate, not a
   measurement. Recalibrating against observed baseline is the first agenda
   item of the first review.

4. **Synthetic probes need staging secrets before they run.** The workflow
   skips cleanly when `STAGING_PROBE_API_BASE_URL` is unset, which is its state
   on merge. Mutating probes need `STAGING_PROBE_ALLOW_MUTATIONS` set
   deliberately for the staging environment. Until then, probe coverage is
   zero and `SyntheticProbeMetricsAbsent` will say so.

5. **Alertmanager receivers are empty.** Routing policy is defined and testable,
   but no destination is configured. Connecting a real one is a deployment
   task, not a repository change.

6. **Intelligence exposition is bearer-token guarded, not network-isolated.**
   The Go API's separate loopback listener is the stronger arrangement; the
   intelligence service serves one port, so it uses a shared secret instead.
   `INTELLIGENCE_METRICS_TOKEN` must be set anywhere the port is reachable.

7. **A bug worth recording.** During development the alert tests caught
   `sum(all) - sum(4xx)` evaluating to an *empty vector* when a service had
   served no 4xx at all — which deleted the API availability SLI entirely, so
   the alerts could never fire and would have stayed silent through a later 5xx
   outage. Fixed with `or vector(0)` and covered by two regression tests. It is
   noted here because it is the exact class of failure this document claims to
   prevent, and it was found by testing rather than by reasoning.
