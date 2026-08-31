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

    # --- Distributed tracing (nester#1054) -------------------------------
    #
    # Tracing is opt-in and off by default so that neither existing
    # deployments nor CI acquire a collector dependency. When disabled no
    # tracer provider is installed and no collector is dialled.
    tracing_enabled: bool = False

    # Prometheus exposition for the intelligence SLIs (nester#1056).
    #
    # Enabled by default, unlike tracing: the metrics are unsampled counters
    # backing an error budget, and a service whose SLI stops being recorded
    # reports a perfect success rate rather than an outage. The cost is a few
    # dozen series with no external dependency and no collector to dial.
    metrics_enabled: bool = True

    # Bearer token required to scrape /metrics. When empty the endpoint is
    # unauthenticated, which is only safe where the port is not publicly
    # routable. The Go API solves this with a separate loopback listener; this
    # service serves one port, so the route is guarded by a shared secret
    # instead. Exposition reveals internal route names, traffic volumes, and
    # error rates, so it is not public data.
    metrics_token: str = ""

    # Deployment environment reported as deployment.environment.name on every
    # span. Mirrors the ENVIRONMENT variable the JWT validator reads, so a
    # single value describes the deployment.
    environment: str = os.getenv("ENVIRONMENT", "development").lower()

    # OTLP/gRPC collector address. The default targets a collector running
    # alongside the service, matching the local observability compose profile.
    otel_exporter_otlp_endpoint: str = "http://localhost:4317"

    # Whether the collector connection skips TLS. True suits local
    # development; deploy with it false so spans are not shipped in plaintext.
    otel_exporter_otlp_insecure: bool = True

    # service.name on every span emitted by this process.
    otel_service_name: str = "nester-intelligence"

    # Bounds a single export round trip to the collector, in seconds.
    otel_exporter_timeout: int = 10

    # Head-based sampling probability for traces this service roots. Traces
    # arriving with an upstream sampling decision honour that decision instead
    # (see build_sampler). Errors and slow requests are retained by the
    # collector's tail sampler regardless of this value.
    tracing_sample_ratio: float = 0.05

    model_config = SettingsConfigDict(
        env_prefix="INTELLIGENCE_",
        env_file=".env",
        extra="ignore",
    )

    @model_validator(mode="after")
    def _validate_tracing_transport(self) -> "Settings":
        """Reject a plaintext OTLP exporter outside development.

        Spans carry request metadata and must not cross a network
        unencrypted. The insecure default suits a collector on the same host
        or compose network; carrying it into a deployed environment would
        ship telemetry over plaintext gRPC, so it must be set explicitly
        there.
        """
        if (
            self.tracing_enabled
            and self.otel_exporter_otlp_insecure
            and self.environment in {"staging", "production"}
        ):
            raise ValueError(
                "INTELLIGENCE_OTEL_EXPORTER_OTLP_INSECURE must be false when "
                "tracing is enabled outside development."
            )
        return self

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
