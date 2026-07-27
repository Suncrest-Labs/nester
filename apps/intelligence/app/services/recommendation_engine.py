"""Deterministic candidate generation + LLM selection for savings
recommendations (#847).

Architectural rule (see `app.models.savings_recommendation` docstring): every
number in the final output must trace back to a `RecommendationCandidate`
computed here in plain Python (or passed through unchanged from a Go API
computation, e.g. the vault rebalance service). Claude, called via tool use,
only selects which candidates to surface, orders them, and writes explanation
prose -- it never computes a number. `_validate_selection` enforces this after
every LLM call and regenerates once, falling back to a templated explanation
built directly from the candidate data if the model still fabricates a figure.

Generation is cached per-user on a cadence (`_RECO_CACHE_TTL`) and invalidated
automatically when the user's goals/vault balances change materially (see
`_context_fingerprint`), rather than being recomputed on every page load.
"""

from __future__ import annotations

import hashlib
import json
import logging
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Optional, cast

from anthropic.types import MessageParam, ToolChoiceToolParam, ToolParam

from app.config import settings
from app.models.savings_recommendation import (
    CandidateActionType,
    RecommendationCandidate,
    RecommendationImpact,
    SavingsRecommendationItem,
    SavingsRecommendationSet,
)
from app.services.finance_math import months_to_reach_target, required_monthly_deposit
from app.services.projection_client import ProjectionProvider, get_projection_provider
from app.services.prometheus import get_client
from app.services.recommendation_store import EngagementStore
from app.services.recommendation_store import store as default_engagement_store
from app.services.retrieval import extract_numbers
from app.services.vault_context import VaultContextFetcher

logger = logging.getLogger(__name__)

SELECT_MAX_TOKENS = 900
_MAX_REGENERATE_ATTEMPTS = 1  # one retry before falling back to templated text
_MAX_CANDIDATES_IN_PROMPT = 10
_SELECTION_LIMIT = 4

_RISK_TOLERANCE_CAPS: dict[str, float] = {
    "conservative": 35.0,
    "moderate": 65.0,
    "aggressive": 100.0,
}

_MEANINGFUL_APY_DELTA_PCT = 0.5  # percentage points
_MEANINGFUL_CONTRIBUTION_GAP_RATIO = 1.05

_RECO_CACHE_TTL = 6 * 3600  # 6 hours -- cadence-based regeneration, not per page load
_RECO_KEY_PREFIX = "prometheus:reco:"
_mem_reco_cache: dict[str, tuple[dict[str, Any], float]] = {}
_redis_client: Any = None
_redis_available = False

_YIELD_ACTION_TYPES: frozenset[CandidateActionType] = frozenset(
    {"move_to_higher_yield", "lock_for_term_boost"}
)
_DEFAULT_YIELD_RISK_CONTEXT = (
    "Higher-yield vaults generally carry higher protocol, liquidity, or "
    "concentration risk than your current allocation. Review the specific "
    "risk score before moving funds."
)


# ---------------------------------------------------------------------------
# Deterministic input contexts
# ---------------------------------------------------------------------------


@dataclass
class GoalContext:
    goal_id: str
    name: str
    target_amount: float
    current_amount: float
    currency: str
    deadline: datetime
    apy: float  # annual rate as a fraction, e.g. 0.08 == 8%
    avg_weekly_deposit: float = 0.0
    vault_id: Optional[str] = None

    @property
    def current_monthly_contribution(self) -> float:
        return self.avg_weekly_deposit * (52.0 / 12.0)

    def months_remaining(self, now: Optional[datetime] = None) -> int:
        now = now or datetime.now(timezone.utc)
        deadline = (
            self.deadline if self.deadline.tzinfo else self.deadline.replace(tzinfo=timezone.utc)
        )
        delta_days = (deadline - now).days
        return max(1, round(delta_days / 30.44))


@dataclass
class VaultPosition:
    vault_id: str
    name: str
    balance_usd: float
    apy: float  # percentage number, e.g. 8.5 == 8.5% (matches VaultContextFetcher convention)
    lock_period_days: int = 0
    rebalance_suggestion: Optional[dict[str, Any]] = None


@dataclass
class AvailableVault:
    vault_id: str
    name: str
    apy: float  # percentage number
    risk_tier_score: float = 100.0  # 0-100, lower is safer


