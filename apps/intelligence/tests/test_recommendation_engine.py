"""Tests for the personalized savings recommendation engine (#847).

Covers the four required behaviors:
1. Candidate generation correctness against known inputs.
2. The fabrication guard: a mocked model that invents a figure is rejected
   and the engine falls back to a deterministic, templated explanation.
3. Risk context is always attached to yield-related recommendations.
4. Dismissed actions are never re-recommended.
"""

from datetime import datetime, timedelta, timezone
from types import SimpleNamespace

import pytest

from app.services import recommendation_engine as engine_mod
from app.services.recommendation_engine import (
    AvailableVault,
    EngineContext,
    GoalContext,
    RecommendationEngine,
    VaultPosition,
    _fallback_recommendation_set,
    _grounded_numbers,
    _validate_selection,
    filter_and_rank_candidates,
    generate_consolidation_candidates,
    generate_contribution_candidates,
    generate_term_lock_candidates,
    generate_yield_move_candidates,
    select_and_explain,
)
from app.services.recommendation_store import _InMemoryEngagementStore

NOW = datetime(2026, 1, 1, tzinfo=timezone.utc)


# ---------------------------------------------------------------------------
# 1. Candidate generation correctness against known inputs
# ---------------------------------------------------------------------------


class TestContributionCandidates:
    def test_fires_when_behind_and_computes_exact_required_deposit(self):
        # target=1200, current=0, 12% APY (1% monthly), 10-month deadline.
        # P = FV * (r / ((1+r)^n - 1)) = 1200 * (0.01 / (1.01^10 - 1)) ~= 114.70
        goal = GoalContext(
            goal_id="goal-1",
            name="New Laptop",
            target_amount=1200.0,
            current_amount=0.0,
            currency="USDC",
            deadline=NOW + timedelta(days=305),  # ~10 months
            apy=0.12,
            avg_weekly_deposit=0.0,
        )
        candidates = generate_contribution_candidates([goal], now=NOW)

        assert len(candidates) == 1
        candidate = candidates[0]
        assert candidate.action_type == "increase_contribution"
        assert candidate.target_id == "goal-1"
        assert candidate.candidate_id == "increase_contribution:goal-1"
        assert "114.70" in candidate.summary
        assert candidate.impact.goal_success_probability_delta == 1.0
        assert candidate.impact.time_saved_months is None  # $0/month never reaches the goal
        assert candidate.impact.projection_source == "heuristic"
        assert candidate.risk_context is None  # not yield-related

    def test_no_candidate_when_already_on_track(self):
        # target=1200, no yield, 10 months -> required = 120/month exactly.
        # avg_weekly_deposit chosen so current_monthly_contribution == 120.
        goal = GoalContext(
            goal_id="goal-2",
            name="On Track Goal",
            target_amount=1200.0,
            current_amount=0.0,
            currency="USDC",
            deadline=NOW + timedelta(days=305),
            apy=0.0,
            avg_weekly_deposit=120.0 * (12.0 / 52.0),
        )
        candidates = generate_contribution_candidates([goal], now=NOW)
        assert candidates == []

    def test_no_candidate_when_goal_already_met(self):
        goal = GoalContext(
            goal_id="goal-3",
            name="Met Goal",
            target_amount=500.0,
            current_amount=600.0,
            currency="USDC",
            deadline=NOW + timedelta(days=60),
            apy=0.05,
        )
        assert generate_contribution_candidates([goal], now=NOW) == []

    def test_time_saved_computed_when_currently_contributing_something(self):
        # No yield, so months = remaining / deposit exactly.
        # remaining=600, current deposit=50/month -> 12 months alone.
        # required for 6-month deadline = 600/6 = 100/month.
        goal = GoalContext(
            goal_id="goal-4",
            name="Vacation",
            target_amount=600.0,
            current_amount=0.0,
            currency="USDC",
            deadline=NOW + timedelta(days=183),  # ~6 months
            apy=0.0,
            avg_weekly_deposit=50.0 * (12.0 / 52.0),
        )
        candidates = generate_contribution_candidates([goal], now=NOW)
        assert len(candidates) == 1
        impact = candidates[0].impact
        assert impact.time_saved_months == pytest.approx(6.0, abs=0.2)


