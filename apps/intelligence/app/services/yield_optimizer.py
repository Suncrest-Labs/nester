"""Constraint-based yield optimization engine (#848).

Computes a deterministic, risk-adjusted-optimal allocation of capital across a
set of candidate yield sources, given hard constraints (diversification cap,
liquidity floor, lock-horizon fit, risk ceiling, protocol deposit caps, and
source status). This module is intentionally plain, synchronous, and free of
any I/O or LLM calls, so it is a pure function of its inputs: `optimize()` can
be exhaustively unit tested with hand-built `YieldSourceInput`/
`AllocationConstraints` values and no mocking whatsoever. The LLM explanation
layer (`app.services.yield_explanation`) is a separate module that only reads
the `OptimizationResult` this file produces; it never computes or adjusts any
number here.

## Objective function

Given weights ``w_i`` (fraction of capital in source i, for i in the eligible
set E), expected APY ``mu_i`` (as a fraction, e.g. 0.085 for 8.5%), and
normalized risk ``r_i = risk_score_i / 100`` in [0, 1], the optimizer
maximizes the risk-adjusted expected return:

    J(w) = sum_i w_i * mu_i            (expected portfolio yield)
         - gamma * sum_i w_i * r_i      (risk penalty)
         - kappa * sum_i w_i ** 2       (concentration penalty)

subject to:

    sum_i w_i == 1                                         (fully allocated)
    0 <= w_i <= min(max_per_source_weight, cap_bound_i)    for each i in E
    sum_i w_i * liquid_fraction_i >= min_liquid_fraction   (liquidity floor)

where ``gamma = constraints.risk_aversion`` and
``kappa = constraints.concentration_aversion``. This is deliberately NOT raw
APY maximization: the second term penalizes exposure to sources the
risk-scoring engine (`vault_context.fetch_vault_risk`) considers risky, and
the third term is (up to a constant) the Herfindahl-Hirschman Index of the
allocation, penalizing concentration in a single source over and above the
explicit diversification cap.

## Why this is a real convex optimization, not ad-hoc weighting

The feasible region (a hyperplane intersected with box bounds and a
halfspace) is convex. The objective J(w) is a strictly concave quadratic
whenever kappa > 0: the yield and risk-penalty terms are linear (both convex
and concave), and ``-kappa * sum(w_i**2)`` is concave because
``kappa * sum(w_i**2)`` is a positive-semidefinite quadratic form (it is
diagonal with positive entries), hence convex. Maximizing a strictly concave
function over a convex set has a *unique* global maximum, so any local
optimum a constrained solver finds is guaranteed to be the true global
optimum -- there is no risk of the kind of "looks reasonable but is not
actually optimal" ad-hoc weighting the issue explicitly warns against (see
`prometheus.py`'s `_rank_vaults`/`_build_allocation_plan`, which uses fixed
percentage splits by risk-tolerance bucket rather than solving anything).

We solve the equivalent minimization of ``-J(w)`` with
``scipy.optimize.minimize`` (method="SLSQP"), which natively supports the mix
of one linear equality constraint (sum to 1), one linear inequality
constraint (liquidity floor), and box bounds (per-source caps) that this
problem needs. See `requirements.txt` for why scipy was chosen over cvxpy/
PuLP: it is already a common, mature, well-tested numerical dependency, and
this is a small, well-behaved quadratic program that does not need a heavier
convex-modeling DSL.

## Hard constraints and eligibility

Before optimizing, sources are filtered to the *eligible set*: a source is
eligible only if ALL of the following hold:

    - status == "active"          (paused/degraded/closed are never eligible)
    - lock_days <= max_lock_days  (lock-horizon fit)
    - risk_score <= max_risk_score (risk tolerance / risk ceiling)

Ineligible sources are excluded from the decision variables entirely (not
merely bounded to zero) and always appear in the result with
``weight == 0`` and ``eligible == False``. There is therefore no way for the
objective to "trade off" past these hard constraints -- they are enforced
structurally, not as soft penalties.

Deposit/protocol caps are enforced as a tighter per-source upper bound: if a
source has ``deposit_cap_usd`` set and the caller passed a nonzero
``investable_amount_usd``, its maximum weight is
``max(0, deposit_cap_usd - current_balance_usd) / investable_amount_usd``,
further capped by ``max_per_source_weight``. When ``investable_amount_usd``
is 0 (unknown/not provided), deposit caps cannot be translated into a weight
bound and are ignored; only the diversification cap applies in that case.

## Infeasibility

If, after eligibility filtering and bound computation, no allocation can
satisfy ``sum(w) == 1`` and the liquidity floor simultaneously within the
per-source bounds, `optimize()` returns
``OptimizationResult(feasible=False, weights=[<all-zero>], ...)`` with
``infeasibility_reasons`` describing exactly which requirement(s) cannot be
met. This is a required, tested behavior: constraints are never silently
relaxed to produce a "best effort" allocation instead.
"""