@dataclass
class EngineContext:
    user_id: str
    goals: list[GoalContext] = field(default_factory=list)
    positions: list[VaultPosition] = field(default_factory=list)
    available: list[AvailableVault] = field(default_factory=list)
    risk_tolerance: str = "moderate"
    data_freshness: str = ""


def _candidate_key(action_type: CandidateActionType, target_id: str) -> str:
    return f"{action_type}:{target_id}"


# ---------------------------------------------------------------------------
# Candidate generators -- pure, deterministic, unit-testable with known inputs
# ---------------------------------------------------------------------------


def generate_contribution_candidates(
    goals: list[GoalContext], *, now: Optional[datetime] = None
) -> list[RecommendationCandidate]:
    """Recommend increasing a goal's monthly contribution when the amount
    currently being deposited will not hit the target by its deadline."""
    candidates: list[RecommendationCandidate] = []
    for goal in goals:
        remaining_amount = max(goal.target_amount - goal.current_amount, 0.0)
        if remaining_amount <= 0:
            continue  # goal already met

        months_left = goal.months_remaining(now)
        monthly_rate = goal.apy / 12.0
        required = required_monthly_deposit(remaining_amount, monthly_rate, months_left)
        current = goal.current_monthly_contribution

        if required <= current * _MEANINGFUL_CONTRIBUTION_GAP_RATIO:
            continue  # already on track

        delta = round(required - current, 2)

        # Heuristic goal-success probability -- overwritten with a real Monte
        # Carlo figure in `enrich_with_projections` when #843 is available.
        p_current = min(current / required, 1.0) if required > 0 else 1.0
        p_after = 1.0  # deterministic assumption: hitting the required deposit meets the goal
        prob_delta = round(p_after - p_current, 4)

        months_at_current = months_to_reach_target(remaining_amount, monthly_rate, current)
        time_saved = None
        if months_at_current is not None:
            time_saved = round(max(months_at_current - months_left, 0.0), 1)

        summary = (
            f'Increase your monthly deposit toward "{goal.name}" from '
            f"${current:.2f} to ${required:.2f} to reach ${goal.target_amount:.2f} "
            f"within the {months_left}-month deadline (${delta:.2f} more per month)."
        )
        candidates.append(
            RecommendationCandidate(
                candidate_id=_candidate_key("increase_contribution", goal.goal_id),
                action_type="increase_contribution",
                title=f"Boost contributions to {goal.name}",
                summary=summary,
                target_id=goal.goal_id,
                impact=RecommendationImpact(
                    goal_success_probability_delta=prob_delta,
                    time_saved_months=time_saved,
                    projection_source="heuristic",
                ),
                risk_context=None,
                priority_score=abs(delta) + prob_delta * 100.0,
            )
        )
    return candidates


def generate_yield_move_candidates(
    positions: list[VaultPosition],
    available: list[AvailableVault],
    risk_tolerance: str,
    *,
    horizon_months: int = 12,
) -> list[RecommendationCandidate]:
    """Recommend moving an idle/low-yield balance to a higher-APY vault.

    Prefers the Go API's own computed rebalance suggestion (already
    deterministic, backend-verified) when present; falls back to comparing
    against the best available vault within the user's risk tolerance.
    """
    candidates: list[RecommendationCandidate] = []
    risk_cap = _RISK_TOLERANCE_CAPS.get(risk_tolerance, _RISK_TOLERANCE_CAPS["moderate"])

    for position in positions:
        if position.balance_usd <= 0:
            continue

        suggestion = position.rebalance_suggestion or {}
        if suggestion.get("has_suggestion"):
            gain_pct = float(suggestion.get("expected_apy_gain_pct", 0.0) or 0.0)
            if gain_pct <= 0:
                continue
            additional_yield = round(
                position.balance_usd * (gain_pct / 100.0) * (horizon_months / 12.0), 2
            )
            if additional_yield <= 0:
                continue
            reason = str(suggestion.get("reason", "")).strip()
            risk_context = (
                "Rebalancing shifts allocation across protocols; review the "
                "suggested split before moving funds."
                + (f" {reason}" if reason else "")
            )
            summary = (
                f'Rebalance "{position.name}" for an estimated +{gain_pct:.2f}% APY '
                f"gain, worth about ${additional_yield:.2f} over {horizon_months} months."
            )
            candidates.append(
                RecommendationCandidate(
                    candidate_id=_candidate_key("move_to_higher_yield", position.vault_id),
                    action_type="move_to_higher_yield",
                    title=f"Rebalance {position.name}",
                    summary=summary,
                    target_id=position.vault_id,
                    impact=RecommendationImpact(
                        additional_yield_usdc=additional_yield,
                        projection_source="rebalance_service",
                    ),
                    risk_context=risk_context,
                    priority_score=additional_yield,
                )
            )
            continue

        pool = [
            v
            for v in available
            if v.risk_tier_score <= risk_cap or risk_tolerance == "aggressive"
        ]
        if not pool:
            pool = list(available)
        if not pool:
            continue
        best = max(pool, key=lambda v: v.apy)
        apy_delta = best.apy - position.apy
        if apy_delta < _MEANINGFUL_APY_DELTA_PCT:
            continue
        additional_yield = round(
            position.balance_usd * (apy_delta / 100.0) * (horizon_months / 12.0), 2
        )
        if additional_yield <= 0:
            continue
        risk_context = (
            f"{best.name} carries a risk score of {best.risk_tier_score:.0f}/100; "
            "higher yield generally carries higher protocol, liquidity, or "
            "concentration risk than your current allocation."
        )
        summary = (
            f'Move ${position.balance_usd:.2f} from "{position.name}" '
            f"({position.apy:.2f}% APY) to \"{best.name}\" ({best.apy:.2f}% APY) for "
            f"about ${additional_yield:.2f} more over {horizon_months} months."
        )
        candidates.append(
            RecommendationCandidate(
                candidate_id=_candidate_key("move_to_higher_yield", position.vault_id),
                action_type="move_to_higher_yield",
                title=f"Move idle balance to {best.name}",
                summary=summary,
                target_id=position.vault_id,
                impact=RecommendationImpact(
                    additional_yield_usdc=additional_yield,
                    projection_source="heuristic",
                ),
                risk_context=risk_context,
                priority_score=additional_yield,
            )
        )
    return candidates


