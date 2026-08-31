# Monte Carlo Savings Forecasting (#843)

The deterministic compound-interest calculator (`model.go` /
`service.CompoundInterestCalculator`) produces a single point-estimate
timeline: it assumes the vault's current APY holds exactly, every month,
forever, and every scheduled contribution happens exactly on time and in
full. That's a reasonable "if nothing changes" number, but it can't answer
"how likely am I to actually hit my goal by its deadline?" — which needs a
*distribution* of outcomes, not one path.

This package adds that: `simulation.go` runs many independent randomized
paths over the horizon and reports the P10/P50/P90 band per month plus (given
a target amount and deadline) the fraction of paths that got there in time.
`internal/service/projection_simulation.go` wires it into
`ProjectionService.SimulateVaultProjection`, resolving the distribution
parameters from real data when it's available.

## What varies per simulated path, and why

Each of `PathCount` paths steps month by month. Two things are drawn at
random each month:

1. **Yield.** `annualYield ~ Normal(ExpectedAPY, APYStdDev)`, converted to a
   monthly rate with the same daily/monthly compounding formula the
   deterministic calculator uses (`monthlyRateFromAnnual` mirrors
   `CompoundInterestCalculator`'s conversion, which is what makes a
   zero-volatility Monte Carlo run collapse exactly onto the deterministic
   numbers — see the "Zero-volatility" test below). A yield draw is floored
   at -100% (a single period can't lose more than the balance itself in this
   model).
2. **Contribution behavior.** Each month (other than the first), the
   scheduled contribution is skipped with probability
   `ContributionSkipProbability`; if it isn't skipped, its size is varied
   uniformly on `[1-ContributionVariationPct, 1+ContributionVariationPct] *
   MonthlyContribution`. The skip-check and variation draw are made every
   month *regardless of the contribution's size*, so the random stream
   consumed by a path is identical no matter what `MonthlyContribution` is —
   this is what makes the sensitivity-grid deposit-monotonicity guarantee
   below exact rather than approximate.

Both distributions are Normal/Uniform, not because savings behavior is
provably Gaussian, but because (a) they're fully described by two
interpretable parameters (mean/stddev, or a probability + a range) that map
directly onto data we actually have, and (b) they're the standard
maximum-entropy choice absent a more specific model. This is stated
explicitly here per the issue's "document every distributional assumption"
acceptance criterion, rather than left implicit in the code.

## Yield volatility: grounded in real historical APY

`ProjectionService.SimulateVaultProjection` resolves `ExpectedAPY` /
`APYStdDev` like this, in order:

1. If `VaultID` is supplied: pull up to 90 days of daily-bucketed realized
   APY via `performance.SnapshotRepository.APYHistoryForVault` and compute
   the sample mean/stddev with `MeanStdDev`. Used only when there are at
   least `minHistoricalAPYSamples` (5) daily buckets — fewer than that and a
   "standard deviation" is mostly noise. `VolatilitySource` is reported as
   `"historical"` in this case.
2. Otherwise (new vault, too few snapshots, or no vault at all but an
   explicit `APY` override was given): mean comes from
   `APYStatsForVault`'s average (if a vault was given) or the caller's
   explicit `APY` override or, failing both, the same 5% fallback
   `CalculateVaultProjection` already uses when no APY data exists at all.
   Standard deviation is a documented prior: **25% of the mean APY**
   (`defaultAPYStdDevFraction`). This is deliberately wide — an unproven
   vault or a caller-supplied hypothetical should read as uncertain, not
   falsely precise. `VolatilitySource` is reported as `"default_prior"`.

