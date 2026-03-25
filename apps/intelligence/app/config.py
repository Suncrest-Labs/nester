from pydantic_settings import BaseSettings


class Settings(BaseSettings):
    app_name: str = "Nester Intelligence"
    host: str = "0.0.0.0"
    port: int = 8000
    anthropic_api_key: str = ""

    model_config = {"env_prefix": "INTELLIGENCE_"}


settings = Settings()