def generate_term_lock_candidates(
    positions: list[VaultPosition],
    available: list[AvailableVault],
    *,
    horizon_months: int = 12,
) -> list[RecommendationCandidate]:
    """Recommend locking a currently-flexible balance into a fixed-term vault
    for a higher APY. Only fires when a real, fetched fixed-term vault (by
    Nester's own naming convention -- "Fixed-30d", "Fixed-90d") offers a
    meaningfully higher APY than the user's current flexible position."""
    candidates: list[RecommendationCandidate] = []
    locked_options = [
        v for v in available if "fixed" in v.name.lower() or "lock" in v.name.lower()
    ]
    if not locked_options:
        return candidates

    for position in positions:
        if position.balance_usd <= 0 or position.lock_period_days > 0:
            continue  # only offer this for currently-flexible balances
        best_lock = max(locked_options, key=lambda v: v.apy)
        apy_delta = best_lock.apy - position.apy
        if apy_delta < _MEANINGFUL_APY_DELTA_PCT:
            continue
        additional_yield = round(
            position.balance_usd * (apy_delta / 100.0) * (horizon_months / 12.0), 2
        )
        if additional_yield <= 0:
            continue
        risk_context = (
            f"{best_lock.name} locks funds for a fixed term -- early withdrawal is "
            f"restricted or penalized -- and carries a risk score of "
            f"{best_lock.risk_tier_score:.0f}/100."
        )
        summary = (
            f'Lock a portion of "{position.name}" into "{best_lock.name}" '
            f"({best_lock.apy:.2f}% APY vs {position.apy:.2f}% flexible) for about "
            f"${additional_yield:.2f} more over {horizon_months} months."
        )
        candidates.append(
            RecommendationCandidate(
                candidate_id=_candidate_key("lock_for_term_boost", position.vault_id),
                action_type="lock_for_term_boost",
                title=f"Lock funds in {best_lock.name}",
                summary=summary,
                target_id=position.vault_id,
                impact=RecommendationImpact(
                    additional_yield_usdc=additional_yield,
                    projection_source="heuristic",
                ),
                risk_context=risk_context,
                priority_score=additional_yield,
            )
        )
    return candidates