from __future__ import annotations

import numpy as np
from numpy.typing import NDArray
from scipy.optimize import LinearConstraint, minimize

from app.models.optimization import (
    AllocationConstraints,
    AllocationWeight,
    OptimizationResult,
    YieldSourceInput,
)


def _eligible_bound(
    source: YieldSourceInput,
    constraints: AllocationConstraints,
    investable_amount_usd: float,
) -> float | None:
    """Return the source's upper weight bound, or None if it is ineligible."""
    if source.status != "active":
        return None
    if source.lock_days > constraints.max_lock_days:
        return None
    if source.risk_score > constraints.max_risk_score:
        return None

    bound = constraints.max_per_source_weight
    if source.deposit_cap_usd is not None and investable_amount_usd > 0:
        headroom_usd = max(0.0, source.deposit_cap_usd - source.current_balance_usd)
        cap_weight = headroom_usd / investable_amount_usd
        bound = min(bound, cap_weight)
    return max(0.0, bound)


def optimize(
    sources: list[YieldSourceInput],
    constraints: AllocationConstraints | None = None,
    investable_amount_usd: float = 0.0,
) -> OptimizationResult:
    """Compute the risk-adjusted-optimal allocation across ``sources``.

    Pure function: no I/O, no randomness, no LLM calls. Deterministic for a
    given input (SLSQP is seeded from a fixed, feasible starting point
    derived from the bounds, so repeated calls with the same input return the
    same result).
    """
    constraints = constraints or AllocationConstraints()

    bounds_by_id: dict[str, float] = {}
    for s in sources:
        bound = _eligible_bound(s, constraints, investable_amount_usd)
        if bound is not None:
            bounds_by_id[s.id] = bound

    eligible = [s for s in sources if s.id in bounds_by_id]

    if not eligible:
        return _infeasible_result(
            sources,
            [
                "No eligible sources: every candidate was excluded by its status, "
                "lock-horizon fit, or risk ceiling."
            ],
        )

    upper_bounds: NDArray[np.float64] = np.array([bounds_by_id[s.id] for s in eligible])
    liquid: NDArray[np.float64] = np.array([s.liquid_fraction for s in eligible])

    reasons: list[str] = []
    total_capacity = float(upper_bounds.sum())
    if total_capacity < 1.0 - 1e-9:
        reasons.append(
            f"Eligible sources can absorb at most {total_capacity * 100:.2f}% of the capital "
            "under the current per-source diversification cap and deposit caps, but 100% "
            "allocation is required."
        )

    liquid_capacity = float(np.dot(upper_bounds, liquid))
    if liquid_capacity < constraints.min_liquid_fraction - 1e-9:
        reasons.append(
            f"Eligible sources can supply at most {liquid_capacity * 100:.2f}% liquidity, "
            f"but a minimum liquid fraction of {constraints.min_liquid_fraction * 100:.2f}% "
            "is required."
        )

    if reasons:
        return _infeasible_result(sources, reasons)

    n = len(eligible)
    mu: NDArray[np.float64] = np.array([s.apy_pct / 100.0 for s in eligible])
    r: NDArray[np.float64] = np.array([s.risk_score / 100.0 for s in eligible])
    gamma = constraints.risk_aversion
    kappa = constraints.concentration_aversion

    def neg_objective(w: NDArray[np.float64]) -> float:
        expected = float(np.dot(w, mu))
        risk_penalty = gamma * float(np.dot(w, r))
        concentration_penalty = kappa * float(np.dot(w, w))
        return -(expected - risk_penalty - concentration_penalty)

    def neg_objective_grad(w: NDArray[np.float64]) -> NDArray[np.float64]:
        grad: NDArray[np.float64] = -(mu - gamma * r - 2 * kappa * w)
        return grad

    lc_sum = LinearConstraint(np.ones(n), lb=1.0, ub=1.0)
    lc_liquid = LinearConstraint(liquid, lb=constraints.min_liquid_fraction, ub=np.inf)
    scipy_bounds = [(0.0, float(b)) for b in upper_bounds]

    # Deterministic, feasible starting point: distribute capital proportional
    # to each source's own bound, scaled to sum to 1 (always feasible since
    # total_capacity >= 1 was already verified above).
    x0 = upper_bounds / total_capacity

    result = minimize(
        neg_objective,
        x0,
        jac=neg_objective_grad,
        method="SLSQP",
        bounds=scipy_bounds,
        constraints=[lc_sum, lc_liquid],
        options={"maxiter": 200, "ftol": 1e-12},
    )

    if not result.success:
        return _infeasible_result(
            sources, [f"Optimizer failed to converge: {result.message}"]
        )

    weights: NDArray[np.float64] = np.clip(result.x, 0.0, None)
    weight_sum = float(weights.sum())
    if weight_sum > 0:
        # Numerical cleanup only: correct floating-point drift from the
        # equality constraint so weights sum to exactly 1. This does not
        # change which constraints were satisfied by the solver.
        weights = weights / weight_sum

    return _feasible_result(sources, eligible, weights, mu, r, -float(result.fun))


