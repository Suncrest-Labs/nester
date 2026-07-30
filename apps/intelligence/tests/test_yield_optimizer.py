"""Tests for the constraint-based yield optimization engine (#848).

`app.services.yield_optimizer.optimize` is a pure, synchronous function with
no I/O, so every scenario here is exercised directly with hand-built
`YieldSourceInput`/`AllocationConstraints` values -- no mocking needed.
"""

import pytest

from app.models.optimization import AllocationConstraints, YieldSourceInput
from app.services import yield_optimizer as yo


def _source(**kwargs: object) -> YieldSourceInput:
    defaults: dict[str, object] = {
        "id": "s",
        "protocol": "Protocol",
        "apy_pct": 10.0,
        "risk_score": 20.0,
    }
    defaults.update(kwargs)
    return YieldSourceInput(**defaults)  # type: ignore[arg-type]


class TestKnownOptimalAllocation:
    """Scenario 1: a hand-constructed small case returns the known-optimal
    allocation.

    Source A dominates source B on both yield (30% vs 5%) and risk (5 vs 60),
    so the unconstrained optimum wants as much of A as possible. A
    diversification cap of 40% forces the solver to put exactly 60% in A
    (the cap) and the remaining 40% in B (the only other source, so it is
    pinned by the sum-to-1 constraint). This is analytically verifiable: for
    the default risk_aversion=0.5 / concentration_aversion=1.0, the
    unconstrained optimum for A's weight is
        w* = (2 + (mu_A - mu_B - gamma*(r_A - r_B)) / kappa) / 4
           = (2 + (0.25 - 0.5*(-0.55)) / 1) / 4 = 0.63125
    which exceeds the 0.6 cap, so the cap binds and w_A = 0.6 exactly.
    """

    def test_two_source_cap_binding_case(self) -> None:
        sources = [
            _source(id="a", protocol="A", apy_pct=30.0, risk_score=5.0),
            _source(id="b", protocol="B", apy_pct=5.0, risk_score=60.0),
        ]
        constraints = AllocationConstraints(max_per_source_weight=0.6)

        result = yo.optimize(sources, constraints, investable_amount_usd=0.0)

        assert result.feasible
        by_id = {w.source_id: w for w in result.weights}
        assert by_id["a"].weight == pytest.approx(0.6, abs=1e-4)
        assert by_id["b"].weight == pytest.approx(0.4, abs=1e-4)
        assert by_id["a"].weight_bps == 6000
        assert by_id["b"].weight_bps == 4000
        assert by_id["a"].eligible is True
        assert by_id["b"].eligible is True

        # Expected yield / risk are deterministic weighted averages of the inputs.
        assert result.expected_yield_pct == pytest.approx(20.0, abs=1e-3)
        assert result.aggregate_risk_score == pytest.approx(27.0, abs=1e-3)
        # HHI = 0.6^2 + 0.4^2 = 0.52 -> diversification = 1 - 0.52 = 0.48
        assert result.diversification_index == pytest.approx(0.48, abs=1e-4)

    def test_single_dominant_source_takes_full_allocation(self) -> None:
        """With only one eligible source, it must receive 100% regardless of
        objective parameters -- the sum-to-1 constraint leaves no choice."""
        sources = [_source(id="only", apy_pct=12.0, risk_score=10.0)]
        result = yo.optimize(sources, AllocationConstraints(max_per_source_weight=1.0))

        assert result.feasible
        assert len(result.weights) == 1
        assert result.weights[0].weight == pytest.approx(1.0, abs=1e-6)
        assert result.weights[0].weight_bps == 10_000