def generate_consolidation_candidates(
    goals: list[GoalContext], *, now: Optional[datetime] = None
) -> list[RecommendationCandidate]:
    """Recommend temporarily redirecting other goals' contribution capacity
    toward the nearest-deadline goal (a savings "snowball"), when doing so
    demonstrably shortens the time to reach it."""
    candidates: list[RecommendationCandidate] = []
    active = [g for g in goals if g.target_amount > g.current_amount]
    if len(active) < 2:
        return candidates

    active_sorted = sorted(active, key=lambda g: g.deadline)
    target = active_sorted[0]
    others = active_sorted[1:]

    remaining = max(target.target_amount - target.current_amount, 0.0)
    monthly_rate = target.apy / 12.0
    months_left = target.months_remaining(now)

    own_contribution = target.current_monthly_contribution
    pooled_contribution = own_contribution + sum(g.current_monthly_contribution for g in others)
    if pooled_contribution <= own_contribution:
        return candidates

    months_alone = months_to_reach_target(remaining, monthly_rate, own_contribution)
    months_pooled = months_to_reach_target(remaining, monthly_rate, pooled_contribution)
    if months_alone is None or months_pooled is None:
        return candidates

    time_saved = round(max(months_alone - months_pooled, 0.0), 1)
    if time_saved <= 0:
        return candidates

    other_names = ", ".join(f'"{g.name}"' for g in others)
    summary = (
        f"Temporarily redirect contributions from {other_names} toward "
        f'"{target.name}" to reach its ${target.target_amount:.2f} target about '
        f"{time_saved:.1f} months sooner (within {months_left} months), then "
        "resume the other goal(s)."
    )
    candidates.append(
        RecommendationCandidate(
            candidate_id=_candidate_key(
                "consolidate_goals",
                "+".join([target.goal_id] + [g.goal_id for g in others]),
            ),
            action_type="consolidate_goals",
            title=f"Consolidate contributions toward {target.name}",
            summary=summary,
            target_id=target.goal_id,
            impact=RecommendationImpact(
                time_saved_months=time_saved,
                projection_source="heuristic",
            ),
            risk_context=None,
            priority_score=time_saved,
        )
    )
    return candidates


def build_candidates(
    context: EngineContext, *, now: Optional[datetime] = None
) -> list[RecommendationCandidate]:
    candidates: list[RecommendationCandidate] = []
    candidates += generate_contribution_candidates(context.goals, now=now)
    candidates += generate_yield_move_candidates(
        context.positions, context.available, context.risk_tolerance
    )
    candidates += generate_term_lock_candidates(context.positions, context.available)
    candidates += generate_consolidation_candidates(context.goals, now=now)
    return candidates


# ---------------------------------------------------------------------------
# Monte Carlo enrichment (#843 integration point)
# ---------------------------------------------------------------------------


async def enrich_with_projections(
    candidates: list[RecommendationCandidate],
    goals: list[GoalContext],
    provider: ProjectionProvider,
    *,
    now: Optional[datetime] = None,
) -> list[RecommendationCandidate]:
    """Replace heuristic goal-success-probability figures with real Monte
    Carlo numbers (#843) when the projection service has data for that goal.

    Degrades silently to the heuristic value already computed whenever the
    provider returns None (route not deployed yet, network error, or an
    unexpected shape).
    """
    goals_by_id = {g.goal_id: g for g in goals}
    enriched: list[RecommendationCandidate] = []
    for candidate in candidates:
        if candidate.action_type != "increase_contribution" or not candidate.target_id:
            enriched.append(candidate)
            continue
        goal = goals_by_id.get(candidate.target_id)
        if goal is None:
            enriched.append(candidate)
            continue

        remaining_amount = max(goal.target_amount - goal.current_amount, 0.0)
        months_left = goal.months_remaining(now)
        monthly_rate = goal.apy / 12.0
        required = required_monthly_deposit(remaining_amount, monthly_rate, months_left)
        current = goal.current_monthly_contribution

        try:
            projection = await provider.fetch_goal_projection(
                candidate.target_id,
                initial_deposit=goal.current_amount,
                current_monthly_contribution=current,
                required_monthly_contribution=required,
                apy=goal.apy,
                period_months=months_left,
                target_amount=goal.target_amount,
                deadline_months=months_left,
            )
        except Exception:
            logger.exception("projection provider raised for goal %s", candidate.target_id)
            projection = None
        if not projection:
            enriched.append(candidate)
            continue
        try:
            delta = float(projection["success_probability_delta"])
        except (KeyError, TypeError, ValueError):
            enriched.append(candidate)
            continue
        new_impact = candidate.impact.model_copy(
            update={
                "goal_success_probability_delta": round(delta, 4),
                "projection_source": "monte_carlo",
            }
        )
        enriched.append(candidate.model_copy(update={"impact": new_impact}))
    return enriched


# ---------------------------------------------------------------------------
# Dismissed/acted-on filtering and ranking
# ---------------------------------------------------------------------------


