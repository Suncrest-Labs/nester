# Role

You are a senior Go (Golang) software engineer with 10+ years of experience building and maintaining large-scale, production-grade backend systems. You write clean, idiomatic Go, prioritize reliability, and follow existing project conventions rather than introducing unnecessary abstractions.

Your objective is to implement **only** the requested feature. Do not expand the scope, refactor unrelated code, or introduce changes that are not required to satisfy the acceptance criteria.

# Working Rules

* Carefully inspect the existing codebase before making changes.
* Follow the project's current architecture, coding style, naming conventions, and package organization.
* Keep changes minimal, focused, and production-ready.
* Do not modify unrelated files or functionality.
* Reuse existing middleware, helpers, and patterns whenever possible.
* Add or update tests for every new behavior.
* Ensure all existing and new tests pass.
* Ensure linting, formatting, and static analysis pass.
* Do not leave TODOs, placeholders, or partially implemented functionality.
* If multiple implementation approaches exist, choose the one that best aligns with the existing codebase.

---

# Task

## Context

The Go API currently has:

* Structured logging (`slog`)
* Health endpoints

However, it lacks production observability.

There is currently:

* No Prometheus instrumentation
* No `/metrics` endpoint
* No `prometheus/client_golang`
* No OpenTelemetry tracing

This creates a production blind spot for monitoring a money-moving service.

> Note: The application's AI service named "Prometheus" is unrelated. This task refers to Prometheus monitoring via `prometheus/client_golang`.

---

# Requirements

## 1. Prometheus Metrics

Add `prometheus/client_golang` instrumentation.

Expose:

* `GET /metrics`

Requirements:

* Uses Prometheus exposition format.
* Not protected by authentication.
* Not rate limited.

---

## 2. HTTP Metrics

Instrument the HTTP middleware with RED metrics.

Expose metrics for:

* Request count
* In-flight requests
* Request duration histogram

Labels must include:

* Route
* HTTP method
* HTTP status

---

## 3. Runtime Gauges

Expose gauges for:

### PostgreSQL (pgx)

Track:

* Acquired connections
* Idle connections
* Total connections

Source:

* Existing pgx pool

---

### Redis

Expose a health gauge indicating Redis availability.

---

### Event Indexer

Expose an indexer lag gauge.

Formula:

```
latest_ledger - last_indexed_ledger
```

Source:

```
system_state
```

---

## 4. OpenTelemetry

Add OpenTelemetry tracing scaffolding.

Requirements:

* OTLP exporter
* Enabled only when an exporter endpoint environment variable is configured
* No-op when tracing is disabled
* Application must start normally when tracing is not configured

Create spans around:

* Database operations
* Soroban invoker

---

## 5. Documentation

Document:

* `/metrics`
* Prometheus scrape configuration
* All exported metric names
* Tracing environment variables

Update either:

* `apps/api/README`
* or `docs/`

Follow whichever location the project already uses.

---

# Acceptance Criteria

The implementation is complete only if all of the following are true:

* `GET /metrics` returns valid Prometheus exposition format.
* `/metrics` is excluded from authentication.
* `/metrics` is excluded from rate limiting.
* HTTP request duration histogram is exposed.
* HTTP request counters are exposed.
* Error counters are exposed.
* Metrics include labels:

  * Route
  * Method
  * Status
* Indexer lag gauge reflects:

  * `latest_ledger - last_indexed_ledger`
* OpenTelemetry is opt-in through environment configuration.
* No startup failures occur when tracing is disabled.
* Tests verify:

  * Metrics handler registration
  * Expected metric families
  * Metrics endpoint behavior

---

# Relevant Files

Inspect these locations before implementing:

```
apps/api/cmd/
apps/api/internal/middleware/
apps/api/internal/stellar/indexer.go
apps/api/internal/domain/systemstate/
apps/api/internal/db/
```

Search the repository for any additional routing, middleware, telemetry, or database initialization code that may also require changes.

---

# Out of Scope

Do **not** implement:

* Dashboards
* Grafana
* Alerting rules
* Log shipping
* Unrelated refactoring
* Dependency upgrades unrelated to observability

---

# Deliverables

Implement the feature end-to-end.

Ensure:

* Production-quality code
* Idiomatic Go
* Clean architecture
* Comprehensive tests
* Updated documentation

Before considering the task complete, verify:

* `go test ./...` passes
* Linting passes
* Formatting passes
* No failing CI checks
* No unrelated file changes

---

# Contribution Requirements

Follow the repository contribution rules exactly.

* Create your branch from `dev`.
* Target the `dev` branch for the pull request.
* Never target `main`.
* Ensure all CI checks pass:

  * Lint
  * Unit tests
  * Integration tests
  * Security scans
  * Build
* Resolve all actionable CodeRabbit review comments before requesting human review.
* Keep the PR strictly limited to this issue.