class TestHardConstraintsSatisfied:
    """Scenario 2: hard constraints (per-source cap, liquidity floor, risk
    ceiling, source-status eligibility) are always satisfied in the result,
    never silently relaxed."""

    def _mixed_sources(self) -> list[YieldSourceInput]:
        return [
            _source(id="a", protocol="A", apy_pct=25.0, risk_score=10.0),
            # Best risk/yield but paused -> must be excluded entirely.
            _source(id="b", protocol="B", apy_pct=15.0, risk_score=30.0, status="paused"),
            # Above the risk ceiling -> must be excluded entirely.
            _source(id="c", protocol="C", apy_pct=12.0, risk_score=90.0),
            _source(id="d", protocol="D", apy_pct=9.0, risk_score=20.0, liquid_fraction=1.0),
            # Lock horizon exceeds the max -> must be excluded entirely.
            _source(
                id="e",
                protocol="E",
                apy_pct=7.0,
                risk_score=15.0,
                liquid_fraction=1.0,
                lock_days=400,
            ),
            # Within the lock horizon and eligible.
            _source(
                id="f",
                protocol="F",
                apy_pct=6.0,
                risk_score=15.0,
                liquid_fraction=1.0,
                lock_days=20,
            ),
        ]

    def test_paused_risk_ceiling_and_lock_horizon_sources_are_ineligible(self) -> None:
        sources = self._mixed_sources()
        constraints = AllocationConstraints(
            max_per_source_weight=0.4,
            min_liquid_fraction=0.3,
            max_risk_score=50.0,
            max_lock_days=30,
        )

        result = yo.optimize(sources, constraints)

        assert result.feasible
        by_id = {w.source_id: w for w in result.weights}

        # Ineligible sources always carry zero weight and eligible=False.
        assert by_id["b"].eligible is False and by_id["b"].weight == 0.0
        assert by_id["c"].eligible is False and by_id["c"].weight == 0.0
        assert by_id["e"].eligible is False and by_id["e"].weight == 0.0

        # Eligible sources were actually used.
        assert by_id["a"].eligible is True
        assert by_id["d"].eligible is True
        assert by_id["f"].eligible is True

    def test_per_source_cap_never_exceeded(self) -> None:
        sources = self._mixed_sources()
        constraints = AllocationConstraints(
            max_per_source_weight=0.4,
            min_liquid_fraction=0.3,
            max_risk_score=50.0,
            max_lock_days=30,
        )
        result = yo.optimize(sources, constraints)

        assert result.feasible
        for w in result.weights:
            assert w.weight <= 0.4 + 1e-6

    def test_liquidity_floor_satisfied_in_solution(self) -> None:
        sources = self._mixed_sources()
        constraints = AllocationConstraints(
            max_per_source_weight=0.4,
            min_liquid_fraction=0.3,
            max_risk_score=50.0,
            max_lock_days=30,
        )
        result = yo.optimize(sources, constraints)

        assert result.feasible
        liquid_by_id = {s.id: s.liquid_fraction for s in sources}
        liquid_weight = sum(
            w.weight * liquid_by_id[w.source_id] for w in result.weights
        )
        assert liquid_weight >= constraints.min_liquid_fraction - 1e-6

    def test_weights_sum_to_one_and_bps_to_ten_thousand(self) -> None:
        sources = self._mixed_sources()
        constraints = AllocationConstraints(
            max_per_source_weight=0.4,
            min_liquid_fraction=0.3,
            max_risk_score=50.0,
            max_lock_days=30,
        )
        result = yo.optimize(sources, constraints)

        assert result.feasible
        assert sum(w.weight for w in result.weights) == pytest.approx(1.0, abs=1e-6)
        assert sum(w.weight_bps for w in result.weights) == 10_000

    def test_deposit_cap_tightens_bound_below_diversification_cap(self) -> None:
        """A source with a small deposit cap relative to the investable amount
        must never receive more weight than its cap allows, even though the
        diversification cap alone would permit more."""
        sources = [
            _source(
                id="capped",
                protocol="Capped",
                apy_pct=20.0,
                risk_score=10.0,
                deposit_cap_usd=100.0,
                current_balance_usd=0.0,
            ),
            _source(id="uncapped", protocol="Uncapped", apy_pct=8.0, risk_score=15.0),
        ]
        constraints = AllocationConstraints(max_per_source_weight=0.9)
        # $1000 investable: capped source's max weight is 100/1000 = 0.10,
        # far below the 0.9 diversification cap.
        result = yo.optimize(sources, constraints, investable_amount_usd=1000.0)

        assert result.feasible
        by_id = {w.source_id: w for w in result.weights}
        assert by_id["capped"].weight <= 0.10 + 1e-6


