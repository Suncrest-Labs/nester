# Nester API

Production-grade Go backend service with clean architecture foundations.

## Overview

Production Go backend service powering the Nester platform. Serves as the authoritative API layer for vault, user, and offramp domains.

## Directory Structure

### `cmd/`
**Entrypoints only.** Contains `server/main.go` — application bootstrap with router initialization and graceful shutdown. No domain logic here.

### `internal/`
**Private domain logic.** Not importable by external packages.

- `config/` — configuration loading and validation
- `domain/` — domain entities and business logic
  - `vault/` — vault operations and state
  - `user/` — user identity and auth
  - `offramp/` — off-ramp transaction processing
- `handler/` — HTTP request handlers
- `middleware/` — request/response middleware
- `repository/` — data access layer
- `service/` — business logic orchestration

### `pkg/`
**Reusable utilities.** Importable by external packages.

- `response/` — standard HTTP response formatting
- `validator/` — input validation helpers

### `migrations/`
**Database schema evolution.** Version-controlled SQL or Go migration scripts.

## Local Development

### Prerequisites
- Go 1.22+
- golangci-lint (optional, for linting)

### Running the server

```bash
make run
```

Server starts on `http://localhost:8080` (or `$PORT` if set).

### Health check

```bash
curl http://localhost:8080/api/v1/healthz
# Returns: {"status":"ok"} (200)

curl http://localhost:8080/api/v1/readyz
# Returns: {"status":"ok"} (200)
```

## Environment Variables

Configuration is driven by environment variables with sensible defaults:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown timeout (Go duration format) |
| `CORS_ALLOWED_ORIGINS` | `*` | Comma-separated list of allowed CORS origins |
| `CORS_ALLOWED_METHODS` | `GET,POST,PUT,PATCH,DELETE,OPTIONS` | Comma-separated list of allowed CORS methods |
| `CORS_ALLOWED_HEADERS` | `Accept,Authorization,Content-Type,X-Request-ID` | Comma-separated list of allowed CORS headers |

### Example: Custom CORS configuration

```bash
export CORS_ALLOWED_ORIGINS="https://app.example.com,https://web.example.com"
export CORS_ALLOWED_METHODS="GET,POST,PUT,DELETE"
export CORS_ALLOWED_HEADERS="Accept,Authorization,Content-Type"
go run ./cmd/server
```

### Building

```bash
make build
```

Binary written to `bin/server`.

### Testing

```bash
make test
```

### Code quality

Format code:
```bash
make fmt
```

Lint:
```bash
make lint
```

## Docker

### Build

```bash
docker build -t nester-api .
```

Multi-stage build ensures minimal runtime image (~15MB).

### Run

```bash
docker run -p 8080:8080 nester-api
```

Set port with environment variable:
```bash
docker run -p 9000:9000 -e PORT=9000 nester-api
```

## Architecture Philosophy

**Clean Architecture.** Domain logic is isolated from HTTP, database, and framework concerns. Changes to the web layer do not cascade into domain code.

**Domain Isolation.** `internal/domain/*` packages own their boundaries. Cross-domain dependencies flow only through service/handler layers.

**Minimal Abstraction.** Uses Chi for routing and `net/http` for the server. Avoids premature indirection. Repository pattern introduced only where data access is complex.

**Simplicity First.** No dependency injection containers, no global singletons, no reflection-based magic. Code is explicit and traceable.

## Implementation Phases

### ✓ Phase 1: HTTP Router Foundation (Issue #5)
- Chi router with `/api/v1/` prefix
- Request ID, logging, recoverer middleware
- Configurable CORS
- Health (`/healthz`) and readiness (`/readyz`) endpoints
- Graceful shutdown with configurable timeout
- Structured JSON error responses

### Upcoming Phases
1. **Database layer** — PostgreSQL integration via `internal/repository`
2. **Auth middleware** — JWT validation in `internal/middleware`
3. **Vault domain** — Core business logic in `internal/domain/vault`
4. **Additional endpoints** — Domain-specific handlers under `/api/v1/`
5. **Integration tests** — End-to-end validation

---

*Go version: 1.22+*
*Module: github.com/Suncrest-Labs/nester*
