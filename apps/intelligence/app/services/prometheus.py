"""Core Prometheus AI logic — system prompt, streaming chat, and structured analysis."""

import json
import logging
import time
from collections.abc import AsyncIterator
from datetime import datetime, timezone
from typing import Any, Literal, Optional, cast

import aiohttp
import anthropic

from app.config import settings
from app.models.coaching import CoachingRequest, CoachingResponse
from app.models.portfolio import (
    AllocationItem,
    PortfolioAnalysisResponse,
    PortfolioBreakdown,
)
from app.models.recommendation import (
    ConfidenceLevel,
    Recommendation,
    RecommendedVault,
    VaultRecommendationRequest,
    VaultRecommendationResponse,
)
from app.services.coingecko import get_client as get_coingecko_client
from app.services.conversation_store import store as conversation_store
from app.services.defillama import get_client as get_defillama_client
from app.services.vault_context import VaultContextFetcher

logger = logging.getLogger(__name__)

CHAT_MAX_TOKENS = 1024
ANALYZE_MAX_TOKENS = 800
RECOMMEND_MAX_TOKENS = 900

_CONTEXT_CACHE_TTL = 60  # seconds
_CONTEXT_KEY_PREFIX = "prometheus:ctx:"
_RISK_LIMITS: dict[str, float] = {
    "conservative": 35.0,
    "moderate": 65.0,
    "aggressive": 100.0,
}

_client: Optional[anthropic.AsyncAnthropic] = None
_vault_context_fetcher: Optional[VaultContextFetcher] = None
_redis_client: Any = None
_redis_available: bool = False

# In-process fallback cache: {user_id: (context_dict, expires_at)}
_mem_context_cache: dict[str, tuple[dict[str, Any], float]] = {}


def get_client() -> anthropic.AsyncAnthropic:
    global _client
    if _client is None:
        _client = anthropic.AsyncAnthropic(api_key=settings.anthropic_api_key)
    return _client


def get_vault_context_fetcher() -> VaultContextFetcher:
    global _vault_context_fetcher
    if _vault_context_fetcher is None:
        _vault_context_fetcher = VaultContextFetcher(
            api_base_url=settings.nester_api_base_url,
            service_api_key=settings.nester_service_api_key,
        )
    return _vault_context_fetcher


def _get_redis() -> Any:
    """Return a shared Redis client, or None if unavailable."""
    global _redis_client, _redis_available
    if _redis_client is not None:
        return _redis_client if _redis_available else None
    try:
        import redis as _redis

        _redis_client = _redis.from_url(settings.redis_url, decode_responses=True)
        _redis_client.ping()
        _redis_available = True
        logger.info("prometheus context cache: redis connected")
    except Exception as exc:
        logger.warning(
            "prometheus context cache: redis unavailable (%s), using in-memory", exc
        )
        _redis_available = False
    return _redis_client if _redis_available else None


def _cache_get(user_id: str) -> Optional[dict[str, Any]]:
    key = _CONTEXT_KEY_PREFIX + user_id
    r = _get_redis()
    if r is not None:
        try:
            raw = r.get(key)
            if raw:
                return dict(json.loads(raw))
        except Exception as exc:
            logger.warning("context cache redis get failed: %s", exc)
    # In-process fallback
    entry = _mem_context_cache.get(user_id)
    if entry and time.monotonic() < entry[1]:
        return entry[0]
    return None


def _cache_set(user_id: str, context: dict[str, Any]) -> None:
    key = _CONTEXT_KEY_PREFIX + user_id
    r = _get_redis()
    if r is not None:
        try:
            r.setex(key, _CONTEXT_CACHE_TTL, json.dumps(context))
            return
        except Exception as exc:
            logger.warning("context cache redis set failed: %s", exc)
    # In-process fallback
    _mem_context_cache[user_id] = (context, time.monotonic() + _CONTEXT_CACHE_TTL)


async def fetch_user_context(
    user_id: str, api_base_url: str, service_api_key: str
) -> dict[str, Any]:
    """Fetch user vaults, balances, allocations, and recent APY snapshots.

    Returns a dict with 'vaults', 'performance', and 'fetched_at'.
    Raises on network or auth errors so the caller can fall back gracefully.
    """
    headers = {
        "Authorization": f"Bearer {service_api_key}",
        "Content-Type": "application/json",
    }
    base = api_base_url.rstrip("/")

    async with aiohttp.ClientSession() as session:
        # Fetch vaults scoped to this user
        async with session.get(
            f"{base}/api/v1/user-vaults/{user_id}",
            headers=headers,
            timeout=aiohttp.ClientTimeout(total=5),
        ) as resp:
            raw_vaults = await resp.json() if resp.status == 200 else {}
            if isinstance(raw_vaults, dict) and raw_vaults.get("success"):
                vaults_data = raw_vaults.get("data") or {}
            else:
                vaults_data = raw_vaults

        # Fetch recent performance snapshots (best-effort)
        async with session.get(
            f"{base}/api/v1/performance/snapshots",
            headers=headers,
            timeout=aiohttp.ClientTimeout(total=5),
        ) as resp:
            raw_perf = await resp.json() if resp.status == 200 else {}
            if isinstance(raw_perf, dict) and raw_perf.get("success"):
                performance_data = raw_perf.get("data") or {}
            else:
                performance_data = raw_perf

        savings_goals: list[dict[str, Any]] = []
        async with session.get(
            f"{base}/api/v1/users/savings-goals",
            headers={**headers, "X-User-Id": user_id},
            timeout=aiohttp.ClientTimeout(total=5),
        ) as resp:
            if resp.status == 200:
                payload = await resp.json()
                if isinstance(payload, dict) and payload.get("success"):
                    savings_goals = payload.get("data") or []

    return {
        "vaults": vaults_data,
        "performance": performance_data,
        "savings_goals": savings_goals,
        "fetched_at": datetime.now(timezone.utc).isoformat(),
    }


