"""Core Prometheus AI logic — system prompt, streaming chat, and structured analysis."""

import json
import logging
from collections.abc import AsyncIterator

from google import genai
from google.genai import types

from app.config import settings
from app.services.conversation_store import store as conversation_store

logger = logging.getLogger(__name__)

MODEL = "gemini-2.0-flash"
CHAT_MAX_TOKENS = 1024
ANALYZE_MAX_TOKENS = 800

_client: genai.Client | None = None


def get_client() -> genai.Client:
    global _client
    if _client is None:
        _client = genai.Client(api_key=settings.gemini_api_key)
    return _client


# ---------------------------------------------------------------------------
# System prompt
# ---------------------------------------------------------------------------

SYSTEM_PROMPT = """You are Prometheus, the financial intelligence layer of Nester.

Nester is a yield-bearing savings platform built on the Stellar blockchain. It lets users in Africa (primarily Nigeria, Ghana, and Kenya) deposit USDC or XLM into yield-generating vaults, earn APY, and withdraw earnings directly to their local bank accounts (NGN, GHS, KES).

## Your role
Help users make smart decisions about their Nester vaults, understand their portfolio, and optimise their yield strategy. You are knowledgeable about:
- DeFi yield strategies on Stellar
- Nester's vault risk tiers: Conservative, Balanced, Growth, DeFi500
- Stellar-native assets: USDC, XLM
- Offramp and settlement flow (crypto → NGN/GHS/KES)
- Savings goals and compounding yield

## Strict scope
You ONLY answer questions about:
- The user's Nester vaults, deposits, and portfolio
- Yield strategies, APYs, and vault risk tiers on Nester
- Savings goals and reaching them with Nester
- How offramp/settlement to local fiat works
- How Nester's contracts, fees, and mechanics work

You do NOT answer questions about:
- Price predictions or market speculation for any asset
- Other DeFi protocols not integrated with Nester
- General financial advice unrelated to Nester
- Anything outside personal savings and yield on the Nester platform

If asked something outside scope, respond with:
"That is outside what I can help with — I am focused on your Nester savings and vaults. Is there something about your portfolio or yield strategy I can help with?"

## Vault tiers (reference)
- **Conservative** — Stablecoin-only, lowest risk, ~4–6% APY. Good for emergency funds.
- **Balanced** — Mix of stablecoin and blue-chip DeFi, ~8–12% APY. Good for medium-term goals.
- **Growth** — Higher-yield DeFi strategies, ~15–25% APY. Suited for long-term horizon with risk tolerance.
- **DeFi500** — Curated top-500 DeFi index exposure, ~20–30% APY. Highest risk, highest reward.

## Tone
Be direct and specific. Use plain language — avoid jargon unless the user is clearly comfortable with it. When you recommend something, say why in one sentence. Keep responses concise; the user is reading a sidebar panel, not an article. Do not use bullet points for simple answers."""


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _to_gemini_history(history: list[dict]) -> list[types.Content]:
    """Convert conversation store format to Gemini Content objects.

    Conversation store uses {"role": "user"|"assistant", "content": str}.
    Gemini uses role "user" or "model" with parts.
    """
    result = []
    for msg in history:
        role = "model" if msg["role"] == "assistant" else "user"
        result.append(types.Content(role=role, parts=[types.Part(text=msg["content"])]))
    return result


# ---------------------------------------------------------------------------
# Streaming chat
# ---------------------------------------------------------------------------

async def stream_chat(user_id: str, message: str) -> AsyncIterator[str]:
    """Yield SSE-formatted data strings for a streaming Gemini response.

    Each yielded string is formatted as `data: <text>\\n\\n`.
    A final `data: [DONE]\\n\\n` is yielded when the stream ends.
    """
    history = conversation_store.get(user_id)
    conversation_store.append(user_id, "user", message)

    # Build contents: history + current message
    contents = _to_gemini_history(history) + [
        types.Content(role="user", parts=[types.Part(text=message)])
    ]

    client = get_client()
    full_response = ""

    try:
        stream = await client.aio.models.generate_content_stream(
            model=MODEL,
            contents=contents,
            config=types.GenerateContentConfig(
                system_instruction=SYSTEM_PROMPT,
                max_output_tokens=CHAT_MAX_TOKENS,
            ),
        )
        async for chunk in stream:
            if chunk.text:
                full_response += chunk.text
                # Escape newlines so each SSE data line stays on one line
                safe = chunk.text.replace("\n", "\\n")
                yield f"data: {safe}\n\n"

        conversation_store.append(user_id, "assistant", full_response)
        yield "data: [DONE]\n\n"

    except Exception:
        logger.exception("Gemini streaming error for user %s", user_id)
        yield "data: Sorry, I had trouble connecting. Please try again.\n\n"
        yield "data: [DONE]\n\n"