class TestYieldMoveCandidates:
    def test_uses_go_rebalance_suggestion_number_directly(self):
        position = VaultPosition(
            vault_id="vault-1",
            name="Growth Vault",
            balance_usd=1000.0,
            apy=5.0,
            rebalance_suggestion={
                "has_suggestion": True,
                "expected_apy_gain_pct": 2.0,
                "reason": "Aave offers better risk-adjusted yield right now.",
            },
        )
        candidates = generate_yield_move_candidates([position], [], "moderate")

        assert len(candidates) == 1
        candidate = candidates[0]
        assert candidate.action_type == "move_to_higher_yield"
        assert candidate.impact.additional_yield_usdc == 20.0  # 1000 * 0.02 * 1
        assert candidate.impact.projection_source == "rebalance_service"
        assert candidate.risk_context is not None
        assert "Aave offers better risk-adjusted yield" in candidate.risk_context

    def test_heuristic_comparison_against_best_available_vault(self):
        position = VaultPosition(
            vault_id="vault-2", name="Flex Vault", balance_usd=2000.0, apy=4.0
        )
        available = [
            AvailableVault(vault_id="a1", name="Balanced Growth", apy=9.0, risk_tier_score=30.0)
        ]
        candidates = generate_yield_move_candidates([position], available, "moderate")

        assert len(candidates) == 1
        candidate = candidates[0]
        # additional_yield = 2000 * ((9.0 - 4.0) / 100) * (12/12) = 100.0
        assert candidate.impact.additional_yield_usdc == 100.0
        assert candidate.impact.projection_source == "heuristic"
        assert candidate.risk_context  # non-empty -- required for yield candidates
        assert "30" in candidate.risk_context

    def test_no_candidate_when_apy_delta_too_small(self):
        position = VaultPosition(
            vault_id="vault-3", name="Vault", balance_usd=1000.0, apy=8.0
        )
        available = [AvailableVault(vault_id="a2", name="Other", apy=8.2, risk_tier_score=40.0)]
        assert generate_yield_move_candidates([position], available, "moderate") == []

    def test_no_candidate_for_zero_balance(self):
        position = VaultPosition(vault_id="vault-4", name="Empty", balance_usd=0.0, apy=1.0)
        available = [AvailableVault(vault_id="a3", name="Other", apy=10.0, risk_tier_score=10.0)]
        assert generate_yield_move_candidates([position], available, "moderate") == []


class TestTermLockCandidates:
    def test_fires_for_flexible_balance_against_fixed_term_vault(self):
        position = VaultPosition(
            vault_id="vault-5", name="Flexible USDC", balance_usd=500.0, apy=5.0,
            lock_period_days=0,
        )
        available = [
            AvailableVault(vault_id="a4", name="Fixed-90d", apy=8.0, risk_tier_score=20.0)
        ]
        candidates = generate_term_lock_candidates([position], available)

        assert len(candidates) == 1
        candidate = candidates[0]
        assert candidate.action_type == "lock_for_term_boost"
        # additional_yield = 500 * ((8.0 - 5.0) / 100) * (12/12) = 15.0
        assert candidate.impact.additional_yield_usdc == 15.0
        assert candidate.risk_context  # required for yield-related candidates
        assert "Fixed-90d" in candidate.risk_context

    def test_no_candidate_when_already_locked(self):
        position = VaultPosition(
            vault_id="vault-6", name="Locked", balance_usd=500.0, apy=5.0,
            lock_period_days=90,
        )
        available = [
            AvailableVault(vault_id="a5", name="Fixed-90d", apy=9.0, risk_tier_score=20.0)
        ]
        assert generate_term_lock_candidates([position], available) == []

    def test_no_candidate_when_no_fixed_term_vault_available(self):
        position = VaultPosition(
            vault_id="vault-7", name="Flexible", balance_usd=500.0, apy=5.0
        )
        available = [AvailableVault(vault_id="a6", name="Balanced Growth", apy=12.0)]
        assert generate_term_lock_candidates([position], available) == []


