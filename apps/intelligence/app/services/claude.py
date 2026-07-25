"""Claude client configuration."""

import anthropic

from app.config import settings

client = anthropic.Anthropic(api_key=settings.anthropic_api_key)

# Model configuration - using claude-3-5-sonnet-20241022 for structured output
# TODO: Update to latest model when available
MODEL_ID = "claude-3-5-sonnet-20241022"


def get_client():
    """Get the Anthropic client."""
    return client


def get_model_id():
    """Get the model ID."""
    return MODEL_ID