async def _get_cached_user_context(user_id: str) -> Optional[dict[str, Any]]:
    """Return cached user context, or fetch and cache it. Returns None on failure."""
    cached = _cache_get(user_id)
    if cached is not None:
        return cached
    try:
        ctx = await fetch_user_context(
            user_id,
            settings.nester_api_base_url,
            settings.nester_service_api_key,
        )
        _cache_set(user_id, ctx)
        return ctx
    except Exception as exc:
        logger.warning("fetch_user_context failed for user %s: %s", user_id, exc)
        return None


# ---------------------------------------------------------------------------
# System prompt
# ---------------------------------------------------------------------------

SYSTEM_PROMPT = """You are Prometheus, the financial intelligence layer of Nester.

Nester is a yield-bearing savings platform built on the Stellar blockchain. It lets users in
Africa (primarily Nigeria, Ghana, and Kenya) deposit USDC or XLM into yield-generating vaults,
earn APY, and withdraw earnings directly to their local bank accounts (NGN, GHS, KES).

## Your role
Help users make smart decisions about their Nester vaults, understand their portfolio, and
optimise their yield strategy. You are knowledgeable about:
- DeFi yield strategies on Stellar
- Nester's vault risk tiers: Conservative, Balanced, Growth, DeFi500
- Stellar-native assets: USDC, XLM
- Offramp and settlement flow (crypto to NGN/GHS/KES)
- Savings goals and compounding yield

## Strict scope
You ONLY answer questions about:
- The user's Nester vaults, deposits, and portfolio
- Yield strategies, APYs, and vault risk tiers on Nester
- Savings goals and reaching them with Nester
- How offramp/settlement to local fiat works
- How Nester's contracts, fees, and mechanics work

If asked something outside this scope, respond warmly and briefly. Acknowledge the question,
explain you are focused on Nester savings and yield topics, and invite them to ask anything
about their portfolio, vaults, or yield strategy. Keep it friendly, not robotic.

## Vault tiers (reference)
- Conservative: Stablecoin-only, lowest risk, ~4-6% APY. Good for emergency funds.
- Balanced: Mix of stablecoin and blue-chip DeFi, ~8-12% APY. Good for medium-term goals.
- Growth: Higher-yield DeFi strategies, ~15-25% APY. Suited for long-term horizon with
  risk tolerance.
- DeFi500: Curated top-500 DeFi index exposure, ~20-30% APY. Highest risk, highest reward.

## Formatting rules
- Never use em dashes (the -- or long dash character). Use commas, colons, or plain periods instead.
- Separate distinct thoughts or points into their own paragraphs with a blank line between them.
- Keep responses concise. The user is reading a sidebar panel, not an article.
- Do not use bullet points for simple single-topic answers.

## Tone
Be direct, warm, and specific. Use plain language. When you recommend something, say why in
one sentence. Never start a sentence with an em dash."""


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _to_anthropic_messages(
    history: list[dict[str, str]],
) -> list[anthropic.types.MessageParam]:
    """Convert conversation store format to Anthropic message params.

    Conversation store uses {"role": "user"|"assistant", "content": str}.
    Anthropic uses the same role names.
    """
    return [
        {
            "role": cast(Literal["user", "assistant"], msg["role"]),
            "content": msg["content"],
        }
        for msg in history
    ]


# ---------------------------------------------------------------------------
# Streaming chat
# ---------------------------------------------------------------------------


async def stream_chat(user_id: str, message: str) -> AsyncIterator[str]:
    """Yield SSE-formatted data strings for a streaming Claude response.

    Each yielded string is formatted as `data: <text>\\n\\n`.
    A final `data: [DONE]\\n\\n` is yielded when the stream ends.
    """
    # Get conversation history
    history = conversation_store.get(user_id)
    conversation_store.append(user_id, "user", message)

    # Fetch vault context, market rates, and risk data
    vault_context_fetcher = get_vault_context_fetcher()
    vaults = await vault_context_fetcher.fetch_user_vaults(user_id)
    market_rates = await vault_context_fetcher.fetch_market_rates()

    # Fetch risk data for each vault
    risk_data = {}
    for vault in vaults:
        vault_id = vault.get("id")
        if vault_id:
            risk_info = await vault_context_fetcher.fetch_vault_risk(vault_id)
            if risk_info:
                risk_data[vault_id] = risk_info

    context_block = vault_context_fetcher.build_context_block(vaults, market_rates)
    risk_profile_block = vault_context_fetcher.build_risk_profile_block(
        vaults, risk_data
    )

    market_context_block = await _build_market_context_block()

    dynamic_system_prompt = f"""You are Prometheus, an AI financial advisor for the
Nester DeFi platform.

{context_block}

{risk_profile_block}

{market_context_block}

## Nester Platform Context
- Vault types: Flexible (no lock), Fixed-30d (30-day lock, higher APY),
  Fixed-90d (90-day lock, highest APY)
- Rebalancing threshold: triggered when allocation drift exceeds 10%
- Fee structure: 0.5% management fee on yield
- Protocols supported: Aave, Blend, Compound

Provide personalized, data-driven advice based on user positions
and current market conditions. Always cite specific numbers from their
portfolio."""

    # Fetch live portfolio context (60s Redis-backed cache).
    # Injected as a prepended user message so Claude can personalise responses
    # without the instruction appearing in the visible conversation history.
    # If the fetch fails, continue with static knowledge — no error surfaced.
    user_context = await _get_cached_user_context(user_id)
    context_injection: list[anthropic.types.MessageParam] = []
    if user_context:
        goals_block = ""
        active_goals = user_context.get("savings_goals") or []
        if active_goals:
            goals_block = (
                "\n\nActive savings goals (use for coaching and progress nudges):\n"
                + json.dumps(active_goals, indent=2)
            )
        context_injection = [
            {
                "role": "user",
                "content": (
                    "[PORTFOLIO CONTEXT — do not quote this back, "
                    "use it to personalise your response]\n"
                    + json.dumps(user_context, indent=2)
                    + goals_block
                ),
            }
        ]

    messages: list[anthropic.types.MessageParam] = (
        context_injection
        + _to_anthropic_messages(history)
        + [{"role": "user", "content": message}]
    )

    client = get_client()
    full_response = ""

    try:
        async with client.messages.stream(
            model=settings.anthropic_model,
            max_tokens=CHAT_MAX_TOKENS,
            system=dynamic_system_prompt,
            messages=messages,
        ) as stream:
            async for text in stream.text_stream:
                full_response += text
                safe = text.replace("\n", "\\n")
                yield f"data: {safe}\n\n"

        conversation_store.append(user_id, "assistant", full_response)
        yield "data: [DONE]\n\n"

    except Exception:
        logger.exception("Anthropic streaming error for user %s", user_id)
        yield "data: Sorry, I had trouble connecting. Please try again.\n\n"
        yield "data: [DONE]\n\n"


