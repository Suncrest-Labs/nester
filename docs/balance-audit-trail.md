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

`VaultService` (in `apps/api/internal/service/vault_service.go`) calls an
optional `BalanceAuditRecorder` after each successful balance change. The
write is best-effort: a failure to append is logged loudly but never blocks
or rolls back the underlying deposit/withdrawal/harvest, since by the time
the append runs the balance change itself is already durably committed.