def filter_and_rank_candidates(
    candidates: list[RecommendationCandidate],
    user_id: str,
    engagement_store: EngagementStore,
) -> list[RecommendationCandidate]:
    """Drop dismissed candidates and boost the deterministic priority score of
    action types the user has previously acted on. This is deterministic
    candidate ranking informed by retrieved engagement history -- not an
    opaque model decision."""
    engagement = engagement_store.get_all(user_id)
    acted_on_types: set[str] = {
        key.split(":", 1)[0]
        for key, info in engagement.items()
        if info.get("engagement") == "acted_on"
    }

    kept: list[RecommendationCandidate] = []
    for candidate in candidates:
        info = engagement.get(candidate.candidate_id)
        if info and info.get("engagement") == "dismissed":
            continue  # never re-recommend a dismissed candidate
        score = candidate.priority_score
        if candidate.action_type in acted_on_types:
            score *= 1.25
        kept.append(candidate.model_copy(update={"priority_score": score}))

    kept.sort(key=lambda c: c.priority_score, reverse=True)
    return kept


# ---------------------------------------------------------------------------
# LLM selection + fabrication guard
# ---------------------------------------------------------------------------

SELECTION_SYSTEM_PROMPT = """You are Prometheus, Nester's savings recommendation assistant.

You will be given a list of candidate actions. Every number attached to a
candidate (dollar amounts, percentages, months, probabilities) was computed by
deterministic backend code. You did NOT compute them and must never invent,
adjust, round differently, or add any new number of your own.

Your job:
1. Select the 2-4 candidates most relevant to this user, given their context.
2. Order them by likely impact and fit for this user.
3. Write a short (1-2 sentence), plain-language explanation for each selected
   candidate, using ONLY the numbers given in that candidate's data. Every
   figure in your explanation MUST appear (verbatim or equivalently rounded)
   in the candidate's summary or impact fields.
4. Never state a number that is not in the candidate you are explaining. If
   you want to convey a number you are unsure of, describe it qualitatively
   instead of stating a figure.
5. Yield-related recommendations already carry risk context in their data --
   reflect that risk in your explanation; never soften or omit it.
6. All projections are probabilities, not guarantees -- phrase them as such
   ("could", "is projected to", "an estimated"), never as certainties.

Call the select_recommendations tool with your selection. Reference
candidates ONLY by their candidate_id from the list provided; never invent a
candidate_id."""


def _build_tool_schema(candidate_ids: list[str]) -> ToolParam:
    return {
        "name": "select_recommendations",
        "description": (
            "Select, order, and explain the most relevant savings recommendations."
        ),
        "input_schema": {
            "type": "object",
            "properties": {
                "selections": {
                    "type": "array",
                    "description": (
                        "2-4 selected candidates, ordered by priority "
                        "(most important first)."
                    ),
                    "items": {
                        "type": "object",
                        "properties": {
                            "candidate_id": {
                                "type": "string",
                                "enum": candidate_ids,
                                "description": "Must exactly match a provided candidate_id.",
                            },
                            "explanation": {
                                "type": "string",
                                "description": (
                                    "1-2 sentence plain-language explanation using "
                                    "only the candidate's own numbers."
                                ),
                            },
                        },
                        "required": ["candidate_id", "explanation"],
                    },
                }
            },
            "required": ["selections"],
        },
    }


def _grounded_numbers(candidate: RecommendationCandidate) -> set[str]:
    """Every normalized number this candidate legitimately carries -- the set
    the fabrication guard checks LLM prose against."""
    text = f"{candidate.summary} {candidate.risk_context or ''}"
    numbers = extract_numbers(text)
    for value in (
        candidate.impact.goal_success_probability_delta,
        candidate.impact.additional_yield_usdc,
        candidate.impact.time_saved_months,
    ):
        if value is None:
            continue
        for rendering in (value, abs(value), value * 100, abs(value) * 100):
            numbers.update(extract_numbers(str(round(rendering, 2))))
    return numbers


def _validate_selection(
    selections: list[dict[str, Any]],
    candidates_by_id: dict[str, RecommendationCandidate],
) -> tuple[bool, list[str]]:
    """Return (ok, violations). ok is False if any selection references an
    unknown candidate_id or states a number not grounded in that candidate."""
    violations: list[str] = []
    for item in selections:
        cid = str(item.get("candidate_id", ""))
        candidate = candidates_by_id.get(cid)
        if candidate is None:
            violations.append(f"unknown candidate_id: {cid}")
            continue
        explanation = str(item.get("explanation", ""))
        grounded = _grounded_numbers(candidate)
        stated = extract_numbers(explanation)
        unsupported = stated - grounded
        if unsupported:
            violations.append(f"{cid}: unsupported numbers {sorted(unsupported)}")
    return (len(violations) == 0), violations