class TestConsolidationCandidates:
    def test_pools_contribution_capacity_toward_nearest_deadline_goal(self):
        # goal_a: nearest deadline (~6 months), target=600, current=0, no yield,
        #   contributing 50/month -> 12 months alone.
        # goal_b: further deadline (~12 months), also contributing 50/month.
        # Pooled = 100/month -> 6 months. time_saved = 12 - 6 = 6.0
        goal_a = GoalContext(
            goal_id="goal-a",
            name="Near Goal",
            target_amount=600.0,
            current_amount=0.0,
            currency="USDC",
            deadline=NOW + timedelta(days=183),
            apy=0.0,
            avg_weekly_deposit=50.0 * (12.0 / 52.0),
        )
        goal_b = GoalContext(
            goal_id="goal-b",
            name="Far Goal",
            target_amount=1200.0,
            current_amount=0.0,
            currency="USDC",
            deadline=NOW + timedelta(days=365),
            apy=0.0,
            avg_weekly_deposit=50.0 * (12.0 / 52.0),
        )
        candidates = generate_consolidation_candidates([goal_a, goal_b], now=NOW)

        assert len(candidates) == 1
        candidate = candidates[0]
        assert candidate.action_type == "consolidate_goals"
        assert candidate.target_id == "goal-a"
        assert candidate.impact.time_saved_months == pytest.approx(6.0, abs=0.2)
        assert candidate.risk_context is None

    def test_no_candidate_with_single_active_goal(self):
        goal = GoalContext(
            goal_id="goal-x",
            name="Only Goal",
            target_amount=600.0,
            current_amount=0.0,
            currency="USDC",
            deadline=NOW + timedelta(days=183),
            apy=0.0,
            avg_weekly_deposit=50.0,
        )
        assert generate_consolidation_candidates([goal], now=NOW) == []


# ---------------------------------------------------------------------------
# 3. Risk context always attached to yield-related recommendations
# ---------------------------------------------------------------------------


class TestRiskContextInvariant:
    def test_every_yield_candidate_carries_risk_context(self):
        positions = [
            VaultPosition(vault_id="v1", name="Flex", balance_usd=1000.0, apy=4.0),
        ]
        available = [
            AvailableVault(vault_id="a1", name="Balanced Growth", apy=9.0, risk_tier_score=30.0),
            AvailableVault(vault_id="a2", name="Fixed-90d", apy=10.0, risk_tier_score=25.0),
        ]
        move_candidates = generate_yield_move_candidates(positions, available, "moderate")
        lock_candidates = generate_term_lock_candidates(positions, available)

        for candidate in move_candidates + lock_candidates:
            assert candidate.risk_context, f"{candidate.candidate_id} missing risk_context"

    def test_ensure_risk_context_fills_default_when_missing(self):
        from app.models.savings_recommendation import (
            RecommendationCandidate,
            RecommendationImpact,
        )
        from app.services.recommendation_engine import _ensure_risk_context

        candidate = RecommendationCandidate(
            candidate_id="move_to_higher_yield:v1",
            action_type="move_to_higher_yield",
            title="Move funds",
            summary="Move $100 to a higher-yield vault.",
            impact=RecommendationImpact(additional_yield_usdc=10.0),
            risk_context=None,
        )
        assert _ensure_risk_context(candidate)  # non-empty default supplied

    def test_non_yield_candidate_risk_context_untouched(self):
        from app.models.savings_recommendation import (
            RecommendationCandidate,
            RecommendationImpact,
        )
        from app.services.recommendation_engine import _ensure_risk_context

        candidate = RecommendationCandidate(
            candidate_id="increase_contribution:g1",
            action_type="increase_contribution",
            title="Boost",
            summary="Deposit more.",
            impact=RecommendationImpact(),
            risk_context=None,
        )
        assert _ensure_risk_context(candidate) is None


# ---------------------------------------------------------------------------
# 2. The fabrication guard
# ---------------------------------------------------------------------------


class FakeMessages:
    def __init__(self, responses):
        self._responses = list(responses)
        self.calls = 0

    async def create(self, *args, **kwargs):
        response = self._responses[min(self.calls, len(self._responses) - 1)]
        self.calls += 1
        return response