# ---------------------------------------------------------------------------
# Structured analysis (non-streaming)
# ---------------------------------------------------------------------------


def _json_strip(raw: str) -> str:
    return (
        raw.strip()
        .removeprefix("```json")
        .removeprefix("```")
        .removesuffix("```")
        .strip()
    )


async def generate_coaching(request: CoachingRequest) -> CoachingResponse:
    """Generate deposit schedule and progress assessment for a savings goal."""
    from app.models.coaching import DepositScheduleItem

    goal = request.goal
    portfolio = request.portfolio
    schema = (
        '{"progress_assessment": str, "deposit_schedule": '
        '[{"date": str, "amount_usdc": float, "note": str}], '
        '"nudges": [str], "confidence": "high"|"medium"|"low"}'
    )
    vaults_preview = json.dumps(portfolio.vaults[:5])
    prompt = (
        "You are Prometheus, a savings coach for Nester on Stellar. "
        f"Goal: target {goal.target_amount} {goal.currency}, deadline {goal.deadline}, "
        f"description: {goal.description or 'none'}. "
        f"Current progress: {goal.progress_pct:.1f}% ({goal.current_amount} saved). "
        f"Portfolio total USD: {portfolio.total_balance_usd}. Vaults: {vaults_preview}. "
        "Return a realistic deposit schedule from today until the deadline, with 3-8 installments. "
        "Include a short progress assessment and 2-3 motivational nudges. "
        f"Respond with JSON only matching: {schema}"
    )
    client = get_client()
    try:
        response = await client.messages.create(
            model=settings.anthropic_model,
            max_tokens=ANALYZE_MAX_TOKENS,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (
                b.text
                for b in response.content
                if isinstance(b, anthropic.types.TextBlock)
            ),
            "",
        )
        parsed = json.loads(_json_strip(text))
        schedule = [
            DepositScheduleItem(
                date=str(item.get("date", "")),
                amount_usdc=float(item.get("amount_usdc", 0)),
                note=item.get("note"),
            )
            for item in parsed.get("deposit_schedule", [])
        ]
        return CoachingResponse(
            progress_assessment=str(parsed.get("progress_assessment", "")),
            deposit_schedule=schedule,
            nudges=[str(n) for n in parsed.get("nudges", [])],
            confidence=str(parsed.get("confidence", "medium")),
        )
    except Exception:
        logger.exception("coaching generation failed")
        remaining = max(goal.target_amount - goal.current_amount, 0)
        return CoachingResponse(
            progress_assessment=(
                f"You are {goal.progress_pct:.0f}% toward your goal. "
                f"About {remaining:.0f} {goal.currency} left to save."
            ),
            deposit_schedule=[],
            nudges=["Keep making steady deposits to stay on track."],
            confidence="low",
        )


async def get_portfolio_insights(user_id: str) -> list[dict[str, Any]]:
    """Return 2 insight cards for the user's portfolio."""
    schema = (
        '[{"title": str, "body": str, "confidence": float,'
        ' "action": {"label": str, "href": str} | null}]'
    )
    prompt = (
        f"Generate 2 concise portfolio insight cards for a Nester user (id: {user_id}). "
        "Each card should have a short title, a one-sentence body, a confidence score "
        "(0.0–1.0), and optionally an action with a label and href. "
        "Focus on practical savings advice relevant to Nester vaults on Stellar. "
        f"Respond with a JSON array only, no markdown, matching this schema: {schema}"
    )

    client = get_client()
    try:
        response = await client.messages.create(
            model=settings.anthropic_model,
            max_tokens=ANALYZE_MAX_TOKENS,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (
                b.text
                for b in response.content
                if isinstance(b, anthropic.types.TextBlock)
            ),
            "",
        )
        return list(json.loads(_json_strip(text)))
    except Exception:
        logger.exception("Failed to get portfolio insights for user %s", user_id)
        return []