def _ensure_risk_context(candidate: RecommendationCandidate) -> Optional[str]:
    if candidate.action_type in _YIELD_ACTION_TYPES:
        return candidate.risk_context or _DEFAULT_YIELD_RISK_CONTEXT
    return candidate.risk_context


def _build_recommendation_set(
    selections: list[dict[str, Any]],
    candidates_by_id: dict[str, RecommendationCandidate],
    context: EngineContext,
) -> SavingsRecommendationSet:
    items: list[SavingsRecommendationItem] = []
    for idx, sel in enumerate(selections[:_SELECTION_LIMIT], start=1):
        candidate = candidates_by_id.get(str(sel.get("candidate_id", "")))
        if candidate is None:
            continue
        items.append(
            SavingsRecommendationItem(
                candidate_id=candidate.candidate_id,
                action_type=candidate.action_type,
                title=candidate.title,
                explanation=str(sel.get("explanation", candidate.summary)).strip()
                or candidate.summary,
                impact=candidate.impact,
                risk_context=_ensure_risk_context(candidate),
                priority=idx,
            )
        )
    return SavingsRecommendationSet(
        user_id=context.user_id,
        recommendations=items,
        data_freshness=context.data_freshness,
    )


def _fallback_recommendation_set(
    candidates: list[RecommendationCandidate],
    context: EngineContext,
    limit: int = _SELECTION_LIMIT,
) -> SavingsRecommendationSet:
    """Deterministic, template-only fallback -- guarantees the response never
    contains an invented figure, even if the LLM call fails entirely."""
    items = [
        SavingsRecommendationItem(
            candidate_id=c.candidate_id,
            action_type=c.action_type,
            title=c.title,
            explanation=c.summary,
            impact=c.impact,
            risk_context=_ensure_risk_context(c),
            priority=idx,
        )
        for idx, c in enumerate(candidates[:limit], start=1)
    ]
    return SavingsRecommendationSet(
        user_id=context.user_id,
        recommendations=items,
        data_freshness=context.data_freshness,
    )


async def select_and_explain(
    candidates: list[RecommendationCandidate],
    context: EngineContext,
) -> SavingsRecommendationSet:
    if not candidates:
        return SavingsRecommendationSet(
            user_id=context.user_id, recommendations=[], data_freshness=context.data_freshness
        )

    prompt_candidates = candidates[:_MAX_CANDIDATES_IN_PROMPT]
    candidates_by_id = {c.candidate_id: c for c in prompt_candidates}
    candidate_ids = list(candidates_by_id.keys())
    tool = _build_tool_schema(candidate_ids)

    candidate_lines = []
    for c in prompt_candidates:
        impact_parts = []
        if c.impact.goal_success_probability_delta is not None:
            impact_parts.append(
                f"success probability change: {c.impact.goal_success_probability_delta:+.2%}"
            )
        if c.impact.additional_yield_usdc is not None:
            impact_parts.append(f"additional yield: ${c.impact.additional_yield_usdc:.2f}")
        if c.impact.time_saved_months is not None:
            impact_parts.append(f"time saved: {c.impact.time_saved_months:.1f} months")
        line = (
            f"- id={c.candidate_id} | type={c.action_type} | {c.summary} | "
            f"impact: {', '.join(impact_parts) or 'n/a'}"
        )
        if c.risk_context:
            line += f" | risk: {c.risk_context}"
        candidate_lines.append(line)

    prompt = (
        f"User risk tolerance: {context.risk_tolerance}.\n"
        f"Data freshness: {context.data_freshness}.\n\n"
        "Candidate actions (each already computed deterministically):\n"
        + "\n".join(candidate_lines)
    )

    messages: list[MessageParam] = [{"role": "user", "content": prompt}]
    client = get_client()
    tool_choice: ToolChoiceToolParam = {"type": "tool", "name": "select_recommendations"}

    for attempt in range(_MAX_REGENERATE_ATTEMPTS + 1):
        try:
            response = await client.messages.create(
                model=settings.anthropic_model,
                max_tokens=SELECT_MAX_TOKENS,
                system=SELECTION_SYSTEM_PROMPT,
                tools=[tool],
                tool_choice=tool_choice,
                messages=messages,
            )
            tool_use = next(
                (b for b in response.content if getattr(b, "type", None) == "tool_use"), None
            )
            if tool_use is None:
                raise ValueError("model did not return a tool_use block")
            # duck-typed rather than isinstance(b, ToolUseBlock): tests exercise
            # this against SimpleNamespace fakes (matching this codebase's
            # existing Anthropic-mocking convention, see prometheus.py's tests),
            # not real SDK block instances.
            tool_input = cast(dict[str, Any], getattr(tool_use, "input", None))
            selections = list(tool_input.get("selections", []))
            if not selections:
                raise ValueError("model returned no selections")

            ok, violations = _validate_selection(selections, candidates_by_id)
            if ok:
                return _build_recommendation_set(selections, candidates_by_id, context)

            logger.warning(
                "recommendation fabrication guard rejected LLM output (attempt %d): %s",
                attempt,
                violations,
            )
            if attempt < _MAX_REGENERATE_ATTEMPTS:
                messages.append({"role": "assistant", "content": response.content})
                messages.append(
                    {
                        "role": "user",
                        "content": (
                            "Your previous selection stated figures not present in "
                            f"the candidate data: {violations}. Call "
                            "select_recommendations again using ONLY the exact "
                            "numbers given for each candidate."
                        ),
                    }
                )
        except Exception:
            logger.exception("recommendation selection call failed (attempt %d)", attempt)
            break

    logger.warning(
        "falling back to deterministic recommendation set for user %s", context.user_id
    )
    return _fallback_recommendation_set(prompt_candidates, context)


