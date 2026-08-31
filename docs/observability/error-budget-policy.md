# Error budget policy

Implements the error-budget policy required by
[nester#1056](https://github.com/Suncrest-Labs/nester/issues/1056).

This is an engineering agreement about what happens when reliability degrades.
Its value comes from being agreed **before** it is needed — when nobody is
under pressure, no launch is at stake, and no one is arguing from a position.
A policy written during an incident is a negotiation, not a policy.

**Status:** proposed. This document is written and reviewable, but it is not
in force until the maintainers adopt it — see [Adoption](#adoption). Nothing
below describes a decision that has already been made or a meeting that has
already happened.

---

## Scope

Applies to every SLO with an error budget in [slo.md](slo.md):

- API availability (99.9%)
- Deposit success (99.5%)
- Withdrawal success (99.5%)
- Deposit/withdrawal latency (99% under 30s)
- Intelligence availability (99%)
- Intelligence TTFT (95% under 3s)

Balance freshness and intelligence refusal rate are **out of scope**: one is a
gauge and the other measures correct behaviour, so neither has a budget to
spend. They still alert, and a sustained breach of either is grounds for the
same reliability prioritisation by judgement rather than by formula.

Budget state is read from the `budget remaining` panel on each SLO dashboard,
computed from the `*_rate28d` recorded series over the rolling 28-day window.

---

## The four states

Each SLO is in exactly one state, determined by budget remaining.

| State | Budget remaining | What it means |
|---|---|---|
| **Healthy** | > 50% | Reliability is comfortably within target. |
| **Watch** | 25–50% | Materially consumed. Worth attention, not yet constraining. |
| **Warning** | 0–25% | On track to exhaust. Reliability work takes precedence. |
| **Exhausted** | ≤ 0% | Target missed over the trailing 28 days. Feature work is gated. |

Budget remaining is deliberately allowed to go negative and is not clamped at
zero. "How far past" determines how the exhausted state is handled: −5% is a
bad month, −200% is a systemic problem, and a policy that reports both as
"exhausted" loses the distinction that matters.

### Healthy — above 50%

Normal feature development. No constraint.

Reliability work is prioritised on its merits like any other work. A budget
that is never consumed is not automatically good news: it can mean the target
is set too loosely to be informative, which is a question for the SLO review
rather than for this policy.

### Watch — 25% to 50%

Feature development continues.

- The SLO is named in the next SLO review with the cause of the consumption.
- Any known reliability issue affecting that SLO gets an owner and a ticket.
- No approval gate.

This state exists to prevent the jump from "everything is fine" to "everything
is blocked" arriving as a surprise.

### Warning — 0% to 25%

Reliability work takes priority over new feature work **for the affected
service**.

- Work that reduces the burn is scheduled ahead of feature work touching the
  same service.
- Changes to that service carry a reliability justification in the PR
  description: why this change does not make the burn worse.
- Feature work not touching the affected service is unconstrained.
- The affected SLO is a standing item in every review until it recovers.

Scoped to the affected service on purpose. A withdrawal-path problem should not
block frontend work that cannot influence it; a blanket freeze produces
resentment and gets ignored, which costs more than the narrower rule does.

### Exhausted — at or below 0%

The target has been missed over the trailing 28 days.

**Gated:**

- New feature work touching the affected service pauses.
- Deploys to that service are limited to reliability fixes, security fixes, and
  rollbacks.
- Reliability work for that service becomes the team's priority until recovery.

**Not gated:**

- Security fixes. Always ship. An unpatched vulnerability is not made safer by
  a reliability freeze.
- Rollbacks and incident mitigation. These reduce the burn.
- Work on services whose budget is healthy.
- Documentation, tests, tooling, and any change that cannot affect runtime
  behaviour of the affected service.

**Required within one working day of entry:**

1. A written cause: which incidents or sustained degradation consumed the
   budget, linked to alerts, dashboards, or postmortems.
2. A named owner for recovery.
3. A remediation plan with specific work items, not "improve reliability".

The gate applies to the **affected service**, not the whole repository, for the
same reason the warning state is scoped.

---

## Recovery

An SLO leaves the exhausted state when **budget remaining returns above 0%** on
the rolling 28-day window.

Two consequences follow from the window being rolling, and both are intended:

**Recovery can happen without any fix**, as a bad day ages out of the 28-day
window. This is not a loophole. It means the incident is no longer
representative of the current 28 days, which is exactly what the rolling window
is measuring. The remediation plan does not disappear when the budget recovers;
it is tracked to completion like any other committed work.

**Recovery cannot be rushed.** No amount of good behaviour today removes
yesterday's consumption from the window. This is deliberate: it prevents
"we fixed it, unblock us" arguments during the period when the fix is least
proven.

The recovery is recorded in the next SLO review with the remediation status. An
SLO recovering by ageing out while its remediation is unfinished is called out
explicitly in that review rather than quietly closed.

---

## Overrides

The policy can be overridden. A policy with no override is a policy that gets
ignored the first time it is genuinely wrong, which destroys its authority for
every subsequent case.

### Who can override

A **repository maintainer** (see CODEOWNERS). One maintainer's approval is
sufficient.

The maintainer overriding must not be the sole author of the work being
unblocked. Self-approval converts the override from a considered exception into
a formality.

### What qualifies

An override is for cases where following the policy causes more harm than the
reliability risk it prevents:

- A regulatory or contractual deadline that cannot move.
- A change that is itself reliability-improving but formally counts as feature
  work.
- A dependency deadline outside our control — an upstream deprecation with a
  fixed date.
- Budget consumed by a cause outside our control, where no engineering work
  available to us would change the outcome.

**What does not qualify:** a launch date chosen internally, a sprint
commitment, or "this change is small". The third is the most common and the
most dangerous: small changes cause a meaningful share of incidents, and the
gate exists precisely because size does not predict risk.

### How overrides are recorded

Every override is recorded — no verbal or chat-only overrides.

Recorded as a comment on the PR being unblocked, containing:

1. Which SLO is exhausted and its budget remaining at the time.
2. Which qualifying reason applies.
3. What is being shipped and its reliability risk.
4. What mitigation is in place (feature flag, staged rollout, rollback plan).
5. The approving maintainer.

The PR is labelled `error-budget-override` so the set is enumerable:

```bash
gh pr list --repo Suncrest-Labs/nester --label error-budget-override --state all
```

### Who reviews overrides

Every override since the last review is examined at the next SLO review:

- Was the reason genuine in hindsight?
- Did the shipped change worsen the burn?
- Is a pattern forming?

**Three or more overrides for the same SLO within two review cycles is itself a
finding.** It means either the target is wrong or the policy is not workable as
written, and the review must resolve which — not keep granting exceptions.

---

## Interaction with alerting

The policy operates on the **28-day budget**; alerts operate on **burn rate**.
They answer different questions and are not interchangeable.

A fast-burn page means "this is consuming budget quickly right now" — an
incident response. It does not by itself put an SLO into a policy state; a
short sharp incident can page without meaningfully denting a 28-day budget.

Conversely an SLO can reach exhausted with no alert currently firing, from
accumulated small degradations that never crossed a burn threshold. That case
is the reason the budget is reviewed on a cadence rather than only when
something pages.

---

## Adoption

This policy is **not in force**. It takes effect when:

1. The maintainers agree to it, recorded on the pull request implementing
   #1056 or on a follow-up issue.
2. The first SLO review is held (see [slo-review.md](slo-review.md)) and the
   thresholds and override process are confirmed workable.

Until then it is a proposal. Recording that honestly matters more than
appearing complete: a policy nobody has agreed to cannot gate anyone's work,
and claiming otherwise would be the same fiction the issue warns against for
SLOs built on telemetry that does not exist.

The first 28 days after merge also produce the first real budget figures. Some
targets in [slo.md](slo.md) will likely move as a result, and the state
thresholds here (50% / 25% / 0%) are themselves a first proposal open to
adjustment at that review.