def _largest_remainder_bps(weights: list[float]) -> list[int]:
    """Round fractional weights to integer basis points summing to exactly
    10000, using largest-remainder rounding so the bps allocation matches the
    fractional one as closely as possible with no source silently dropped by
    rounding."""
    raw = [w * 10_000 for w in weights]
    floors = [int(x) for x in raw]
    remainder = 10_000 - sum(floors)
    order = sorted(range(len(raw)), key=lambda i: (raw[i] - floors[i]), reverse=True)
    for i in order[:remainder]:
        floors[i] += 1
    return floors


def _feasible_result(
    all_sources: list[YieldSourceInput],
    eligible: list[YieldSourceInput],
    weights: NDArray[np.float64],
    mu: NDArray[np.float64],
    r: NDArray[np.float64],
    objective_value: float,
) -> OptimizationResult:
    bps_list = _largest_remainder_bps(list(weights))
    eligible_ids = {s.id for s in eligible}
    weight_by_id = {s.id: float(w) for s, w in zip(eligible, weights, strict=True)}
    bps_by_id = {s.id: b for s, b in zip(eligible, bps_list, strict=True)}

    out: list[AllocationWeight] = []
    for s in all_sources:
        if s.id in eligible_ids:
            out.append(
                AllocationWeight(
                    source_id=s.id,
                    protocol=s.protocol,
                    weight=round(weight_by_id[s.id], 6),
                    weight_bps=bps_by_id[s.id],
                    apy_pct=s.apy_pct,
                    risk_score=s.risk_score,
                    eligible=True,
                )
            )
        else:
            out.append(
                AllocationWeight(
                    source_id=s.id,
                    protocol=s.protocol,
                    weight=0.0,
                    weight_bps=0,
                    apy_pct=s.apy_pct,
                    risk_score=s.risk_score,
                    eligible=False,
                )
            )

    expected_yield_pct = float(np.dot(weights, mu)) * 100.0
    aggregate_risk_score = float(np.dot(weights, r)) * 100.0
    hhi = float(np.dot(weights, weights))
    diversification_index = 1.0 - hhi

    return OptimizationResult(
        feasible=True,
        weights=out,
        expected_yield_pct=round(expected_yield_pct, 4),
        aggregate_risk_score=round(aggregate_risk_score, 4),
        diversification_index=round(diversification_index, 6),
        objective_value=round(objective_value, 6),
        method="scipy.optimize.minimize(SLSQP) on a concave-quadratic risk-adjusted objective",
        infeasibility_reasons=[],
    )


def _infeasible_result(
    all_sources: list[YieldSourceInput], reasons: list[str]
) -> OptimizationResult:
    out = [
        AllocationWeight(
            source_id=s.id,
            protocol=s.protocol,
            weight=0.0,
            weight_bps=0,
            apy_pct=s.apy_pct,
            risk_score=s.risk_score,
            eligible=False,
        )
        for s in all_sources
    ]
    return OptimizationResult(
        feasible=False,
        weights=out,
        expected_yield_pct=0.0,
        aggregate_risk_score=0.0,
        diversification_index=0.0,
        objective_value=0.0,
        method="scipy.optimize.minimize(SLSQP) on a concave-quadratic risk-adjusted objective",
        infeasibility_reasons=reasons,
    )
