import anthropic

from app.config import settings

client = anthropic.Anthropic(api_key=settings.anthropic_api_key)
