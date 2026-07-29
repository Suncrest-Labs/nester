"""Claude client configuration."""

"""Claude client and per-user tone/style prompt helpers (#927).

Prometheus's base persona and scope rules live in
`app.services.prometheus.SYSTEM_PROMPT`. This module builds an additional,
optional instruction block from a user's saved `ResponsePreferences` (response
length and tone) and appends it to a system prompt, so the same underlying
facts get reworded to match the user's taste without touching the base
persona, grounding, or trust-boundary rules.
"""

import anthropic

from app.config import settings
from app.models.preferences import ResponsePreferences

client = anthropic.Anthropic(api_key=settings.anthropic_api_key)

# Use the configured model from settings
MODEL_ID = settings.anthropic_model


def get_client():
    return client


def get_model_id():
    return MODEL_ID


_LENGTH_INSTRUCTIONS: dict[str, str] = {
    "concise": (
        "Keep answers short: 1-3 sentences where possible, no more than one "
        "short paragraph. Skip preamble and get straight to the point."
    ),
    "detailed": (
        "Give thorough answers: explain the reasoning behind a recommendation "
        "and any relevant context, using multiple short paragraphs when useful."
    ),
}

_TONE_INSTRUCTIONS: dict[str, str] = {
    "casual": (
        "Use a warm, casual, conversational tone, like a knowledgeable friend. "
        "Contractions are fine."
    ),
    "formal": (
        "Use a professional, formal tone. Avoid slang and contractions, and "
        "write in complete, precise sentences."
    ),
}


def build_tone_instructions(preferences: ResponsePreferences | None) -> str:
    """Render a preferences-driven instruction block for the system prompt.

    Returns an empty string when no preferences are supplied, so callers with
    no preference on file get the base prompt unchanged.
    """
    if preferences is None:
        return ""

    length_line = _LENGTH_INSTRUCTIONS.get(
        preferences.response_length, _LENGTH_INSTRUCTIONS["detailed"]
    )
    tone_line = _TONE_INSTRUCTIONS.get(preferences.response_tone, _TONE_INSTRUCTIONS["casual"])
    return (
        "\n\n## User tone preference (applies to this response only)\n"
        f"- Length: {length_line}\n"
        f"- Tone: {tone_line}"
    )


def apply_tone_preferences(system_prompt: str, preferences: ResponsePreferences | None) -> str:
    """Append the user's tone/length preference instructions to a system prompt.

    A no-op (returns `system_prompt` unchanged) when `preferences` is None.
    """
    block = build_tone_instructions(preferences)
    return system_prompt + block if block else system_prompt
