import os

from pydantic import model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    app_name: str = "Nester Intelligence"
    # The service runs in a container and must bind every interface to be
    # reachable from outside it; 127.0.0.1 would make it unreachable.
    # Exposure is governed by the network policy and ingress, not by this
    # default, which is overridable via INTELLIGENCE_HOST.
    host: str = "0.0.0.0"  # nosec B104
    port: int = 8000
    anthropic_api_key: str = ""
    # claude-sonnet-5 is the current flagship id. Used for explanation-only
    # workloads (narrating an already-computed allocation, the recommendation
    # engine's tool-use selection, coaching/analysis narration) on a per-user
    # cadence, not per keystroke, so Sonnet-tier capability for prose quality
    # and tool-use/structured-output reliability is worth the cost over Haiku.
    # Still fully overridable via INTELLIGENCE_ANTHROPIC_MODEL for deployments
    # that want to trade capability for cost.
    anthropic_model: str = "claude-sonnet-5"
    max_tool_rounds: int = 4
    daily_token_budget_per_user: int = 100000
    jwt_secret: str = ""
    redis_url: str = "redis://localhost:6379/0"  # gitleaks:allow
    nester_api_base_url: str = "http://localhost:8080"
    nester_service_api_key: str = ""
    defillama_base_url: str = "https://api.llama.fi"

    # Maximum token count for conversation history before automatic summarization
    # is triggered. When the estimated token count of the active history exceeds
    # this threshold, Prometheus generates a summary of the earlier turns and
    # replaces them with "[Summary of previous conversation] <summary>" so the
    # active context stays within this bound.
    #
    # The default of 80_000 corresponds to roughly 80% of the claude-sonnet-5
    # context window (200k tokens). Adjust down to reduce per-request cost or
    # up to allow more conversational depth before summarization.
    #
    # Set INTELLIGENCE_MAX_HISTORY_TOKENS=0 to disable summarization entirely
    # (not recommended for production — long sessions will hit context limits).
    max_history_tokens: int = 80_000

    # Number of most-recent turns to ALWAYS retain verbatim in the active
    # context after a summarization pass. Older turns are replaced by the
    # summary. Must be ≥ 2 (one user + one assistant) so the conversation
    # remains coherent.
    history_recent_turns_kept: int = 6

    model_config = SettingsConfigDict(
        env_prefix="INTELLIGENCE_",
        env_file=".env",
        extra="ignore",
    )

    @model_validator(mode="after")
    def _validate_production_jwt(self) -> "Settings":
        environment = os.getenv("ENVIRONMENT", "development").lower()
        if environment != "development" and not self.jwt_secret:
            raise ValueError(
                "INTELLIGENCE_JWT_SECRET must be set in non-development environments. "
                "Running without JWT authentication is not allowed outside development."
            )
        return self


settings = Settings()