# ---------------------------------------------------------------------------
# Caching -- cadence-based generation, invalidated on material context change
# ---------------------------------------------------------------------------


def _get_redis() -> Any:
    global _redis_client, _redis_available
    if _redis_client is not None:
        return _redis_client if _redis_available else None
    try:
        import redis as _redis

        _redis_client = _redis.from_url(settings.redis_url, decode_responses=True)
        _redis_client.ping()
        _redis_available = True
    except Exception as exc:
        logger.warning("recommendation cache: redis unavailable (%s), using in-memory", exc)
        _redis_available = False
    return _redis_client if _redis_available else None


def _cache_get(user_id: str) -> Optional[dict[str, Any]]:
    key = _RECO_KEY_PREFIX + user_id
    r = _get_redis()
    if r is not None:
        try:
            raw = r.get(key)
            if raw:
                return dict(json.loads(raw))
        except Exception as exc:
            logger.warning("recommendation cache redis get failed: %s", exc)
    entry = _mem_reco_cache.get(user_id)
    if entry and time.monotonic() < entry[1]:
        return entry[0]
    return None


def _cache_set(user_id: str, payload: dict[str, Any]) -> None:
    key = _RECO_KEY_PREFIX + user_id
    r = _get_redis()
    if r is not None:
        try:
            r.setex(key, _RECO_CACHE_TTL, json.dumps(payload))
            return
        except Exception as exc:
            logger.warning("recommendation cache redis set failed: %s", exc)
    _mem_reco_cache[user_id] = (payload, time.monotonic() + _RECO_CACHE_TTL)


def _context_fingerprint(context: EngineContext) -> str:
    """A short hash of the values that matter for regeneration -- goal
    balances/targets/deadlines and vault balances/APYs. Cheap to recompute
    every request; a mismatch against the cached fingerprint means the
    context changed materially, so we regenerate instead of serving stale
    cache."""
    parts = [
        f"g:{g.goal_id}:{g.target_amount}:{g.current_amount}:"
        f"{g.deadline.isoformat()}:{g.avg_weekly_deposit}"
        for g in context.goals
    ] + [f"v:{p.vault_id}:{p.balance_usd}:{p.apy}" for p in context.positions]
    raw = "|".join(sorted(parts))
    return hashlib.sha256(raw.encode()).hexdigest()[:16]


# ---------------------------------------------------------------------------
# Engine
# ---------------------------------------------------------------------------


def _parse_deadline(raw: Any) -> datetime:
    if isinstance(raw, datetime):
        return raw if raw.tzinfo else raw.replace(tzinfo=timezone.utc)
    text = str(raw)
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    return datetime.fromisoformat(text)


