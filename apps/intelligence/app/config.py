import os

from pydantic import model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    app_name: str = "Nester Intelligence"
    host: str = "0.0.0.0"
    port: int = 8000
    anthropic_api_key: str = ""
    # Explanation-only workload (narrating an already-computed allocation, plus
    # existing coaching/recommendation narration): claude-sonnet-5 is the current
    # flagship id and keeps prose quality high for user-facing copy. Still fully
    # overridable via INTELLIGENCE_ANTHROPIC_MODEL for cost tuning (e.g. a haiku
    # tier) without a code change.
    anthropic_model: str = "claude-sonnet-5"
    jwt_secret: str = ""
    redis_url: str = "redis://localhost:6379/0"  # gitleaks:allow
    nester_api_base_url: str = "http://localhost:8080"
    nester_service_api_key: str = ""
    defillama_base_url: str = "https://api.llama.fi"

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
