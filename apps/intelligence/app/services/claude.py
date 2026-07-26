"""Claude client configuration."""

import anthropic
from app.config import settings

client = anthropic.Anthropic(api_key=settings.anthropic_api_key)

# Pull model ID from config - update in .env
MODEL_ID = getattr(settings, "claude_model_id", "claude-3-5-sonnet-20241022")


def get_client():
    return client


def get_model_id():
    return MODEL_ID
