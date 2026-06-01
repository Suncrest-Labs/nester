# Rebalancing Scheduler (#372)

Background scheduler that evaluates each active vault on a configurable
interval and triggers an on-chain rebalance when the expected APY gain
(in basis points) exceeds the configured threshold.

## What this package contains

- **`decision.go`** — `Decide(DecisionInput) Decision`. Pure logic.
  Decides whether to rebalance from a snapshot of the vault's current
  allocations and the latest per-protocol APYs. No side effects. The
  acceptance criterion in the issue (*"Unit tests for the rebalancing
  decision logic — no contract calls needed, mock the yield fetcher"*)
  is what `decision_test.go` exercises.
- **`scheduler.go`** — the loop driver. Wires three interfaces
  (`VaultFetcher`, `YieldFetcher`, `RebalanceSubmitter`) around the
  pure decision core; runs the loop on `Config.Interval`; exposes
  `TriggerOnce(ctx, vaultID)` for the admin endpoint.
- **`scheduler_test.go`** — integration tests with recording fakes
  that exercise the loop's tick → decide → submit path, the circuit-
  breaker tolerance, and the per-vault filter for the admin endpoint.

## Wiring (deferred to a follow-up)

The `cmd/api/main.go` wiring + concrete `RebalanceSubmitter` (calling
`internal/service/soroban_vault_chain_invoker.go`) + the admin handler
`POST /api/v1/admin/vaults/{id}/rebalance` are intentionally **not**
in this PR. They are the next step:

1. In `main.go`, after the existing vault + chain invoker wiring:
   ```go
   schedCfg := scheduler.Config{
       Enabled:       cfg.Rebalancer().Enabled(),
       Interval:      cfg.Rebalancer().Interval(),
       MinAPYGainBPS: cfg.Rebalancer().MinAPYGainBPS(),
   }
   sched := scheduler.New(schedCfg, vaultLister, yieldFetcher, chainSubmitter, baseLogger)
   go sched.Run(ctx)
   ```
2. Add `REBALANCER_*` parsing to `internal/config/config.go`.
3. Add `POST /api/v1/admin/vaults/{id}/rebalance` to `AdminHandler` that
   calls `sched.TriggerOnce(ctx, id)`.

Keeping the wiring out of this PR means we can land + iterate the
decision logic and loop driver without touching `main.go`'s bootstrap
graph, which is the riskiest surface to change blind.

## Configuration

| Env var                       | Type     | Default | Description                                          |
| ----------------------------- | -------- | ------- | ---------------------------------------------------- |
| `REBALANCER_ENABLED`          | bool     | `false` | Toggle the loop. `false` means `Run` returns at once |
| `REBALANCER_INTERVAL_MINUTES` | int      | `15`    | Tick interval                                        |
| `REBALANCER_MIN_APY_GAIN_BPS` | int      | `50`    | Skip rebalance unless expected gain exceeds this     |

## Circuit-breaker semantics

A `RebalanceSubmitter` that returns `scheduler.ErrCircuitBreakerTriggered`
signals the vault contract's built-in safety brake fired (e.g. >20%
withdrawal in the last 2 hours). The loop logs at INFO and continues
with the next vault; the next tick re-evaluates.
