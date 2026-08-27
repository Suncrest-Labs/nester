# SLO review cadence

Implements the review cadence required by
[nester#1056](https://github.com/Suncrest-Labs/nester/issues/1056).

An SLO nobody reviews is decoration. Reviewing them is what surfaces targets
that were set wrong — and every target in [slo.md](slo.md) was set from
reasoning about deployment topology and user impact rather than from observed
history, because five of the nine SLIs did not exist before this change. Some of
them are wrong. The review is how that gets found.

**Status of the first review:** scheduled, **not yet held**. See
[First review](#first-review). No review has taken place as of this change, and
nothing in this document should be read as reporting the outcome of one.

---

## Cadence

**Monthly**, aligned to the 28-day rolling window so each review covers a full
window without overlap.

Monthly rather than weekly because a 28-day budget barely moves in a week, and a
review with nothing to decide teaches people to skip it. Monthly rather than
quarterly because a target that is wrong stays wrong for a quarter, and a
budget trending toward exhaustion needs to be seen before it arrives.

An **ad-hoc review** is convened when any SLO enters the exhausted state, rather
than waiting for the next scheduled one.

---

## Owner

The **repository maintainer group** (CODEOWNERS) owns the review.

One maintainer is the **review owner** for each session: they prepare the
artifact, run the session, and record the outcome. The role rotates so that
familiarity with the SLO stack does not concentrate in one person — the SLOs are
most useful to whoever is on-call, which is not always the same individual.

---

## Participants

**Required:**

- The review owner.
- At least one maintainer other than the review owner.
- Whoever held on-call during the period under review. They saw which alerts
  fired, which were useful, and which were noise — that is the single most
  valuable input, and a review without it is guessing about alert quality.

**Optional but useful:**

- Anyone who worked an incident during the period.
- Anyone proposing a target change.

The review can be asynchronous — a written artifact with comments and explicit
sign-off — provided the required participants respond. A distributed
contributor base makes a synchronous meeting a poor requirement, and an
asynchronous review that actually happens is worth more than a synchronous one
that keeps slipping.

---

## What is reviewed

The review owner prepares an artifact from the
[checklist](#review-artifact-template) before the session. It is not a
discussion of whatever comes to mind.

### 1. Attainment and budget, per SLO

For every SLO with a budget: 28-day attainment, budget remaining, and the
direction of travel since last review.

Read from the first panel row of each SLO dashboard.

### 2. Incidents and consumption

What consumed budget this period. Each significant consumption event linked to
its alert, dashboard snapshot, or postmortem.

The question is not only "what broke" but "did the SLO reflect the user impact"
— an incident users noticed that barely moved a budget means the SLI is
measuring the wrong thing.

### 3. Alert quality

Per alert that fired:

- Was it actionable?
- Did the runbook lead to the cause?
- Did it fire at the right severity?

And the question that only on-call can answer: **did anything break that no
alert caught?** A missed incident is a stronger signal than any firing alert.

False pages and alerts that fired without a corresponding user-visible problem
are recorded explicitly. An alert that pages without being actionable is worse
than no alert, because it erodes trust in every other alert.

### 4. Target appropriateness

For each SLO:

- Is the target consistently met with room to spare? It may be too loose to be
  informative.
- Is it consistently missed? Either the service needs work or the target is
  unachievable with the current topology.
- Does attainment match user experience? Support volume and user reports are
  the check on whether the SLI measures what it claims.

### 5. Error-budget policy effectiveness

- Did any SLO enter warning or exhausted? Was the policy followed?
- Every override since the last review: was the reason genuine in hindsight?
  Did the change worsen the burn?
- **Three or more overrides for one SLO within two cycles** is a finding in its
  own right: either the target is wrong or the policy is unworkable, and the
  review must decide which rather than continuing to grant exceptions.
- Did the policy prevent anything worth preventing, or only create friction?

### 6. Synthetic probe coverage

- Did the probes run? (`SyntheticProbeStale` firing means they did not.)
- Did they catch anything real traffic missed? That is their entire purpose.
- Is a flow now worth probing that is not covered?

### 7. Cardinality and cost

Series count trend from the Prometheus target-status page. New labels since
last review, and whether each is bounded.

Included because cardinality growth is silent until it takes the metrics
backend down, at which point every SLO goes blind simultaneously.

---

## How changes are proposed

### Target changes

1. Open an issue titled `slo: adjust <SLI> target from X to Y`.
2. Include: observed attainment over at least two full 28-day windows, the user
   impact argument for the new number, and why the current one is wrong.
3. Discuss at the review. A target change needs agreement from two maintainers.
4. On agreement, one PR updates: the threshold in `slo_alerts.yml`, the expected
   values in `slo_alerts_test.yml`, the tables in `slo.md`, and the dashboard
   generator if a displayed threshold changes.

CI runs the alert tests against the real rule files, so a threshold changed
without updating its arithmetic fails the build. That is the mechanism enforcing
step 4 — the tables cannot silently drift from the rules.

**Loosening a target requires an explicit user-impact argument**, not "we keep
missing it". Missing a target is information about the service; loosening it to
stop the alerts firing discards that information and is how an SLO becomes
decoration.

**A target should not be changed on one period's data.** Two full windows
minimum, so a single bad month does not permanently loosen a target.

### Alert tuning

Threshold, window, and severity changes follow the same route but need only one
maintainer's agreement, since they change detection rather than the commitment.

Any tuning that makes an alert **less** sensitive states what would still be
caught and what would now be missed.

### Runbook changes

No review needed. If on-call found a runbook step wrong or missing, fix it
immediately — a runbook is most accurate right after someone has used it in
anger. Runbook corrections are reported at the next review as a signal about
which subsystems are least understood.

---

## Review artifact template

The review owner copies this into an issue titled
`SLO review: YYYY-MM-DD`, fills it in, and links it from the next review.

```markdown
# SLO review — YYYY-MM-DD

Period: YYYY-MM-DD to YYYY-MM-DD (28 days)
Review owner:
Participants:
On-call during period:

## 1. Attainment

| SLO | Target | Attainment | Budget remaining | Trend | State |
|---|---|---|---|---|---|
| API availability | 99.9% | | | | |
| Deposit success | 99.5% | | | | |
| Withdrawal success | 99.5% | | | | |
| Flow latency | 99% <30s | | | | |
| Intelligence availability | 99% | | | | |
| Intelligence TTFT | 95% <3s | | | | |
| Balance freshness | ≤60 ledgers | n/a | n/a | | |
| Intelligence refusal | <30% | n/a | n/a | | |

## 2. Incidents

| Date | SLO(s) | Budget consumed | Cause | Postmortem |
|---|---|---|---|---|

## 3. Alert quality

| Alert | Times fired | Actionable? | Runbook led to cause? | Right severity? |
|---|---|---|---|---|

Missed incidents (no alert fired, but something broke):

False pages:

## 4. Target appropriateness

Per SLO: keep / propose change, with reasoning.

## 5. Error-budget policy

States entered:
Overrides ( `gh pr list --label error-budget-override` ):
Was the policy followed?
Findings:

## 6. Synthetic probes

Ran as scheduled:
Caught anything real traffic missed:
Coverage gaps:

## 7. Cardinality

Series count (this period / last):
New labels, and whether bounded:

## Actions

| Action | Owner | Due |
|---|---|---|

## Next review
```

---

## First review

**Scheduled for 28 days after this change merges**, so it has a full rolling
window of real data rather than a partial one.

**It has not been held.** This section describes what is planned, not what
happened.

Its agenda differs from a steady-state review, because it is the first
opportunity to check reasoning against reality:

1. **Recalibrate the intelligence refusal threshold.** The 30% figure in
   `slo.md` is an estimate, not a measurement. This is the first agenda item
   because it is the least defensible number in the whole configuration.

2. **Validate every target against observed data.** All were set from reasoning
   about topology and user impact. Some will be wrong; the review is where that
   is established rather than assumed.

3. **Confirm the burn-rate thresholds produce useful alerts.** Specifically
   whether fast-burn pages were actionable and whether slow-burn tickets were
   worth filing.

4. **Adopt or amend the error-budget policy.** It is a proposal until the
   maintainers agree to it, including the 50% / 25% / 0% state thresholds.

5. **Confirm the synthetic probes are configured and running.** Probe coverage
   is zero until the staging secrets are set; if they are still unset at the
   first review, that is the finding.

6. **Review the SLI definitions themselves**, not only the targets. Did any
   exclusion turn out to hide a real failure? The `failed_chain` decision — not
   excusing Soroban failures — is the one most likely to be argued, and this is
   where that argument should happen.

To schedule it, open the artifact issue at merge time with the target date, so
the review is on the tracker rather than depending on someone remembering.