class FakeClient:
    def __init__(self, responses):
        self.messages = FakeMessages(responses)


def _tool_use_response(selections):
    block = SimpleNamespace(type="tool_use", input={"selections": selections})
    return SimpleNamespace(content=[block])


def _sample_context_and_candidates():
    goal = GoalContext(
        goal_id="goal-1",
        name="New Laptop",
        target_amount=1200.0,
        current_amount=0.0,
        currency="USDC",
        deadline=NOW + timedelta(days=305),
        apy=0.12,
    )
    candidates = generate_contribution_candidates([goal], now=NOW)
    context = EngineContext(user_id="user-1", goals=[goal], data_freshness="goals=live")
    return context, candidates


class TestFabricationGuard:
    def test_grounded_number_passes_validation(self):
        context, candidates = _sample_context_and_candidates()
        candidates_by_id = {c.candidate_id: c for c in candidates}
        ok, violations = _validate_selection(
            [
                {
                    "candidate_id": candidates[0].candidate_id,
                    "explanation": "Raise your deposit to $114.70 to hit the goal on time.",
                }
            ],
            candidates_by_id,
        )
        assert ok
        assert violations == []

    def test_invented_number_is_rejected(self):
        context, candidates = _sample_context_and_candidates()
        candidates_by_id = {c.candidate_id: c for c in candidates}
        ok, violations = _validate_selection(
            [
                {
                    "candidate_id": candidates[0].candidate_id,
                    "explanation": "You'll have $9999 saved by next year!",
                }
            ],
            candidates_by_id,
        )
        assert not ok
        assert any("9999" in v for v in violations)

    def test_unknown_candidate_id_is_rejected(self):
        context, candidates = _sample_context_and_candidates()
        candidates_by_id = {c.candidate_id: c for c in candidates}
        ok, violations = _validate_selection(
            [{"candidate_id": "not-a-real-id", "explanation": "Do this."}],
            candidates_by_id,
        )
        assert not ok
        assert any("unknown candidate_id" in v for v in violations)

    @pytest.mark.asyncio
    async def test_engine_regenerates_then_falls_back_on_persistent_fabrication(
        self, monkeypatch
    ):
        context, candidates = _sample_context_and_candidates()
        cid = candidates[0].candidate_id

        # Both attempts invent a number not present in the candidate data --
        # the engine must never surface it and must fall back to the
        # deterministic, templated explanation (the candidate's own summary).
        bad_response = _tool_use_response(
            [{"candidate_id": cid, "explanation": "You'll earn an extra $9999 this year!"}]
        )
        fake_client = FakeClient([bad_response, bad_response])
        monkeypatch.setattr(engine_mod, "get_client", lambda: fake_client)

        result = await select_and_explain(candidates, context)

        assert fake_client.messages.calls == 2  # initial attempt + one regeneration
        assert len(result.recommendations) == 1
        explanation = result.recommendations[0].explanation
        assert "9999" not in explanation
        assert explanation == candidates[0].summary  # deterministic fallback text

    @pytest.mark.asyncio
    async def test_engine_accepts_valid_selection_on_first_attempt(self, monkeypatch):
        context, candidates = _sample_context_and_candidates()
        cid = candidates[0].candidate_id
        good_response = _tool_use_response(
            [{"candidate_id": cid, "explanation": "Raise your deposit to $114.70 per month."}]
        )
        fake_client = FakeClient([good_response])
        monkeypatch.setattr(engine_mod, "get_client", lambda: fake_client)

        result = await select_and_explain(candidates, context)

        assert fake_client.messages.calls == 1
        assert len(result.recommendations) == 1
        assert result.recommendations[0].explanation == (
            "Raise your deposit to $114.70 per month."
        )

    @pytest.mark.asyncio
    async def test_no_candidates_short_circuits_without_calling_llm(self, monkeypatch):
        context = EngineContext(user_id="user-1", data_freshness="none")

        def _boom():
            raise AssertionError("LLM should not be called with zero candidates")

        monkeypatch.setattr(engine_mod, "get_client", _boom)
        result = await select_and_explain([], context)
        assert result.recommendations == []


