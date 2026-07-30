"""Conversation history summarization service for Prometheus AI.

When conversation history exceeds the configured token threshold
(``INTELLIGENCE_MAX_HISTORY_TOKENS``, default 80 000 tokens), this service:

1. Estimates the token count of the active history using a simple character-
   based approximation (4 chars ≈ 1 token).  This avoids a round-trip to the
   tokenizer API on every message and is accurate enough for threshold detection.

2. Calls Claude to generate a concise, factual summary of the *older* turns —
   those that will be replaced.

3. Returns a new history list where the older turns have been replaced with a
   single synthetic ``assistant`` message containing
   ``[Summary of previous conversation]\\n<summary>``, followed by the most
   recent ``history_recent_turns_kept`` turns verbatim.

4. The caller (``conversation_store``) is responsible for persisting both the
   compacted active history *and* the full pre-summarization history to a
   separate Redis key for audit purposes.

The user never sees any indication of summarization — their next message is
answered with the same context quality as before, just compressed.
"""

import logging
from typing import TYPE_CHECKING

import anthropic

from app.config import settings

if TYPE_CHECKING:
    pass

logger = logging.getLogger(__name__)

# Sentinel prefix embedded in the synthetic summary message so callers can
# detect (and skip re-summarizing) a message that is already a summary.
SUMMARY_PREFIX = "[Summary of previous conversation]\n"

# Characters per token approximation used for threshold detection.
_CHARS_PER_TOKEN = 4

# Max tokens Claude should use when producing the summary itself.
_SUMMARY_MAX_TOKENS = 512


def estimate_token_count(history: list[dict[str, str]]) -> int:
    """Return a rough token estimate for ``history``.

    Uses the heuristic of 4 characters per token, which is accurate to within
    ~20% for English prose and good enough for threshold detection.
    """
    total_chars = sum(len(msg.get("content", "")) for msg in history)
    return total_chars // _CHARS_PER_TOKEN


def needs_summarization(history: list[dict[str, str]]) -> bool:
    """Return True if ``history`` exceeds the configured token threshold.

    Returns False when the threshold is 0 (disabled) or when the history is
    empty.
    """
    threshold = settings.max_history_tokens
    if threshold <= 0 or not history:
        return False
    return estimate_token_count(history) > threshold


def _split_history(
    history: list[dict[str, str]],
    recent_turns: int,
) -> tuple[list[dict[str, str]], list[dict[str, str]]]:
    """Split history into (older_turns, recent_turns_list).

    ``recent_turns`` controls how many messages to keep verbatim.  The rest
    become the input for summarization.  We always keep at least 2 messages
    (one user, one assistant) regardless of the configured value.
    """
    keep = max(2, recent_turns)
    if len(history) <= keep:
        # Not enough history to split — nothing to summarize.
        return [], history
    split_at = len(history) - keep
    return history[:split_at], history[split_at:]


def _build_summarization_prompt(older_turns: list[dict[str, str]]) -> str:
    """Build the user prompt asking Claude to summarize ``older_turns``."""
    lines = []
    for msg in older_turns:
        role = msg.get("role", "unknown").capitalize()
        content = msg.get("content", "").strip()
        lines.append(f"{role}: {content}")
    conversation_text = "\n\n".join(lines)
    return (
        "The following is the earlier part of a conversation between a user and Prometheus, "
        "the Nester financial AI assistant. Summarize the key facts, decisions, and context "
        "discussed so that the assistant can continue the conversation accurately without "
        "reading the full text.\n\n"
        "Be concise and factual. Focus on:\n"
        "- The user's savings goals, vault choices, and portfolio details mentioned\n"
        "- Any recommendations Prometheus made\n"
        "- Specific numbers (APYs, amounts, dates) that might be referenced later\n"
        "- The overall trajectory of the conversation\n\n"
        "Do NOT include pleasantries or meta-commentary. Output plain prose, 3-6 sentences.\n\n"
        f"Conversation to summarize:\n{conversation_text}"
    )


async def summarize_history(
    history: list[dict[str, str]],
    client: anthropic.AsyncAnthropic,
) -> list[dict[str, str]]:
    """Summarize ``history`` and return a compacted replacement.

    The returned list is:
      [{"role": "assistant", "content": "[Summary of previous conversation]\\n<summary>"},
       <last N turns verbatim>]

    If summarization fails (e.g. API error), the original ``history`` is
    returned unchanged so the caller can proceed without data loss.
    """
    recent_turns = max(2, settings.history_recent_turns_kept)
    older, recent = _split_history(history, recent_turns)

    if not older:
        # Nothing to summarize.
        return history

    prompt = _build_summarization_prompt(older)

    try:
        response = await client.messages.create(
            model=settings.anthropic_model,
            max_tokens=_SUMMARY_MAX_TOKENS,
            system=(
                "You are a precise summarizer for a financial AI assistant. "
                "Output only the summary text with no preamble or sign-off."
            ),
            messages=[{"role": "user", "content": prompt}],
        )
        summary_text = ""
        for block in response.content:
            if hasattr(block, "text"):
                summary_text += block.text

        summary_text = summary_text.strip()
        if not summary_text:
            logger.warning("summarize_history: empty summary returned, keeping full history")
            return history

        summary_message: dict[str, str] = {
            "role": "assistant",
            "content": f"{SUMMARY_PREFIX}{summary_text}",
        }
        compacted = [summary_message] + recent
        logger.info(
            "summarize_history: compressed %d turns → 1 summary + %d recent (est. %d → %d tokens)",
            len(older),
            len(recent),
            estimate_token_count(history),
            estimate_token_count(compacted),
        )
        return compacted

    except Exception as exc:
        logger.warning("summarize_history: failed to summarize, keeping full history: %s", exc)
        return history
