# Database session timeouts

`statement_timeout`, `lock_timeout` and `idle_in_transaction_session_timeout`
are enforced on the PostgreSQL server for **every** connection in the API's
connection pool. They are sent in the connection startup packet (via pgx
`RuntimeParams`), so they apply to connections created at any time, not just
the initial pool fill. A slow or lock-waiting query is cancelled by the
database instead of holding a connection until the pool is exhausted.

## Environment variables

| Variable | Default (dev/test) | Default (mainnet) | Disabled |
|---|---|---|---|
| `DATABASE_STATEMENT_TIMEOUT` | `30s` | `10s` | `0` |
| `DATABASE_LOCK_TIMEOUT` | `5s` | `2s` | `0` |
| `DATABASE_IDLE_IN_TRANSACTION_SESSION_TIMEOUT` | `60s` | `30s` | `0` |

- The default depends on `STELLAR_NETWORK_PASSPHRASE`: on mainnet the stricter
  column applies, because a stuck query on mainnet is an availability incident
  (launch load on the ledger tables, nester#1220 context).
- `0` disables the corresponding server-side timeout. This is documented as an
  escape hatch for a **reviewed migration or backfill job**, where a
  statement may legitimately run longer than the API's bounds. Do not run the
  API itself with disabled timeouts.
- Any other value overrides the default for that parameter only.

## Migrations and backfill

`db.MigrateUp` / `db.MigrateDown` execute `SET LOCAL statement_timeout = 0;
SET LOCAL lock_timeout = 0; SET LOCAL idle_in_transaction_session_timeout = 0`
at the start of each migration transaction, so schema changes are not
bounded by the API defaults. `SET LOCAL` is scoped to the transaction: every
other connection keeps its configured values.

The startup auto-migration path in `cmd/api` uses the same pool, so a
long-running migration job should set `DATABASE_STATEMENT_TIMEOUT=0`
(and the other two if needed) for that process only.

## Tests

- `apps/api/internal/db/db_integration_test.go` — `TestStatementTimeoutCancelsQuery`
  proves a `pg_sleep` longer than the configured `statement_timeout` is
  cancelled by PostgreSQL (SQLSTATE 57014). Gated on `TEST_DATABASE_DSN`
  (build tag `integration`).
- `apps/api/internal/db/db_pool_timeout_test.go` — `TestApplySessionTimeouts`
  unit-tests default / mainnet-default / explicit / disabled resolution
  without a database.