async def get_market_sentiment() -> dict[str, Any]:
    """Return a market sentiment summary for the Stellar DeFi / stablecoin space."""
    schema = (
        '{"signal": "bull"|"bear"|"neutral", "summary": str (1 sentence),'
        ' "confidence": float (0.0–1.0), "updatedAt": str (ISO timestamp now)}'
    )
    prompt = (
        "Give a brief market sentiment assessment for the Stellar DeFi and stablecoin "
        "yield space as it relates to Nester users in Africa. "
        f"Respond with JSON only, no markdown, matching this schema: {schema}"
    )

    client = get_client()
    try:
        response = await client.messages.create(
            model=settings.anthropic_model,
            max_tokens=200,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (
                b.text
                for b in response.content
                if isinstance(b, anthropic.types.TextBlock)
            ),
            "",
        )
        return dict(json.loads(_json_strip(text)))
    except Exception:
        logger.exception("Failed to get market sentiment")
        return {
            "signal": "neutral",
            "summary": "Sentiment data temporarily unavailable.",
            "confidence": 0.0,
            "updatedAt": "",
        }


async def _build_market_context_block() -> str:
    """Fetch live DeFiLlama + CoinGecko data and format as a system prompt block.

    Returns an empty string on total failure so the prompt degrades gracefully.
    """
    sections: list[str] = []

    try:
        cg = get_coingecko_client()
        prices = await cg.get_prices(["usd-coin", "stellar"])
        sentiment = await cg.get_market_sentiment()

        price_lines: list[str] = []
        usdc = prices.get("usd-coin")
        xlm = prices.get("stellar")
        if usdc:
            peg = "stable" if abs(usdc.usd - 1.0) < 0.005 else "off-peg"
            price_lines.append(
                f"- USDC: ${usdc.usd:.4f} ({peg}, 24h {usdc.usd_24h_change:+.2f}%)"
            )
        if xlm:
            price_lines.append(
                f"- XLM: ${xlm.usd:.4f} (24h {xlm.usd_24h_change:+.2f}%, "
                f"mktcap ${xlm.usd_market_cap:,.0f})"
            )

        if price_lines:
            sections.append("## Live Price Data\n" + "\n".join(price_lines))

        if sentiment.defi_market_cap_usd > 0:
            sections.append(
                f"## DeFi Market Sentiment\n"
                f"- Signal: {sentiment.signal.upper()}\n"
                f"- DeFi market cap: ${sentiment.defi_market_cap_usd:,.0f}\n"
                f"- DeFi dominance: {sentiment.defi_dominance_pct:.2f}%"
            )
    except Exception as exc:
        logger.warning("market context: coingecko fetch failed: %s", exc)

    try:
        dl = get_defillama_client()
        pools = await dl.get_yield_pools(chain="Stellar")
        if pools:
            top5 = sorted(pools, key=lambda p: p.get("apy") or 0, reverse=True)[:5]
            pool_lines = [
                f"- {p['project']} {p['symbol']}: {p['apy']:.2f}% APY, "
                f"TVL ${p['tvlUsd']:,.0f}"
                for p in top5
            ]
            sections.append(
                "## Top Stellar DeFi Pools (DeFiLlama)\n" + "\n".join(pool_lines)
            )
    except Exception as exc:
        logger.warning("market context: defillama fetch failed: %s", exc)

    return "\n\n".join(sections)


async def get_yield_recommendation() -> dict[str, Any]:
    """Return an AI-picked yield opportunity based on current DeFiLlama and CoinGecko data."""
    dl = get_defillama_client()
    cg = get_coingecko_client()

    pools: list[dict[str, Any]] = []
    prices: dict[str, Any] = {}
    sentiment_signal = "neutral"

    try:
        pools = await dl.get_yield_pools(chain="Stellar")
    except Exception as exc:
        logger.warning("get_yield_recommendation: defillama failed: %s", exc)

    try:
        raw_prices = await cg.get_prices(["usd-coin", "stellar"])
        prices = {
            k: {"usd": v.usd, "change_24h": v.usd_24h_change}
            for k, v in raw_prices.items()
        }
        raw_sentiment = await cg.get_market_sentiment()
        sentiment_signal = raw_sentiment.signal
    except Exception as exc:
        logger.warning("get_yield_recommendation: coingecko failed: %s", exc)

    top_pools_summary = ""
    if pools:
        top5 = sorted(pools, key=lambda p: p.get("apy") or 0, reverse=True)[:5]
        lines = [
            f"  - {p['project']} {p['symbol']}: APY {p['apy']:.2f}%, TVL ${p['tvlUsd']:,.0f}"
            for p in top5
        ]
        top_pools_summary = "Top Stellar pools:\n" + "\n".join(lines)

    schema = (
        '{"protocol": str, "symbol": str, "apy": float, "tvl_usd": float,'
        ' "rationale": str (1-2 sentences), "risk_level": "low"|"medium"|"high",'
        ' "confidence": float (0.0-1.0)}'
    )
    prompt = (
        "Based on current Stellar DeFi market conditions, pick the single best yield opportunity "
        "for a Nester user seeking risk-adjusted returns. "
        f"Market sentiment: {sentiment_signal}. "
        f"Prices: {json.dumps(prices)}. "
        f"{top_pools_summary}\n"
        f"Respond with JSON only, no markdown, matching this schema: {schema}"
    )

    client = get_client()
    try:
        response = await client.messages.create(
            model=settings.anthropic_model,
            max_tokens=ANALYZE_MAX_TOKENS,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (
                b.text
                for b in response.content
                if isinstance(b, anthropic.types.TextBlock)
            ),
            "",
        )
        return dict(json.loads(_json_strip(text)))
    except Exception:
        logger.exception("Failed to get yield recommendation")
        return {
            "protocol": "",
            "symbol": "",
            "apy": 0.0,
            "tvl_usd": 0.0,
            "rationale": "Recommendation temporarily unavailable.",
            "risk_level": "medium",
            "confidence": 0.0,
        }


