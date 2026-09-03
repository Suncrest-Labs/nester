# Predictive protocol-health deterioration monitoring

`ProtocolHealthChecker` (`internal/scheduler/protocol_health_checker.go`) has
always run a fixed rule: alert when a protocol's TVL drops more than 20% in
24 hours. That reacts to a collapse already underway. The deterioration
engine (`internal/scheduler/deterioration_*.go`) adds prediction: a
continuous, graduated score computed from leading indicators, independent of
and running alongside the fixed 24h/20% check.

## Indicators

Computed in `ComputeIndicators` (`deterioration_indicators.go`) from data
already ingested — DeFiLlama TVL/APY snapshots (`protocoltvl`, `apysnapshot`)
— no new external data source required:

- **TVL outflow velocity** — percentage decline from the first to the last
  snapshot in the window.
- **APY abnormality** — z-score of the latest APY reading against the
  window's own mean/stddev. A large absolute z-score in either direction
  (spike or collapse) is abnormal.
- **Reported-vs-derived APY gap** — the latest reading compared against a
  short trailing baseline of the same series. This is a proxy: a true
  on-chain-derived accrual figure requires an oracle-aggregation layer that
  has not landed as a separate piece of work. Widening divergence from a
  protocol's own recent trend is the real, computable signal this stands in
  for.
- **Price instability** — coefficient of variation (stddev/mean) of the TVL
  series, standing in for underlying/LP token price instability where a
  direct per-protocol price feed isn't available.

Each indicator is independently interpretable — this is what makes an alert
explainable rather than an opaque number.

## Scoring

`Score` (`deterioration_score.go`) is a deliberately simple, fully
transparent weighted-logistic model: each indicator is normalized to
roughly [0,1], weighted, summed, and passed through a logistic squash to
produce a probability. Every weight is a named constant; nothing is opaque.
TVL outflow carries the heaviest weight (the issue's own framing: "smart
money leaving early" is the strongest tell), APY abnormality and the
reported/derived gap are strong corroborating signals, and price instability
is a weaker, noisier one.

Thresholds (`ThresholdMild` / `ThresholdModerate` / `ThresholdSevere`) are
calibrated so a single strong indicator alone lands at most in "moderate";
reaching "severe" — which can trigger automatic capital movement — requires
multiple corroborating signals. A thin sample (fewer than
`minSampleCountForConfidence` data points) is capped below "severe"
regardless of what it scores, so a noisy one-off reading can never alone
justify moving funds.

## Graduated action

`DeteriorationEngine.DispatchAction` (`deterioration_action.go`) maps level
to action:

| Level | Action |
|---|---|
| none | nothing |
| mild | ceiling-cut recorded (informational) |
| moderate | rebalance recommendation recorded + logged for operators |
| severe | automatic protective rebalance for every vault allocated to the protocol, via the existing slippage-safe `AdminService.TriggerRebalance` |

Every action — including a failed automatic-rebalance attempt — is written
to `deterioration_actions` before/alongside any notification. Nothing here
moves funds through any path other than the existing, audited rebalance
mechanism.

## Calibration review

Every assessment (not just ones that cross a threshold) is recorded to
`deterioration_assessments`, so a probability can later be checked against
what actually happened to the protocol — the calibration-validation loop the
issue asks for.