class TestInfeasibilityReporting:
    """Scenario 3: an infeasible constraint set is reported as infeasible and
    is never silently relaxed into a constraint-violating "best effort"
    allocation."""

    def test_liquidity_floor_unreachable_is_infeasible(self) -> None:
        sources = [
            _source(id="a", apy_pct=10.0, risk_score=20.0, liquid_fraction=0.0),
        ]
        constraints = AllocationConstraints(max_per_source_weight=1.0, min_liquid_fraction=0.5)

        result = yo.optimize(sources, constraints)

        assert result.feasible is False
        assert result.infeasibility_reasons
        assert any("liquid" in reason.lower() for reason in result.infeasibility_reasons)
        # No allocation is returned when infeasible.
        assert all(w.weight == 0.0 and w.weight_bps == 0 for w in result.weights)
        assert result.expected_yield_pct == 0.0
        assert result.aggregate_risk_score == 0.0

    def test_all_sources_above_risk_ceiling_is_infeasible(self) -> None:
        sources = [
            _source(id="a", apy_pct=10.0, risk_score=90.0),
            _source(id="b", apy_pct=8.0, risk_score=95.0),
        ]
        constraints = AllocationConstraints(max_risk_score=50.0)

        result = yo.optimize(sources, constraints)

        assert result.feasible is False
        assert result.infeasibility_reasons
        assert all(w.eligible is False for w in result.weights)

    def test_diversification_cap_too_tight_for_full_allocation_is_infeasible(self) -> None:
        """Two eligible sources each capped at 30% can supply at most 60% of
        capital -- never enough to reach the required 100% allocation."""
        sources = [
            _source(id="a", apy_pct=10.0, risk_score=10.0),
            _source(id="b", apy_pct=9.0, risk_score=10.0),
        ]
        constraints = AllocationConstraints(max_per_source_weight=0.3)

        result = yo.optimize(sources, constraints)

        assert result.feasible is False
        assert any(
            "100%" in reason or "capacity" in reason.lower()
            for reason in result.infeasibility_reasons
        )

    def test_no_eligible_sources_at_all_is_infeasible(self) -> None:
        sources = [
            _source(id="a", apy_pct=10.0, risk_score=10.0, status="paused"),
            _source(id="b", apy_pct=8.0, risk_score=10.0, status="closed"),
        ]
        result = yo.optimize(sources, AllocationConstraints())

        assert result.feasible is False
        assert result.infeasibility_reasons


class TestObjectiveIsRiskAdjustedNotRawApy:
    """The objective must penalize risk and concentration, not just rank by
    raw APY -- a higher-APY-but-much-riskier source should not always win."""

    def test_higher_risk_source_gets_less_weight_than_lower_risk_similar_apy(self) -> None:
        sources = [
            _source(id="safe", protocol="Safe", apy_pct=10.0, risk_score=5.0),
            _source(id="risky", protocol="Risky", apy_pct=10.5, risk_score=80.0),
        ]
        # No per-source cap binding, so the objective alone determines the split.
        constraints = AllocationConstraints(max_per_source_weight=1.0, risk_aversion=1.0)
        result = yo.optimize(sources, constraints)

        assert result.feasible
        by_id = {w.source_id: w for w in result.weights}
        # Despite a slightly higher raw APY, "risky" should receive materially
        # less weight than "safe" once risk is penalized.
        assert by_id["safe"].weight > by_id["risky"].weight
