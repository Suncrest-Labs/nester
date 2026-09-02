# Data retention and deletion policy

nester#1226. Establishes, per data category: how long it's kept, whether
it's exempt from deletion (and why), and how deletion is enforced.

## Why this exists

The schema accumulates per-user history with no expiry: `activity_events`,
`nudge_dispatch_log`, `nudge_outcomes`, `audit_logs`, `processed_events`,
and performance snapshots. Nothing in the tree deleted any of it before this
policy, and there was no account-deletion path.

The problem that follows from that is operational: tables that only grow
eventually dominate backup and restore times, and a retention decision is
much cheaper to design now than to retrofit under a deadline.

Audit logs deliberately should not be deletable on request — that is a
reason to write the policy down, not a reason to have none.

## Scope of this baseline

This document states the policy for every category below. Enforcement — an
actual scheduled deletion job — is implemented for the two categories marked
**Enforced** in the table: `activity_events` and `nudge_dispatch_log` (whose
cascade removes `nudge_outcomes` alongside it). Both are ordinary operational
event logs with no legal retention requirement and no account-deletion
interaction to reason about, which makes them the safe, low-risk place to
land the first real enforcement mechanism (`internal/scheduler/data_retention.go`
— see that file's doc comment for the mechanics).

The remaining categories are genuinely harder — a user-initiated
account-deletion path and backup
retention are cross-cutting concerns each substantial enough to warrant their
own implementation and its own review, not something to bolt onto a
first-pass retention sweep. Marking a policy **Stated, not yet enforced**
below is a real status, not a placeholder — it means the decision has been
made and written down, and the next PR that enforces it inherits an actual
answer to work from instead of having to make the call under a deadline.

## Policy by category

| Category | Table(s) | Retention | Deletion mechanism | Status |
|---|---|---|---|---|
| Activity events | `activity_events` | 180 days | Scheduled hard delete | **Enforced** |
| Nudge dispatch history | `nudge_dispatch_log`, `nudge_outcomes` (cascades) | 180 days | Scheduled hard delete | **Enforced** |
| Audit logs | `audit_logs` | Indefinite | None — exempt by design | Exempt (see below) |
| Processed chain events | `processed_events` | 90 days | Not yet implemented | Stated, not yet enforced |
| Performance snapshots | performance snapshot tables | 2 years | Not yet implemented | Stated, not yet enforced |
| Account deletion (user-initiated) | cross-table | N/A — see below | Not yet implemented | Stated, not yet enforced |

### Activity events — 180 days, enforced

`activity_events` (migration 071) records `login`/`deposit` timing events per
user, read by `usersignal.HeuristicTimingProvider` to infer behavioral
signals for nudging. 180 days is long enough to observe a full seasonal cycle
of user behavior (the nudge engine's own effectiveness measurement window is
90 days — see below — so 180 days gives it two full windows of headroom
before the underlying event history disappears out from under a live
measurement). Enforced by `DataRetentionJob.sweepActivityEvents` in
`internal/scheduler/data_retention.go`, a straightforward `DELETE ... WHERE
occurred_at < cutoff` — there is no soft-delete or recovery window for this
category, since a login/deposit timing event has no user-facing recovery use
case the way a savings goal does.

### Nudge dispatch history — 180 days, enforced

`nudge_dispatch_log` and `nudge_outcomes` (migration 072) record every nudge
sent and its measured outcome. `nudge_outcomes.dispatch_id` is `ON DELETE
CASCADE` from `nudge_dispatch_log`, so deleting a dispatch row removes its
outcome row automatically — the retention job only issues one `DELETE`
statement for this whole category.

**180 days is a hard floor, not just a target**: `NudgeHistoryRepository
.GetEffectivenessStats` (`internal/repository/postgres/nudge_history_repository.go`)
reads dispatch/outcome rows back 90 days (`effectivenessWindow`) to rank
which nudge types actually convert. Retention shorter than 90 days would
silently starve that ranking of data — not error, just quietly degrade to
"no data" for older cohorts — which is a much worse failure mode than a
slightly larger table. 180 days keeps a full 90-day margin past that
constraint. If `effectivenessWindow` is ever widened, this retention period
must move with it (both are documented at each other's definition site).

### Audit logs — indefinite, exempt by design

`audit_logs` (migration 011) is the record of who changed what, when — the
same table `DataRetentionJob` itself writes to when it deletes something else
(see "Deletion is itself audit-logged" below). An audit trail that can be
shortened by the same actors it's meant to hold accountable is not an audit
trail; the whole point is that it survives the events it describes. This
table is therefore explicitly out of scope for any deletion job, now or in
any planned future work, and that exemption itself is the policy decision
this document exists to state rather than leave implicit.

If a legal retention limit is ever imposed on audit logs specifically (e.g. a
statutory maximum, as opposed to a self-imposed minimum), that would be a
distinct, deliberate policy change requiring its own review — not something
a generic retention sweep should be trusted to apply.

### Processed chain events — 90 days, not yet enforced

`processed_events` (migration 028) is an idempotency/dedup ledger for
Stellar events already processed by the indexer, keyed on `event_id`. It
carries no personal data — `ledger_sequence` and `processed_at` only — so
its retention concern is purely storage growth, not privacy. 90 days is
proposed because that comfortably exceeds any plausible reorg-recovery or
backfill replay window this system would need to re-derive idempotency
against; if `internal/stellar/backfill.go`'s replay window is ever
documented with a different bound, this number should be reconciled against
it before enforcement ships. Not enforced in this baseline because it is a
distinct concern from the two categories above (dedup correctness during a
live backfill, not user data hygiene) and deserves its own review against
the indexer's actual replay behavior rather than reusing this job's generic
shape.

### Performance snapshots — 2 years, not yet enforced

Vault performance snapshot history backs the `/vaults/{id}/performance`
APY/history endpoints (`internal/service/performance`). 2 years is proposed
to preserve a meaningful multi-year APY history for user-facing charts and
year-over-year comparisons — a much longer window than the operational logs
above, because this data is directly displayed to users as their own
investment history, not just an internal signal. Not enforced here because
snapshot retention interacts with the timeseries rollup job
(`internal/timeseries/rollup_job.go`, which already deletes raw
high-resolution points after `RawRetention` — currently 30 days by default —
while keeping rolled-up minute/hour/day aggregates) and needs its cutoff
reconciled against that existing mechanism rather than introduced as an
independent, possibly-conflicting deletion path.

### Account deletion (user-initiated) — not yet enforced

There is currently no account-deletion path in this codebase at all — a user
cannot request their account be closed and their data removed. This is a
larger feature than a retention sweep (it touches auth, sessions, vault
balances that must be settled or transferred before an account can close,
and every category above needs its own deletion-vs-anonymisation answer
applied in the context of "this whole account is going away," not just "this
one row aged out"). Flagged here as the gap it is rather than silently
absent from the policy.

## Deletion is itself audit-logged

Every deletion `DataRetentionJob` performs writes an `audit_logs` entry
(`action: "data_retention.delete"`, `entity_type`: the table name,
`new_value: {deleted_count, cutoff}`) — see `DataRetentionJob.audit` in
`internal/scheduler/data_retention.go`. No per-row entity IDs: a single sweep
can remove thousands of rows, and the audit trail's job here is "prove a
sweep ran, when, and how much it removed," not reconstruct which individual
rows were affected — the deleted data itself is gone by design, and the
count + cutoff is what an operator reviewing this trail actually needs.

## Enforcement mechanism (for the two enforced categories)

`internal/scheduler/data_retention.go`'s `DataRetentionJob` runs once daily,
leader-elected (mirrors `SavingsGoalPurgeJob`'s pattern — running the sweep
from every replica is wasteful but not unsafe, since each `DELETE` is
idempotent). Each table's sweep is independent: one table's delete failing
does not block the other's, and does not stop the job from retrying on its
next daily tick.

Wired in `cmd/api/main.go` alongside the other leader-elected sweep jobs.