async def get_vault_recommendations(vault_id: str) -> dict[str, Any]:
    """Return AI commentary and recommendations for a specific vault."""
    schema = (
        '{"vaultId": str, "commentary": str, "percentileRank": int (0-100),'
        ' "recommendations": [str], "confidence": float}'
    )
    prompt = (
        f"Give an AI commentary and recommendations for Nester vault id '{vault_id}'. "
        "Assume it is a yield-bearing Stellar vault. "
        "Be specific about what type of user this vault suits. "
        f"Respond with JSON only, no markdown, matching this schema: {schema}"
    )

    client = get_client()
    try:
        response = await client.messages.create(
            model=settings.anthropic_model,
            max_tokens=ANALYZE_MAX_TOKENS,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (
                b.text
                for b in response.content
                if isinstance(b, anthropic.types.TextBlock)
            ),
            "",
        )
        return dict(json.loads(_json_strip(text)))
    except Exception:
        logger.exception("Failed to get vault recommendations for vault %s", vault_id)
        return {
            "vaultId": vault_id,
            "commentary": "Recommendations temporarily unavailable.",
            "percentileRank": 0,
            "recommendations": [],
            "confidence": 0.0,
        }


def _confidence_from_sources(
    vaults_ready: bool,
    risks_ready: bool,
    market_ready: bool,
) -> tuple[ConfidenceLevel, str, str]:
    ready_count = sum((vaults_ready, risks_ready, market_ready))
    freshness_parts = [
        f"vaults={'live' if vaults_ready else 'unavailable'}",
        f"risk scores={'live' if risks_ready else 'partial'}",
        f"market rates={'live' if market_ready else 'unavailable'}",
    ]

    if ready_count == 3:
        return (
            "high",
            "Live vault APYs, risk scores, and market rates are all available.",
            ", ".join(freshness_parts),
        )
    if ready_count >= 1:
        return (
            "medium",
            "Some live feeds are missing, so the recommendation leans on partial live data.",
            ", ".join(freshness_parts),
        )
    return (
        "low",
        "No live data was available, so the response falls back to general guidance.",
        ", ".join(freshness_parts),
    )


def _estimate_expected_yield(
    deposit_usdc: float, time_horizon_months: int, weighted_apy: float
) -> float:
    projected = deposit_usdc * (weighted_apy / 100.0) * (time_horizon_months / 12.0)
    return round(max(projected, 0.0), 2)


def _rank_vaults(
    vaults: list[dict[str, Any]],
    risk_scores: dict[str, dict[str, Any]],
    risk_tolerance: str,
) -> list[dict[str, Any]]:
    risk_cap = _RISK_LIMITS.get(risk_tolerance, _RISK_LIMITS["moderate"])
    ranked: list[dict[str, Any]] = []

    for vault in vaults:
        vault_id = str(vault.get("id", "")).strip()
        if not vault_id:
            continue
        risk = risk_scores.get(vault_id, {})
        overall = float(risk.get("overall", 100.0))
        apy = float(vault.get("apy", 0.0) or 0.0)
        if overall > risk_cap and risk_tolerance != "aggressive":
            continue
        score = (
            float(vault.get("apy", 0.0) or 0.0)
            - (float(risk.get("overall", 100.0)) / 100.0) * 3.0
        )
        ranked.append(
            {
                "id": vault_id,
                "name": str(vault.get("name", "Vault")),
                "apy": apy,
                "risk": overall,
                "score": score,
            }
        )

    if not ranked:
        for vault in vaults:
            vault_id = str(vault.get("id", "")).strip()
            if not vault_id:
                continue
            risk = risk_scores.get(vault_id, {})
            ranked.append(
                {
                    "id": vault_id,
                    "name": str(vault.get("name", "Vault")),
                    "apy": float(vault.get("apy", 0.0) or 0.0),
                    "risk": float(risk.get("overall", 100.0)),
                    "score": (
                        float(vault.get("apy", 0.0) or 0.0)
                        - (float(risk.get("overall", 100.0)) / 100.0) * 3.0
                    ),
                }
            )

    ranked.sort(key=lambda item: (item["score"], item["apy"]), reverse=True)
    return ranked


def _build_allocation_plan(
    ranked: list[dict[str, Any]], risk_tolerance: str
) -> list[RecommendedVault]:
    if not ranked:
        return []

    split_map = {
        "conservative": [100],
        "moderate": [70, 30],
        "aggressive": [60, 40],
    }
    desired_split = split_map.get(risk_tolerance, [70, 30])
    selected = ranked[: len(desired_split)]
    if len(selected) == 1:
        selected = ranked[:1]
        desired_split = [100]

    plan: list[RecommendedVault] = []
    for vault, pct in zip(selected, desired_split, strict=False):
        rationale = (
            f"{vault['name']} combines {vault['apy']:.2f}% APY with "
            f"a risk score of {vault['risk']:.0f}/100."
        )
        plan.append(
            RecommendedVault(
                vault_id=vault["id"],
                allocation_pct=int(pct),
                rationale=rationale,
            )
        )

    total = sum(item.allocation_pct for item in plan)
    if total != 100 and plan:
        plan[0] = RecommendedVault(
            vault_id=plan[0].vault_id,
            allocation_pct=plan[0].allocation_pct + (100 - total),
            rationale=plan[0].rationale,
        )
    return plan


def _fallback_rationale(
    request: VaultRecommendationRequest, ranked: list[dict[str, Any]]
) -> str:
    if not ranked:
        return (
            "No live vault data was available, so the recommendation follows "
            f"the user's {request.risk_tolerance} risk tolerance and time "
            f"horizon of {request.time_horizon_months} months."
        )
    best = ranked[0]
    return (
        f"{best['name']} is the strongest live match for a {request.risk_tolerance} profile "
        f"because it combines live APY with an acceptable risk score for the selected horizon."
    )


