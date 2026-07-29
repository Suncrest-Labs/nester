import anthropic

from app.config import settings
from app.services.i18n import localize_system_prompt

client = anthropic.Anthropic(api_key=settings.anthropic_api_key)


def get_model() -> str:
    """Return the configured Claude model id for all generation surfaces."""
    return settings.anthropic_model


def build_system_prompt(base_system: str, language: str) -> str:
    """Attach the per-language directive and financial glossary to a system
    prompt so every generation call — chat, coaching, recommendations,
    digests, and nudges — responds natively in the user's language with the
    correct local financial terminology (#multilingual).
    """
    return localize_system_prompt(base_system, language)
