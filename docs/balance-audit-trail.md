# Balance audit trail (nester#1124)

Every balance-changing vault operation (deposit, withdrawal, harvest) appends
one row to `balance_audit_log` (migration `apps/api/migrations/107_create_balance_audit_log.up.sql`):

| column           | meaning                                              |
|------------------|-------------------------------------------------------|
| `actor`          | who caused the change — a user id, or `system:<job>` for a background job (e.g. `system:harvest`) |
| `operation`      | `deposit` / `withdrawal` / `harvest` / `rebalance_*` / `emergency_withdraw` |
| `amount`         | magnitude of the change                                |
| `balance_before` | vault `current_balance` immediately before             |
| `balance_after`  | vault `current_balance` immediately after              |
| `chain_reference`| on-chain transaction hash, when one exists              |
| `created_at`     | when the row was written                                |

## Append-only

No application code path issues `UPDATE` or `DELETE` against this table.
`internal/domain/balanceaudit.Repository` only declares `Append`, `ListByVault`,
and `ListByUser` — there is no update/delete method to call. The Postgres
implementation (`internal/repository/postgres/balance_audit_repository.go`)
mirrors that: it has no such methods either.

## Reconstructing history and reconciling

`ListByVault` / `ListByUser` return entries oldest-first. Summing
`balance_after - balance_before` across the whole ledger
(`balanceaudit.Reconcile`) reproduces the balance implied by every recorded
change from zero. If that total disagrees with the vault's live
`current_balance`, an out-of-band mutation happened that the ledger doesn't
account for — the trigger to investigate.

## Retention

Rows are kept indefinitely. Growth is one row per deposit/withdrawal/harvest
leg — the same order of magnitude as `vault_transactions`, which the system
already sustains without a purge job. No retention/rollup job exists yet;
revisit if/when table size becomes an operational concern.

## What writes it

`VaultService` (in `apps/api/internal/service/vault_service.go`) writes the
audit entry as part of the same database transaction as the balance
mutation, via `postgres.VaultRepository`'s `RecordDepositWithAudit` /
`RecordWithdrawalWithAudit` / `RecordHarvestWithAudit`: the balance change
and its audit entry commit together, or neither commits. A failed audit
append rolls back the deposit/withdrawal/harvest with it, so the ledger can
never silently fall behind the balance it describes.

(`VaultService.recordBalanceAudit` — the older, best-effort, append-after-commit
path — still exists as a fallback for repository implementations, such as
test doubles, that don't implement the transactional `*WithAudit` methods;
the production Postgres repository always takes the atomic path above.)

## Launch caps and concurrent deposits

The per-user and global launch caps (nester#1119) are enforced inside the
same transaction as the deposit's balance credit and audit append —
`RecordDepositWithAudit` re-reads both totals under a Postgres advisory
transaction lock (keyed per-user, and a fixed key for the global cap) before
crediting, so two concurrent deposits can never both pass the check and
collectively push a total over its cap. (An earlier, non-transactional
`CheckDeposit` path remains for callers that only need a fast, non-atomic
pre-check, e.g. to fail a request before it ever reaches the chain.)