An explicit `APY` override in the request always wins for the *mean*
(matching `CalculateVaultProjection`'s existing `APYOverride` precedence);
only the *stddev* falls back to the historical figure when both a vault and
an override are present, since we still have real volatility data even if
the caller wants to override the central estimate.

`internal/forecast.Predict` (issue #118) was evaluated as an alternative
source of yield drift/confidence, but it's a *next-period point forecast*
(with a TVL-trend/policy-rate adjustment), not a distribution of monthly
outcomes over a multi-year horizon — using its confidence band as a stand-in
for month-by-month volatility would conflate "how sure are we about next
month" with "how much does this vault's realized APY actually bounce around
month to month". The two are complementary (a savings-calculator UI could
show both) but simulation.go uses `APYHistoryForVault`/`APYStatsForVault`
directly as the more faithful source for *this* purpose.

## Contribution behavior: grounded in the user's own schedule

`MonthlyContribution`, `ContributionSkipProbability`, and
`ContributionVariationPct` are resolved like this:

1. If `GoalID` is supplied and the user has an active
   `savingsschedule.SavingsSchedule` for that goal
   (`Repository.GetActiveByGoal`): the schedule's `Amount` is converted to a
   monthly-equivalent (weekly × 52/12, biweekly × 26/12, monthly × 1) and
   used as `MonthlyContribution`. Skip probability is **5%**
   (`scheduledContributionSkipProbability`) and size variation is **10%**
   (`scheduledContributionVariationPct`) — tighter than the new-user prior
   below, because opting into an automated recurring deposit is a real,
   observable commitment signal, but not zero, because real schedules still
   miss occasional runs (insufficient balance, a cancelled funding source, a
   manual pause). `ContributionSource` is reported as `"schedule"`.
2. Otherwise (no goal, or a goal with no active schedule — i.e. a new user
   who hasn't set up automated saving yet, or is only exploring "what if"
   numbers): the caller's `MonthlyContribution` is used as-is, with skip
   probability **12%** (`newUserContributionSkipProbability`) and size
   variation **20%** (`newUserContributionVariationPct`). There is no
   first-party historical distribution to draw a new user's behavior from,
   so this is a documented prior rather than a computed one: 12% sits in the
   middle of the 10-15% range called out in the issue, based on typical
   first-90-day skip/lapse rates for comparable recurring-consumer-savings
   products (e.g. round-up/auto-save apps). `ContributionSource` is reported
   as `"default_prior"` so a caller/UI can see which regime produced the
   band.

## Goal success probability and the deposit/deadline sensitivity grid

When both a target amount and a deadline are known (explicit
`TargetAmount`/`DeadlineMonths` on the request, or resolved from the goal via
`GoalID`), `GoalSuccessProbability` is the fraction of the `PathCount`
simulated paths whose balance reached the target by the deadline month
(inclusive).

`SensitivityGrid` re-runs the simulation across a small grid of
monthly-contribution deltas (±20%/±40% of the base contribution) crossed
with deadline deltas (±6 months), reusing the *same* `Seed` for every cell —
a "common random numbers" variance-reduction technique. Because every path's
random stream (yield draw, skip check, variation draw) is identical across
cells regardless of contribution size, and a yield multiplier is never ≤
-100%, increasing the contribution can only ever add to a path's running
balance at every month. That makes "more deposit never lowers success
probability" (for a fixed deadline) an **exact structural guarantee of this
implementation**, not a statistical tendency of the sampling — see
`TestSensitivityGrid_MoreDepositNeverLowersSuccessProbability` in
`calculator_test.go`.

## Path count

`DefaultPathCount = 3000`, bounded to `[MinPathCount=500, MaxPathCount=5000]`
by caller request. 3000 paths keeps P10/P50/P90 stable to the cent across
repeated runs with the same seed for typical horizons (see
`TestRunMonteCarloSimulation_DeterministicGivenSeed`) while completing in low
single-digit milliseconds — which is what keeps this usable **inline**, as
required by the issue, rather than needing a background job queue.

## Determinism and caching

`RunMonteCarloSimulation` is a pure function of `MonteCarloParams` — no
repositories, no context, no clock — so an identical `Seed` (and identical
other params) always produces bit-for-bit identical output.
`DeriveSeed(parts ...string)` hashes stable request inputs (vault/goal id,
amounts, resolved APY mean/stddev, period, compound frequency, skip/variation
probabilities, target/deadline) with SHA-256 into a seed, so two requests
with the same *effective* inputs always get the same band, even served by
different API replicas.

`ProjectionService` caches `SimulationOutput` keyed on that derived seed in a
small in-process TTL cache (`simulationCache`, `internal/service/
projection_simulation.go`) with a 5-minute window
(`SimulationCacheTTL`). This is intentionally not Redis: the simulation
itself is cheap enough (low single-digit ms) that a distributed cache would
add operational cost without a latency win for a single-replica-serves-fine
workload; the cache is keyed on the same seed a Redis-backed cache would use,
so swapping the backing store later doesn't change the method's contract.