class RecommendationEngine:
    def __init__(
        self,
        fetcher: Optional[VaultContextFetcher] = None,
        projection_provider: Optional[ProjectionProvider] = None,
        engagement_store: Optional[EngagementStore] = None,
    ) -> None:
        self.fetcher = fetcher or VaultContextFetcher(
            api_base_url=settings.nester_api_base_url,
            service_api_key=settings.nester_service_api_key,
        )
        self.projection_provider: ProjectionProvider = (
            projection_provider or get_projection_provider()
        )
        self.engagement_store: EngagementStore = engagement_store or default_engagement_store

    async def gather_context(
        self, user_id: str, risk_tolerance: str = "moderate"
    ) -> EngineContext:
        user_vaults = await self.fetcher.fetch_user_vaults(user_id)
        available_vaults = await self.fetcher.fetch_available_vaults()
        goals_raw = await self.fetcher.fetch_savings_goals(user_id)

        positions: list[VaultPosition] = []
        for v in user_vaults:
            vault_id = str(v.get("id", "")).strip()
            rebalance: Optional[dict[str, Any]] = None
            if vault_id:
                rebalance = await self.fetcher.fetch_vault_rebalance_suggestion(
                    vault_id, user_id
                )
            positions.append(
                VaultPosition(
                    vault_id=vault_id,
                    name=str(v.get("name", "Vault")),
                    balance_usd=float(v.get("balance_usd", 0) or 0),
                    apy=float(v.get("apy", 0) or 0),
                    lock_period_days=int(v.get("lock_period_days", 0) or 0),
                    rebalance_suggestion=rebalance or None,
                )
            )

        available: list[AvailableVault] = []
        for v in available_vaults:
            vault_id = str(v.get("id", "")).strip()
            score = 100.0
            if vault_id:
                risk = await self.fetcher.fetch_vault_risk(vault_id)
                if risk:
                    score = float(risk.get("overall", 100.0))
            available.append(
                AvailableVault(
                    vault_id=vault_id,
                    name=str(v.get("name", "Vault")),
                    apy=float(v.get("apy", 0) or 0),
                    risk_tier_score=score,
                )
            )

        goals: list[GoalContext] = []
        for g in goals_raw:
            try:
                deadline = _parse_deadline(g.get("deadline"))
            except Exception:
                continue
            goals.append(
                GoalContext(
                    goal_id=str(g.get("id", "")),
                    name=str(g.get("name") or g.get("description") or "Savings Goal"),
                    target_amount=float(g.get("target_amount", 0) or 0),
                    current_amount=float(g.get("current_amount", 0) or 0),
                    currency=str(g.get("currency", "USDC")),
                    deadline=deadline,
                    apy=float(g.get("apy", 0.08) or 0.08),
                    avg_weekly_deposit=float(g.get("avg_weekly_deposit", 0) or 0),
                    vault_id=(str(g["vault_id"]) if g.get("vault_id") else None),
                )
            )

        data_freshness = (
            f"vaults={'live' if user_vaults else 'unavailable'}, "
            f"goals={'live' if goals_raw else 'unavailable'}, "
            f"available_vaults={'live' if available_vaults else 'unavailable'}"
        )
        return EngineContext(
            user_id=user_id,
            goals=goals,
            positions=positions,
            available=available,
            risk_tolerance=risk_tolerance,
            data_freshness=data_freshness,
        )

    async def generate_for_user(
        self,
        user_id: str,
        risk_tolerance: str = "moderate",
        force_refresh: bool = False,
    ) -> SavingsRecommendationSet:
        context = await self.gather_context(user_id, risk_tolerance)
        fingerprint = _context_fingerprint(context)

        if not force_refresh:
            cached = _cache_get(user_id)
            if cached and cached.get("fingerprint") == fingerprint:
                return SavingsRecommendationSet.model_validate(cached["result"])

        candidates = build_candidates(context)
        candidates = await enrich_with_projections(
            candidates, context.goals, self.projection_provider
        )
        candidates = filter_and_rank_candidates(candidates, user_id, self.engagement_store)
        result = await select_and_explain(candidates, context)

        _cache_set(
            user_id,
            {"fingerprint": fingerprint, "result": result.model_dump(mode="json")},
        )
        return result

    def dismiss(self, user_id: str, candidate_id: str) -> None:
        self.engagement_store.record(user_id, candidate_id, "dismissed")

    def mark_acted_on(self, user_id: str, candidate_id: str) -> None:
        self.engagement_store.record(user_id, candidate_id, "acted_on")


engine: RecommendationEngine = RecommendationEngine()