def _fallback_vault_recommendation(
    request: VaultRecommendationRequest,
    ranked: list[dict[str, Any]],
    confidence: ConfidenceLevel,
) -> VaultRecommendationResponse:
    plan = _build_allocation_plan(ranked, request.risk_tolerance)
    weighted_apy = 0.0
    if plan and ranked:
        by_id = {item["id"]: item for item in ranked}
        for item in plan:
            source = by_id.get(item.vault_id)
            if source:
                weighted_apy += source["apy"] * (item.allocation_pct / 100.0)
    expected = _estimate_expected_yield(
        request.initial_deposit_usdc, request.time_horizon_months, weighted_apy
    )
    if not plan and ranked:
        plan = [
            RecommendedVault(
                vault_id=ranked[0]["id"],
                allocation_pct=100,
                rationale=_fallback_rationale(request, ranked),
            )
        ]
    elif not plan:
        plan = []
    return VaultRecommendationResponse(
        recommended_vaults=plan,
        expected_yield_usdc=expected,
        confidence=confidence,
    )


async def recommend_vaults(
    request: VaultRecommendationRequest,
    user_id: str | None = None,
) -> VaultRecommendationResponse:
    vault_context_fetcher = get_vault_context_fetcher()
    live_vaults = await vault_context_fetcher.fetch_available_vaults()
    market_rates = await vault_context_fetcher.fetch_market_rates()

    user_vaults: list[dict[str, Any]] = []
    if user_id:
        user_vaults = await vault_context_fetcher.fetch_user_vaults(user_id)

    risk_scores: dict[str, dict[str, Any]] = {}
    for vault in live_vaults:
        vault_id = str(vault.get("id", "")).strip()
        if not vault_id:
            continue
        risk = await vault_context_fetcher.fetch_vault_risk(vault_id)
        if risk:
            risk_scores[vault_id] = risk

    confidence, confidence_reason, data_freshness = _confidence_from_sources(
        bool(live_vaults),
        bool(risk_scores),
        bool(market_rates),
    )

    ranked = _rank_vaults(live_vaults, risk_scores, request.risk_tolerance)
    fallback = _fallback_vault_recommendation(request, ranked, confidence)

    selected_vaults = fallback.recommended_vaults
    weighted_apy = 0.0
    ranked_by_id = {item["id"]: item for item in ranked}
    for item in selected_vaults:
        source = ranked_by_id.get(item.vault_id)
        if source:
            weighted_apy += source["apy"] * (item.allocation_pct / 100.0)

    fallback = fallback.model_copy(
        update={
            "expected_yield_usdc": _estimate_expected_yield(
                request.initial_deposit_usdc,
                request.time_horizon_months,
                weighted_apy,
            ),
        }
    )

    vault_context_lines = [
        (
            f"- {v['name']}: APY {v.get('apy', 0.0):.2f}%, "
            f"risk {risk_scores.get(str(v.get('id', '')), {}).get('overall', 100.0):.0f}/100"
        )
        for v in live_vaults[:8]
    ]
    user_context_lines = [
        (
            f"- {v.get('name', 'Vault')}: "
            f"${float(v.get('balance_usd', 0.0) or 0.0):,.2f} balance, "
            f"APY {float(v.get('apy', 0.0) or 0.0):.2f}%"
        )
        for v in user_vaults[:5]
    ]

    def _vault_context_line(vault: dict[str, Any]) -> str:
        vid = str(vault.get("id", ""))
        risk_overall = risk_scores.get(vid, {}).get("overall", 100.0)
        return (
            f"- {vault['name']}: APY {vault.get('apy', 0.0):.2f}%, "
            f"risk {risk_overall:.0f}/100"
        )

    def _user_context_line(vault: dict[str, Any]) -> str:
        bal = float(vault.get("balance_usd", 0.0) or 0.0)
        apy = float(vault.get("apy", 0.0) or 0.0)
        return f"- {vault.get('name', 'Vault')}: ${bal:,.2f} balance, APY {apy:.2f}%"

    vault_context_lines = [_vault_context_line(v) for v in live_vaults[:8]]
    user_context_lines = [_user_context_line(v) for v in user_vaults[:5]]
    schema = (
        '{"recommended_vaults": [{"vault_id": str, "allocation_pct": int, "rationale": str}], '
        '"expected_yield_usdc": float, "confidence": "high"|"medium"|"low"}'
    )
    positions_json = json.dumps(user_vaults[:5])
    prompt = (
        "Recommend the best vault or vault split for a Nester user. "
        "Use only the live context below. "
        f"Risk tolerance: {request.risk_tolerance}. "
        f"Time horizon: {request.time_horizon_months} months. "
        f"Initial deposit: ${request.initial_deposit_usdc:.2f} USDC. "
        f"Savings goal: {request.savings_goal or 'not specified'}. "
        f"User positions: {positions_json}. "
        f"Live vaults:\n{chr(10).join(vault_context_lines)}. "
        "Existing position snapshot:\n"
        f"{chr(10).join(user_context_lines) if user_context_lines else 'none'}. "
        f"Confidence guidance: {confidence_reason}. "
        f"Data freshness: {data_freshness}. "
        f"Return JSON only, matching this schema: {schema}. "
        "Keep the rationale plain-language and avoid redundant wording."
    )

    try:
        response = await get_client().messages.create(
            model=settings.anthropic_model,
            max_tokens=RECOMMEND_MAX_TOKENS,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": prompt}],
        )
        text = next(
            (
                b.text
                for b in response.content
                if isinstance(b, anthropic.types.TextBlock)
            ),
            "",
        )
        parsed = json.loads(_json_strip(text))
        plan = [
            RecommendedVault(
                vault_id=str(item.get("vault_id", "")).strip(),
                allocation_pct=int(item.get("allocation_pct", 0)),
                rationale=str(item.get("rationale", "")).strip(),
            )
            for item in parsed.get("recommended_vaults", [])
            if str(item.get("vault_id", "")).strip()
        ]
        if not plan:
            return fallback

        return VaultRecommendationResponse(
            recommended_vaults=plan,
            expected_yield_usdc=float(
                parsed.get("expected_yield_usdc", fallback.expected_yield_usdc)
                or fallback.expected_yield_usdc
            ),
            confidence=confidence,
        )
    except Exception:
        logger.exception("Failed to get vault recommendation")
        return fallback


