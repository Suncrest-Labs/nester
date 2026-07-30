# Adaptive API abuse protection

`AbuseProtector` layers behavioral detection above the existing per-key limiter.
It aggregates failed authentication across clients, tracks distinct lookup probes,
and detects shared high-velocity fingerprints on signup, authentication, referral,
reward, and other incentive-sensitive routes.

Responses are graduated. Suspicious traffic receives HTTP `428` with
`STEP_UP_REQUIRED`; it is not permanently blocked and can proceed after completing
verification. Endpoint escalation expires automatically. Each decision emits an
`AbuseEvent` through `AbuseObserver`, which is the shared integration point for
fraud/anomaly processing, security metrics, and false-positive monitoring.

Sensitive account and token lookup handlers must always call
`WriteUniformLookupResponse` after their internal lookup so existence cannot be
distinguished by status or body. Deployment adapters should store aggregate
windows in the same Redis cluster as rate limiting; policy remains fail-open if
that shared backend is unavailable. The included in-memory policy store is
single-instance only and is intended for tests and local deployments.
