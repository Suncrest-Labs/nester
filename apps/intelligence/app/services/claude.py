"""Claude client configuration."""

import anthropic
from app.config import settings

client = anthropic.Anthropic(api_key=settings.anthropic_api_key)

# Use the configured model from settings
MODEL_ID = settings.anthropic_model


def get_client():
    return client


def get_model_id():
    return MODEL_ID