async def analyze_recommendation(
    prompt: str,
    user_id: str | None = None,
) -> Recommendation:
    vault_context_fetcher = get_vault_context_fetcher()
    live_vaults = await vault_context_fetcher.fetch_available_vaults()
    market_rates = await vault_context_fetcher.fetch_market_rates()
    user_vaults = (
        await vault_context_fetcher.fetch_user_vaults(user_id) if user_id else []
    )

    risk_scores: dict[str, dict[str, Any]] = {}
    for vault in live_vaults:
        vault_id = str(vault.get("id", "")).strip()
        if not vault_id:
            continue
        risk = await vault_context_fetcher.fetch_vault_risk(vault_id)
        if risk:
            risk_scores[vault_id] = risk

    confidence, confidence_reason, data_freshness = _confidence_from_sources(
        bool(live_vaults),
        bool(risk_scores),
        bool(market_rates),
    )

    schema = (
        '{"action": str, "rationale": str, "confidence": "high"|"medium"|"low", '
        '"confidence_reason": str, "data_freshness": str, "disclaimer": str}'
    )
    context_lines = [
        (
            f"- {v['name']}: APY {v.get('apy', 0.0):.2f}%, "
            f"risk {v.get('risk_tier', 'unknown')}"
        )
        for v in live_vaults[:6]
    ]
    user_context = json.dumps(user_vaults[:5]) if user_vaults else "[]"
    analysis_prompt = (
        f"Analyse this user request for Nester: {prompt}. "
        f"Live vault context:\n{chr(10).join(context_lines)}. "
        f"User positions: {user_context}. "
        f"Confidence guidance: {confidence_reason}. "
        f"Data freshness: {data_freshness}. "
        f"Return JSON only, matching this schema: {schema}."
    )

    try:
        response = await get_client().messages.create(
            model=settings.anthropic_model,
            max_tokens=ANALYZE_MAX_TOKENS,
            system=SYSTEM_PROMPT,
            messages=[{"role": "user", "content": analysis_prompt}],
        )
        text = next(
            (
                b.text
                for b in response.content
                if isinstance(b, anthropic.types.TextBlock)
            ),
            "",
        )
        parsed = json.loads(_json_strip(text))
        return Recommendation(
            action=str(parsed.get("action", "Review your vault allocation")).strip(),
            rationale=str(parsed.get("rationale", "")).strip(),
            confidence=confidence,
            confidence_reason=str(
                parsed.get("confidence_reason", confidence_reason)
            ).strip()
            or confidence_reason,
            data_freshness=str(parsed.get("data_freshness", data_freshness)).strip()
            or data_freshness,
            disclaimer=str(
                parsed.get("disclaimer", "This is guidance, not financial advice.")
            ).strip()
            or "This is guidance, not financial advice.",
        )
    except Exception:
        logger.exception("Failed to analyze recommendation prompt")
        return Recommendation(
            action="Review your vault allocation",
            rationale=_fallback_rationale(
                VaultRecommendationRequest(
                    risk_tolerance="moderate",
                    time_horizon_months=12,
                    initial_deposit_usdc=1.0,
                ),
                ranked=[],
            ),
            confidence=confidence,
            confidence_reason=confidence_reason,
            data_freshness=data_freshness,
            disclaimer="This is guidance, not financial advice.",
        )