class TestFallbackRecommendationSet:
    def test_fallback_uses_candidate_summary_verbatim(self):
        context, candidates = _sample_context_and_candidates()
        result = _fallback_recommendation_set(candidates, context)
        assert result.recommendations[0].explanation == candidates[0].summary


# ---------------------------------------------------------------------------
# 4. Dismissed actions are not re-recommended
# ---------------------------------------------------------------------------


class TestDismissedFiltering:
    def test_dismissed_candidate_is_filtered_out(self):
        store = _InMemoryEngagementStore()
        store.record("user-1", "move_to_higher_yield:vault-1", "dismissed")

        position = VaultPosition(vault_id="vault-1", name="Flex", balance_usd=1000.0, apy=4.0)
        available = [AvailableVault(vault_id="a1", name="Growth", apy=9.0, risk_tier_score=30.0)]
        candidates = generate_yield_move_candidates([position], available, "moderate")
        assert len(candidates) == 1  # sanity: candidate would otherwise be generated

        kept = filter_and_rank_candidates(candidates, "user-1", store)
        assert kept == []

    def test_dismissal_is_scoped_per_user(self):
        store = _InMemoryEngagementStore()
        store.record("user-1", "move_to_higher_yield:vault-1", "dismissed")

        position = VaultPosition(vault_id="vault-1", name="Flex", balance_usd=1000.0, apy=4.0)
        available = [AvailableVault(vault_id="a1", name="Growth", apy=9.0, risk_tier_score=30.0)]
        candidates = generate_yield_move_candidates([position], available, "moderate")

        kept = filter_and_rank_candidates(candidates, "user-2", store)
        assert len(kept) == 1

    def test_acted_on_type_gets_priority_boost_not_filtered(self):
        store = _InMemoryEngagementStore()
        store.record("user-1", "move_to_higher_yield:vault-old", "acted_on")

        position = VaultPosition(vault_id="vault-1", name="Flex", balance_usd=1000.0, apy=4.0)
        available = [AvailableVault(vault_id="a1", name="Growth", apy=9.0, risk_tier_score=30.0)]
        candidates = generate_yield_move_candidates([position], available, "moderate")
        base_score = candidates[0].priority_score

        kept = filter_and_rank_candidates(candidates, "user-1", store)
        assert len(kept) == 1
        assert kept[0].priority_score == pytest.approx(base_score * 1.25)

    @pytest.mark.asyncio
    async def test_engine_end_to_end_never_recommends_dismissed_candidate(self, monkeypatch):
        class FakeFetcher:
            async def fetch_user_vaults(self, user_id):
                return [
                    {"id": "vault-1", "name": "Flex Vault", "balance_usd": 1000.0, "apy": 4.0}
                ]

            async def fetch_available_vaults(self):
                return [{"id": "a1", "name": "Growth", "apy": 9.0}]

            async def fetch_savings_goals(self, user_id):
                return []

            async def fetch_vault_rebalance_suggestion(self, vault_id, user_id):
                return {}

            async def fetch_vault_risk(self, vault_id):
                return {"overall": 30.0}

        store = _InMemoryEngagementStore()
        store.record("user-1", "move_to_higher_yield:vault-1", "dismissed")

        def _boom():
            raise AssertionError(
                "LLM should not be reachable once the only candidate is filtered out"
            )

        monkeypatch.setattr(engine_mod, "get_client", _boom)

        eng = RecommendationEngine(
            fetcher=FakeFetcher(), projection_provider=None, engagement_store=store
        )
        # Force refresh so we don't hit any cross-test cache collisions.
        result = await eng.generate_for_user("user-1", "moderate", force_refresh=True)
        assert result.recommendations == []


# ---------------------------------------------------------------------------
# _grounded_numbers sanity
# ---------------------------------------------------------------------------


def test_grounded_numbers_includes_summary_and_impact_figures():
    context, candidates = _sample_context_and_candidates()
    grounded = _grounded_numbers(candidates[0])
    assert "114.7" in grounded
    assert "1200" in grounded
