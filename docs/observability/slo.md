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

### 1a. Per-route availability (nester#1227)

| | |
|---|---|
| **Numerator** | Requests to one route returning 5xx |
| **Denominator** | Requests to that route that matched a registered route, minus 4xx |
| **Source** | `nester_http_requests_total{route,method,status_class}`, same series as §1 |
| **Window** | 1h only — no attainment window, no burn-rate policy (see below) |
| **Aggregation** | `sum by (route) (rate(...))` |
| **Recorded as** | `route:availability:{eligible,bad,error_ratio}_rate1h` |
| **Alert** | `EndpointErrorRateHigh` — single absolute threshold, `> 20%` sustained 15m |

**Why this exists alongside §1, not instead of it:** §1 answers "is the API
healthy overall", and deposit/withdraw volume dominates that aggregate — a
low-traffic endpoint (an analytics or savings-goal route, say) can fail
continuously without moving it, because the money-path volume swamps the
signal. This section answers the narrower per-route question, using the same
bounded `route` label §1 already uses (never an arbitrary client-supplied
path — see `internal/metrics/http.go`'s `resolveRoute`, and its own tests
`TestRouteLabelUsesPatternNotRawPath` / `TestRouteLabelBoundedForUnmatchedPaths`
in `apps/api/internal/metrics/http_test.go`), so grouping `by (route)` cannot
mint an unbounded number of series.