async def analyze_portfolio(user_id: str) -> PortfolioAnalysisResponse:
    """Analyze user's portfolio using Claude tool use to produce structured output.

    Fetches user vault positions and uses Claude's tool use capability to generate
    structured portfolio breakdown (allocation, risk level, recommendations) along
    with a narrative explanation.

    Args:
        user_id: The authenticated user's ID.

    Returns:
        PortfolioAnalysisResponse with structured analysis and narrative.
    """
    vault_context_fetcher = get_vault_context_fetcher()
    user_vaults = await vault_context_fetcher.fetch_user_vaults(user_id)

    if not user_vaults:
        return PortfolioAnalysisResponse(
            analysis=PortfolioBreakdown(
                total_value_usdc=0.0,
                yield_30d_usdc=0.0,
                allocation_breakdown=[],
                risk_level="moderate",
                top_recommendation="Start by depositing into a Conservative vault for stability.",
                rebalance_suggested=False,
            ),
            narrative=(
                "You don't have any active vault positions yet. Start with a "
                "Conservative vault for stable, low-risk savings."
            ),
            confidence="high",
            generated_at=datetime.now(timezone.utc),
        )

    # Calculate portfolio totals and build allocation context
    total_value_usdc = sum(float(v.get("balance_usd", 0) or 0) for v in user_vaults)
    total_yield_30d = sum(float(v.get("yield_earned", 0) or 0) for v in user_vaults)

    allocation_context_lines = []
    for vault in user_vaults:
        vault_name = vault.get("name", "Unknown Vault")
        balance = float(vault.get("balance_usd", 0) or 0)
        apy = float(vault.get("apy", 0) or 0)
        pct = (balance / total_value_usdc * 100) if total_value_usdc > 0 else 0
        allocation_context_lines.append(
            f"- {vault_name}: ${balance:,.2f} ({pct:.1f}%), APY {apy:.2f}%"
        )

    allocation_context = "\n".join(allocation_context_lines)

    # Define Claude tool schema for structured portfolio analysis
    tools: list[dict[str, Any]] = [
        {
            "name": "portfolio_breakdown",
            "description": (
                "Return a structured portfolio analysis with allocation "
                "breakdown and recommendations"
            ),
            "input_schema": {
                "type": "object",
                "properties": {
                    "total_value_usdc": {
                        "type": "number",
                        "description": "Total portfolio value in USDC",
                    },
                    "yield_30d_usdc": {
                        "type": "number",
                        "description": "Expected yield over next 30 days in USDC",
                    },
                    "allocation_breakdown": {
                        "type": "array",
                        "description": "Breakdown by protocol with weights and APYs",
                        "items": {
                            "type": "object",
                            "properties": {
                                "protocol": {
                                    "type": "string",
                                    "description": "Protocol or vault name",
                                },
                                "weight": {
                                    "type": "number",
                                    "description": "Allocation weight as percentage (0-100)",
                                },
                                "apy": {
                                    "type": "number",
                                    "description": "Current APY for this allocation",
                                },
                            },
                            "required": ["protocol", "weight", "apy"],
                        },
                    },
                    "risk_level": {
                        "type": "string",
                        "enum": ["conservative", "moderate", "aggressive"],
                        "description": "Overall portfolio risk assessment",
                    },
                    "top_recommendation": {
                        "type": "string",
                        "description": "Primary recommendation for the user",
                    },
                    "rebalance_suggested": {
                        "type": "boolean",
                        "description": "Whether rebalancing is recommended",
                    },
                },
                "required": [
                    "total_value_usdc",
                    "yield_30d_usdc",
                    "allocation_breakdown",
                    "risk_level",
                    "top_recommendation",
                    "rebalance_suggested",
                ],
            },
        }
    ]

    # Build analysis prompt with portfolio context
    analysis_prompt = (
        "Analyze this Nester user's portfolio and provide structured output "
        "using the provided tool.\n\n"
        f"Portfolio Overview:\n"
        f"Total Value: ${total_value_usdc:,.2f} USDC\n"
        f"30-day Yield Earned: ${total_yield_30d:,.2f} USDC\n"
        f"Vault Positions:\n{allocation_context}\n\n"
        f"Market Context:\n"
        f"- Platform supports: Aave, Blend, Compound\n"
        f"- Fee structure: 0.5% management fee on yield\n"
        f"- Vault types: Flexible, Fixed-30d, Fixed-90d\n\n"
        "Use the portfolio_breakdown tool to return:\n"
        "1. Total portfolio value and expected 30-day yield\n"
        "2. Allocation breakdown by protocol with weights and APYs\n"
        "3. Overall risk level assessment\n"
        "4. Top recommendation for optimization\n"
        "5. Whether rebalancing is suggested\n\n"
        "Base your analysis on the current positions shown above."
    )

    client = get_client()
    structured_data = None
    confidence: ConfidenceLevel = "medium"

    try:
        response = await client.messages.create(
            model=settings.anthropic_model,
            max_tokens=1000,
            system=SYSTEM_PROMPT,
            tools=tools,  # type: ignore[arg-type]
            messages=[{"role": "user", "content": analysis_prompt}],
        )

        # Extract tool use block from response
        tool_use_block = None
        for block in response.content:
            if block.type == "tool_use":
                tool_use_block = block
                break

        if tool_use_block:
            # Parse tool input as structured portfolio data
            tool_input: dict[str, Any] = cast(dict[str, Any], tool_use_block.input)

            # Validate and construct models
            allocation_items = [
                AllocationItem(
                    protocol=item["protocol"],
                    weight=float(item["weight"]),
                    apy=float(item["apy"]),
                )
                for item in tool_input.get("allocation_breakdown", [])
            ]

            structured_data = PortfolioBreakdown(
                total_value_usdc=float(
                    tool_input.get("total_value_usdc", total_value_usdc)
                ),
                yield_30d_usdc=float(tool_input.get("yield_30d_usdc", total_yield_30d)),
                allocation_breakdown=allocation_items,
                risk_level=tool_input.get("risk_level", "moderate"),
                top_recommendation=str(tool_input.get("top_recommendation", "")),
                rebalance_suggested=bool(tool_input.get("rebalance_suggested", False)),
            )
            confidence = "high"
        else:
            logger.warning(
                "No tool_use block found in Claude response for portfolio analysis"
            )

    except Exception as exc:
        logger.exception(
            "Failed to parse Claude tool use response for portfolio: %s", exc
        )

    # Fallback to structured response if tool use failed
    if not structured_data:
        structured_data = PortfolioBreakdown(
            total_value_usdc=total_value_usdc,
            yield_30d_usdc=total_yield_30d,
            allocation_breakdown=[
                AllocationItem(
                    protocol=v.get("name", "Unknown"),
                    weight=(
                        (float(v.get("balance_usd", 0) or 0) / total_value_usdc * 100)
                        if total_value_usdc > 0
                        else 0
                    ),
                    apy=float(v.get("apy", 0) or 0),
                )
                for v in user_vaults
            ],
            risk_level="moderate",
            top_recommendation="Review your allocation across your vaults.",
            rebalance_suggested=False,
        )
        confidence = "medium"

    # Generate narrative explanation
    narrative = (
        f"Your Nester portfolio has a total value of ${total_value_usdc:,.2f} "
        f"USDC across {len(user_vaults)} vault(s). Over the last 30 days, "
        f"you've earned ${total_yield_30d:,.2f} in yield. "
        f"Your portfolio risk level is {structured_data.risk_level}. "
        f"{structured_data.top_recommendation}"
    )

    return PortfolioAnalysisResponse(
        analysis=structured_data,
        narrative=narrative,
        confidence=confidence,
        generated_at=datetime.now(timezone.utc),
    )