# ---------------------------------------------------------------------------
# Structured analysis (non-streaming)
# ---------------------------------------------------------------------------

async def get_portfolio_insights(user_id: str) -> list[dict]:
    """Return 2 insight cards for the user's portfolio."""
    prompt = (
        f"Generate 2 concise portfolio insight cards for a Nester user (id: {user_id}). "
        "Each card should have a short title, a one-sentence body, a confidence score (0.0–1.0), "
        "and optionally an action with a label and href. "
        "Focus on practical savings advice relevant to Nester vaults on Stellar. "
        "Respond with a JSON array only, no markdown, matching this schema: "
        '[{"title": str, "body": str, "confidence": float, "action": {"label": str, "href": str} | null}]'
    )

    client = get_client()
    try:
        response = await client.aio.models.generate_content(
            model=MODEL,
            contents=prompt,
            config=types.GenerateContentConfig(
                system_instruction=SYSTEM_PROMPT,
                max_output_tokens=ANALYZE_MAX_TOKENS,
            ),
        )
        raw = response.text.strip().removeprefix("```json").removeprefix("```").removesuffix("```").strip()
        return json.loads(raw)
    except Exception:
        logger.exception("Failed to get portfolio insights for user %s", user_id)
        return []


async def get_market_sentiment() -> dict:
    """Return a market sentiment summary for the Stellar DeFi / stablecoin space."""
    prompt = (
        "Give a brief market sentiment assessment for the Stellar DeFi and stablecoin yield space "
        "as it relates to Nester users in Africa. "
        "Respond with JSON only, no markdown, matching this schema: "
        '{"signal": "bull"|"bear"|"neutral", "summary": str (1 sentence), "confidence": float (0.0–1.0), "updatedAt": str (ISO timestamp now)}'
    )

    client = get_client()
    try:
        response = await client.aio.models.generate_content(
            model=MODEL,
            contents=prompt,
            config=types.GenerateContentConfig(
                system_instruction=SYSTEM_PROMPT,
                max_output_tokens=200,
            ),
        )
        raw = response.text.strip().removeprefix("```json").removeprefix("```").removesuffix("```").strip()
        return json.loads(raw)
    except Exception:
        logger.exception("Failed to get market sentiment")
        return {
            "signal": "neutral",
            "summary": "Sentiment data temporarily unavailable.",
            "confidence": 0.0,
            "updatedAt": "",
        }


async def get_vault_recommendations(vault_id: str) -> dict:
    """Return AI commentary and recommendations for a specific vault."""
    prompt = (
        f"Give an AI commentary and recommendations for Nester vault id '{vault_id}'. "
        "Assume it is a yield-bearing Stellar vault. Be specific about what type of user this vault suits. "
        "Respond with JSON only, no markdown, matching this schema: "
        '{"vaultId": str, "commentary": str, "percentileRank": int (0-100), "recommendations": [str], "confidence": float}'
    )

    client = get_client()
    try:
        response = await client.aio.models.generate_content(
            model=MODEL,
            contents=prompt,
            config=types.GenerateContentConfig(
                system_instruction=SYSTEM_PROMPT,
                max_output_tokens=ANALYZE_MAX_TOKENS,
            ),
        )
        raw = response.text.strip().removeprefix("```json").removeprefix("```").removesuffix("```").strip()
        return json.loads(raw)
    except Exception:
        logger.exception("Failed to get vault recommendations for vault %s", vault_id)
        return {
            "vaultId": vault_id,
            "commentary": "Recommendations temporarily unavailable.",
            "percentileRank": 0,
            "recommendations": [],
            "confidence": 0.0,
        }