**Why not the full burn-rate treatment §1 gets:** with roughly 93 registered
routes, replicating the four-window, two-severity burn-rate machinery per
route would multiply the alert count by ~93 and produce a flood of
near-duplicate pages for a single root cause (the aggregate alert and the
affected route's alert would both fire). A single absolute-threshold alert is
the deliberately simpler tool for "is this one route currently broken",
matching the doctrine §5 (balance freshness) already uses for signals that
don't warrant burn-rate shape.

**The 20% threshold and the 15m `for:`:** looser and slower than §1's
1.344%/2m fast-burn pair on purpose. This alert exists to catch a route that
is broken *continuously*, not one that is occasionally degraded — a
low-traffic route naturally has more variance, and a route carrying enough
traffic to be statistically meaningful at a low error rate is already covered
by the aggregate. Ticket severity, never page: the aggregate and the
money-path flow alerts already page for anything urgent enough to wake
someone; a single misbehaving low-traffic endpoint is a business-hours fix.

**Cardinality guard:** the `eligible_rate1h > 0.01` condition on the alert
(roughly one request per 100 seconds) exists so a single stray request on an
otherwise-idle route cannot produce a 100% error ratio and fire. Bounded
`route`-label cardinality itself is asserted at the promtool level in
`slo_alerts_test.yml` (`per-route recording rules do not introduce unbounded
labels beyond the bounded route set`) and at the Go level by the two
`resolveRoute` tests named above.

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
| **Measurement** | age of the indexed view of the chain, in seconds and in ledgers |
| **Source** | `nester_indexer_lag_seconds`, `nester_indexer_lag_ledgers` |
| **Staleness budget** | **300s (5 minutes)** — `INDEXER_STALENESS_BUDGET`, default `5m` |
| **Equivalent in ledgers** | ≤ 60 ledgers (60 × 5s close interval = 300s) |
| **Stale** | `lag_seconds > budget`. Strictly greater: exactly at the budget is fresh. |

#### The staleness budget

There is **one** budget, and it governs three things that must never disagree:
the alert that pages, the `X-Indexer-Stale` header the API returns to clients,
and this document. The application exports the value it is enforcing as
`nester_indexer_staleness_budget_seconds`, and `IndexerStalenessBudgetExceeded`
compares lag against that series rather than against a literal, so retuning
`INDEXER_STALENESS_BUDGET` moves the pager and the UI together.

**Why 300 seconds.** It is the pre-existing ledger-lag SLO restated in the unit
the API contract needs, not a new number: 60 ledgers at a ~5s close interval is
exactly 300s. Healthy operation sits an order of magnitude inside it — the
indexer polls every 6s and advances its cursor to the tip it just read, so a
healthy sample is a handful of ledgers behind and at most one poll old, well
under 20s of staleness. The gap between that and 300s absorbs a slow RPC round
trip, a rolling restart, and the 12-ledger cold-start rewind without paging,
while still catching a user staring at a wrong balance within minutes.

#### How staleness is calculated

```
lag_seconds = (now − last successful sample)
            + (network tip − indexed ledger) × 5s
```

The second term is the lag the indexer last observed. The first term is how
long it has been since it observed anything, and it is what makes this SLI
trustworthy: **both terms are required.**

Dropping the first would let the number freeze at a healthy value the moment
the indexer died — lag 0, dashboards green, pager silent — which is the single
most dangerous failure a freshness signal can have. Dropping the second would
report a busily-polling but hopelessly-behind indexer as perfectly fresh.

Because the first term grows on the clock, the value is **derived at scrape
time from `internal/freshness`, not pushed by the indexer**. A dead indexer
therefore produces a climbing number with nothing of ours still running to
produce it, and `IndexerStalenessBudgetExceeded` fires whether the indexer is
merely behind or has stopped completely.

The 5s close interval is nominal and real ledgers vary by a few hundred
milliseconds, so the seconds figure inherits that error. At the scale the
budget operates on — minutes — it is immaterial.

#### Missing data

1. `nester_indexer_lag_seconds` is always present and always moving, so an
   indexer that never started ages past the budget and pages rather than
   reporting zero forever.
2. `nester_indexer_lag_last_sample_age_seconds` separates "running but behind"
   (near zero) from "stopped" (climbing) — the first thing the runbook reads.
3. `nester_indexer_lag_sample_errors_total` counts failed samples rather than
   writing a sentinel lag, which would be indistinguishable from a real stall.
   A failed sample never resets the sample age.
4. `IndexerMetricsAbsent` fires on `absent(...)`, so deleting or breaking the
   metric cannot buy silence.

**Cold start:** before the indexer has committed a cursor there is no position
to report, so `nester_indexer_lag_ledgers` is **withheld** rather than
published as zero — `tip − 0` would report the entire ledger history as lag,
and a zero would claim the indexer is exactly at the tip, which is the value
that looks most perfect. The seconds view ages from process start instead, so a
fresh deployment gets exactly one budget of grace and an indexer that never
starts still pages.

#### Client contract

Stale balances are still served, with `X-Indexer-Stale: true`, rather than
turned into a 5xx: a stale balance a user is told about is far more useful than
an error, and failing the request would take down screens that do not depend on
indexed data at all. See [the metrics reference](metrics.md#indexer-freshness)
for the headers.

**No burn rate.** This is a gauge, not a ratio of events. Forcing it into
burn-rate shape would produce a number that cannot be reasoned about during an
incident.

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
| Balance freshness | ≤300s staleness (≤60 ledgers) | n/a (gauge) | n/a | page |

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

**Balance freshness has no error budget.** It is a gauge, not a ratio of
events. Applying the formula to it would produce a number with no meaning, so
it is not given one rather than inventing a synthetic event count to force the
shape.

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
| `IndexerStalenessBudgetExceeded` | page | Staleness over the budget (300s) for 2m | [balance-freshness](runbooks/balance-freshness.md) |
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

[`tests/probes/probe.py`](../../tests/probes/probe.py) exercises three flows
against staging every 15 minutes via
[`.github/workflows/synthetic-probes.yml`](../../.github/workflows/synthetic-probes.yml):

| Probe | Mutating? | Checks |
|---|---|---|
| `balance` | no | Vault read returns a well-formed response |
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

Four dashboards in the **SLO** folder, generated by
[`scripts/build_slo_dashboards.py`](../../scripts/build_slo_dashboards.py) and
committed. CI fails if the committed JSON differs from what the generator
produces.

| Dashboard | UID | Covers |
|---|---|---|
| SLO — API availability | `nester-slo-api` | Availability, status classes, slow routes, dependency health |
| SLO — Deposits and withdrawals | `nester-slo-flow` | Both flows' success and latency, outcomes, Soroban health |
| SLO — Balance freshness | `nester-slo-balance` | Data staleness vs budget, indexer lag, sample age, sample errors |
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
raw error string, request path, request ID.

Every label added by this work is a closed constant set fixed at compile time:

| Label | Values | Bound |
|---|---|---|
| `flow` | deposit, withdrawal | 2 |
| `outcome` (flow) | succeeded, rejected, cancelled, failed_chain, failed_internal | 5 |
| `probe` | balance, deposit, withdrawal | 3 |
| `probe reason` | ok, timeout, connection, http_4xx, http_5xx, bad_payload, assertion, unknown | 8 |

Total added series is bounded at roughly `2×5 + 2×5×11 buckets + gauges`
— a few hundred, fixed, and unable to grow with traffic or be influenced by a
caller.

**Specific protections:**

- Flow outcome classification maps errors to a fixed enum. An unclassified
  error becomes `failed_internal`, never its message.
- Probe failure detail is truncated to 200 characters and goes to the log only.
  A test asserts no probe label falls outside `{probe, reason, environment}`.
- No amount appears anywhere in metrics or alert annotations.

**Alert-driven cardinality:** every alert expression aggregates with `sum()` or
`max()` before comparison, so a malformed metric cannot produce a per-series
alert storm. Alertmanager groups by `(slo, burn, service)`, all bounded.

**Monitoring failures do not break the application:**

- `RecordFlowAttempt` is nil-safe; a service constructed without metrics
  behaves exactly as before.
- Indexer lag sampling is inside the existing loop with a nil-checked recorder;
  it cannot stop the indexer from indexing, and a failed sample does not abort
  the poll that follows it.
- `freshness.Tracker` is nil-safe on every path, so an API wired without an
  indexer serves requests unannotated rather than panicking in the request
  path.
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
   there is no 28-day history of these SLIs yet — much of the telemetry is
   introduced by this change. The first review after 28 days of data is
   the point at which they can be validated, and some will likely move.

3. **Synthetic probes need staging secrets before they run.** The workflow
   skips cleanly when `STAGING_PROBE_API_BASE_URL` is unset, which is its state
   on merge. Mutating probes need `STAGING_PROBE_ALLOW_MUTATIONS` set
   deliberately for the staging environment. Until then, probe coverage is
   zero and `SyntheticProbeMetricsAbsent` will say so.

4. **Alertmanager receivers are empty.** Routing policy is defined and testable,
   but no destination is configured. Connecting a real one is a deployment
   task, not a repository change.

5. **A bug worth recording.** During development the alert tests caught
   `sum(all) - sum(4xx)` evaluating to an *empty vector* when a service had
   served no 4xx at all — which deleted the API availability SLI entirely, so
   the alerts could never fire and would have stayed silent through a later 5xx
   outage. Fixed with `or vector(0)` and covered by two regression tests. It is
   noted here because it is the exact class of failure this document claims to
   prevent, and it was found by testing rather than by reasoning.
