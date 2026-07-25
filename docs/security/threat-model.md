# Threat Model

> Version 1.0 — July 2026  
> Scope: Nester monorepo (Go API, Python Intelligence LLM, Next.js dApp)

## 1. System Overview

Nester is a DeFi yield aggregator built on the Stellar network. Users connect
wallets, create vaults, deposit assets, and earn yield through protocol
allocations. An AI assistant (Intelligence) provides portfolio insights, savings
coaching, and vault recommendations.

```
┌──────────────────────────────────────────────────────────────────┐
│                         Client (Browser)                         │
│                     Next.js dApp (SPA)                           │
└────────┬──────────────────────┬──────────────────────┬───────────┘
         │ HTTPS                │ HTTPS                │ HTTPS
         ▼                      ▼                      ▼
┌────────────────┐  ┌─────────────────────┐  ┌────────────────────┐
│   Go API        │  │ Python Intelligence  │  │ Stellar Network    │
│ (cmd/api)       │  │ (FastAPI + LLM)      │  │ (RPC / Horizon)    │
│                 │  │                      │  │                    │
│  PostgreSQL     │  │  Redis               │  │  Soroban Contracts │
│  Redis          │  │  Anthropic API       │  │                    │
└─────────────────┘  └──────────────────────┘  └────────────────────┘
```

## 2. Assets

### Critical

| Asset | Description | Impact if Compromised |
|---|---|---|
| User vaults | Yield-bearing positions on Stellar | Direct financial loss |
| Bank account credentials | AES-256-GCM encrypted PII | Identity theft, fraud |
| JWT signing secret | HS256 key for API and Intelligence auth | Account takeover, full API impersonation |
| Stellar operator secret | Signs on-chain transactions | Theft of protocol funds |
| Database (PostgreSQL) | All user data, vaults, transactions | Full data breach |

### High

| Asset | Description | Impact if Compromised |
|---|---|---|
| User session (JWT) | Bearer token for authenticated endpoints | Vault access, transaction creation |
| Challenge store (Redis) | SEP-53 challenge-response sessions | Auth bypass if challenges leaked |
| LLM prompt context | User financial data sent to Anthropic | Data exfiltration via prompt injection |

### Medium

| Asset | Description | Impact if Compromised |
|---|---|---|
| API rate limit state | Redis-backed token buckets | Rate limit bypass, DoS |
| CORS configuration | Allowed origins | CSRF from unauthorized origins |

## 3. Trust Boundaries

### TB1: Client ↔ Go API

- Transport: HTTPS
- Authentication: SEP-53 challenge-response → JWT issuance
- Controls: CORS, rate limiting (global/auth/write/wallet/settlement), request body size limit (1 MB), security headers

### TB2: Go API ↔ PostgreSQL

- Transport: Local/TLS database connection
- Authentication: Database credentials
- Controls: Parameterized queries (SQL injection prevented by Go sqlx), connection pooling

### TB3: Go API ↔ Redis

- Transport: Local/TLS Redis connection
- Authentication: Redis AUTH (when configured)
- Controls: Used for challenge store and distributed rate limiting

### TB4: Go API ↔ Stellar Network

- Transport: HTTPS (RPC/Horizon)
- Authentication: Operator secret key signs transactions
- Controls: Withdrawal slippage limits (BPS), transaction confirmation polling

### TB5: Go API ↔ Python Intelligence

- Transport: HTTP (internal service-to-service)
- Authentication: Service API key (optional, `INTELLIGENCE_NESTER_SERVICE_API_KEY`)
- Controls: Separate process, network isolation in production

### TB6: Python Intelligence ↔ Anthropic API

- Transport: HTTPS
- Authentication: Anthropic API key
- Controls: Model-level content filtering (Anthropic responsibility)

### TB7: Python Intelligence ↔ Redis

- Transport: Local/TLS Redis connection
- Controls: Rate limiting (30/min chat, 20/min coaching/analysis, 5/hr portfolio)

## 4. Entry Points

### 4.1 Go API Endpoints

**Public (no auth required):**

| Method | Path | Purpose |
|---|---|---|
| GET | `/health`, `/healthz`, `/readyz` | Liveness/readiness probes |
| GET | `/ws` | WebSocket upgrade |
| POST | `/api/v1/auth/challenge` | SEP-53 challenge generation |
| POST | `/api/v1/auth/verify` | SEP-53 challenge verification → JWT |
| GET | `/api/v1/banks/*` | Bank resolution (public) |
| GET | `/api/v1/yields/*` | Yield rates (public) |
| GET | `/api/v1/savings-goals/shared/*` | Shared savings goals |

**Authenticated (JWT required):**

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/v1/vaults` | List user vaults |
| POST | `/api/v1/vaults` | Create vault |
| GET | `/api/v1/vaults/{id}` | Get vault details + allocations |
| POST | `/api/v1/vaults/{id}/deposit` | Initiate deposit |
| POST | `/api/v1/vaults/{id}/withdraw` | Initiate withdrawal |
| GET | `/api/v1/vaults/{id}/allocations` | Get vault allocations |
| POST | `/api/v1/transactions` | Create transaction record |
| GET | `/api/v1/transactions/{hash}` | Get transaction by hash |
| GET | `/api/v1/transactions` | List user transactions |
| GET | `/api/v1/portfolio/summary` | Portfolio summary |
| POST | `/api/v1/settlements` | Create settlement |
| PATCH | `/api/v1/settlements/{id}` | Update settlement |
| GET | `/api/v1/savings-goals/*` | Savings goals CRUD |
| POST | `/api/v1/savings-goals` | Create savings goal |

**Admin (JWT + admin role required):**

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/v1/admin/rebalance` | Trigger rebalance |
| POST | `/api/v1/admin/pause` | Pause vault |
| POST | `/api/v1/admin/resume` | Resume vault |
| GET | `/api/v1/admin/*` | Admin operations |

### 4.2 Intelligence Endpoints

**Public:**

| Method | Path | Purpose |
|---|---|---|
| GET | `/health` | Health check |

**JWT-protected:**

| Method | Path | Rate Limit | Purpose |
|---|---|---|---|
| GET | `/intelligence/chat` | 30/min | SSE streaming chat |
| DELETE | `/intelligence/conversation` | — | Clear conversation |
| POST | `/intelligence/coaching` | 20/min | Savings coaching |
| GET | `/portfolio/{user_id}/insights` | 20/min | Portfolio insights |
| GET | `/market/sentiment` | 30/min | Market sentiment |
| GET | `/recommend/vault` | 20/min | Vault recommendation |
| POST | `/analyze` | 20/min | General analysis |
| POST | `/recommend/vault` | 20/min | Vault recommendation (POST) |
| GET | `/vaults/{vault_id}/recommendations` | 20/min | Vault recommendations |
| POST | `/portfolio/analyze` | 5/hr | Full portfolio analysis |
| POST | `/intelligence/savings-plan` | — | Savings plan generation |
| WS | `/ws/chat` | — | WebSocket chat |

### 4.3 WebSocket

- Path: `/ws` (Go API), `/ws/chat` (Intelligence)
- Authentication: Optional authenticator callback (Go), manual JWT decode from first frame (Intelligence)
- Channels: `vaults:global` (yield events), `vaults:{id}` (vault-specific), per-user (`users:{id}`)

## 5. Threat Actors

### 5.1 External Attacker (Unauthenticated)

- **Capability:** Network access, craft arbitrary HTTP requests
- **Motivation:** Financial gain, data theft
- **Primary targets:** Auth endpoints (brute force), public endpoints (reconnaissance), IDOR (data access)

### 5.2 Authenticated User (Legitimate)

- **Capability:** Valid JWT, access own vault/transactions
- **Motivation:** May attempt to access other users' data (IDOR), escalate privileges
- **Primary targets:** Cross-user vault access, admin endpoint access

### 5.3 Compromised Account

- **Capability:** Valid JWT from compromised credentials
- **Motivation:** Vault manipulation, fund theft, data exfiltration
- **Primary targets:** Deposit/withdrawal endpoints, transaction creation

### 5.4 Insider / Operator

- **Capability:** Access to secrets, database, infrastructure
- **Motivation:** Varies
- **Primary targets:** Stellar operator secret, JWT signing key, database

## 6. Security Controls

### 6.1 Authentication

- **SEP-53 challenge-response:** Cryptographic wallet-based authentication
- **JWT (HS256):** Custom implementation in `apps/api/internal/auth/jwt.go`
  - Constant-time HMAC comparison
  - Bearer token in Authorization header
  - Claims: `sub` (user ID), `exp` (expiration)
- **Intelligence JWT:** PyJWT library, same HS256 algorithm
  - Dev-mode bypass when `INTELLIGENCE_JWT_SECRET` is empty (blocked in non-development by Pydantic validator)

### 6.2 Authorization

- **Handler-level ownership checks** (IDOR prevention):
  - `GET /vaults/{id}`: Owner check via `vault.UserID == user.ID`
  - `GET /vaults/{id}/allocations`: Same pattern
  - `POST /transactions`: Owner check via vault lookup
  - `GET /transactions/{hash}`: Owner check via vault lookup
- **Admin role enforcement:** Middleware checks `role == "admin"` for `/api/v1/admin/*`
- **HarvestVault / RebalancePosition:** Ownership check at service layer (lines 609, 876 of `vault_service.go`)

### 6.3 Rate Limiting (Multi-Tier)

| Tier | Limit | Key | Scope |
|---|---|---|---|
| Global | 100/min | IP | All requests |
| Auth | 10/min | IP | Challenge + verify |
| Write | 20/min | IP | POST/PUT/PATCH/DELETE |
| Wallet | 60/min | Wallet address | Vault operations |
| Settlement | 5/min | User ID | Settlement creation |
| Rebalance | 3/hr | — | Admin rebalance |

- Redis-backed (distributed), falls back to in-memory when Redis unavailable
- X-Forwarded-For parsing respects `TRUSTED_PROXY_COUNT` (default 0 = ignores XFF)

### 6.4 Input Validation

- **Request body size limit:** 1 MB (`middleware.LimitRequestBody`)
- **SQL injection prevention:** Parameterized queries via sqlx
- **UUID validation:** Path parameters validated before use
- **Type validation:** Transaction types validated against allowlist
- **Decimal validation:** Amount parsed via `shopspring/decimal`

### 6.5 Transport Security

- **HTTPS** for all external communication
- **CORS** configured per environment via `ALLOWED_ORIGINS`
- **Security headers** applied per environment

### 6.6 Data Protection

- **Bank accounts:** AES-256-GCM encryption at rest with key versioning
- **Keys:** Never logged, sourced from environment/secrets manager
- **Challenge store:** Redis with TTL expiry, atomic `GetDel` prevents replay

### 6.7 Resilience

- **Panic recovery:** Middleware catches panics, returns 500
- **Timeouts:** Read, write, idle, and header timeouts configured
- **Circuit breakers:** None currently (future improvement)

## 7. Authentication Flow

```
Client                          Go API                         Stellar
  │                               │                               │
  │  POST /auth/challenge         │                               │
  │  { "address": "G..." }       │                               │
  │ ─────────────────────────────>│                               │
  │                               │  Generate random challenge    │
  │                               │  Store in Redis with TTL      │
  │  { "challenge": "abc..." }   │                               │
  │ <─────────────────────────────│                               │
  │                               │                               │
  │  Sign challenge with wallet   │                               │
  │  POST /auth/verify            │                               │
  │  { "address", "signed" }     │                               │
  │ ─────────────────────────────>│                               │
  │                               │  GetDel from Redis (atomic)   │
  │                               │  Verify ed25519 signature     │
  │                               │  Look up or create user       │
  │                               │  Issue JWT (HS256)            │
  │  { "token": "eyJ..." }       │                               │
  │ <─────────────────────────────│                               │
  │                               │                               │
  │  Authorization: Bearer eyJ... │                               │
  │ ─────────────────────────────>│                               │
  │                               │  Parse JWT, extract user ID   │
  │  Protected resource           │                               │
  │ <─────────────────────────────│                               │
```

## 8. Assumptions

1. **Stellar network** is Byzantine-fault-tolerant; on-chain transactions are immutable once confirmed
2. **PostgreSQL** is not directly accessible from the public internet
3. **Redis** is not directly accessible from the public internet
4. **Anthropic API** applies its own content filtering to LLM inputs/outputs
5. **HTTPS** is enforced at the load balancer / reverse proxy level
6. **JWT signing secret** is stored in a secrets manager, not source control
7. **Stellar operator secret** is stored in a secrets manager, not source control
8. **Database credentials** are stored in a secrets manager, not source control
9. **Single-tenant deployment:** Each Nester instance serves one organization
10. **dApp is served over HTTPS** and does not introduce XSS via client-side code

## 9. Residual Risk

### 9.1 Deposit Flow (Critical — Business Logic)

**Finding:** The deposit endpoint computes shares from the user-supplied `amount`
without on-chain slippage protection. The `min_shares_out` parameter is hardcoded
to 0, meaning the user cannot protect against unfavorable execution.

**Status:** Acknowledged. Fixing requires API contract changes and on-chain
verification. Documented as recommendation for maintainers.

**Risk:** An attacker who can manipulate `price_per_share` (currently fetched
from Horizon) could inflate shares. Mitigated by Horizon being a trusted data
source, but the lack of user-side slippage protection remains.

### 9.2 Prompt Injection (Medium)

**Finding:** Intelligence endpoints pass user input directly to Anthropic's API
without input sanitization, length limits, or system prompt isolation beyond
Anthropic's defaults.

**Status:** Acknowledged. Per-user isolation limits blast radius to individual
conversations. No shared-memory cross-user contamination possible.

**Risk:** An attacker could exfiltrate their own session data or manipulate
the LLM's responses. Cannot access other users' data due to per-user isolation.

### 9.3 IDOR in Service Layer (Low-Medium)

**Finding:** Authorization is implemented at the handler level, not the service
layer. System-level callers (scheduler, rebalance, TVL tracker, projections)
bypass handler-level checks by calling service methods directly.

**Status:** Acknowledged. Service methods are called by trusted internal
components only. Refactoring 60+ callers would introduce architectural risk
disproportionate to the security benefit for this PR.

**Risk:** A future developer could add a new HTTP handler that calls
`vaultService.GetVault()` without adding ownership checks. Mitigated by
establishing the pattern in existing handlers and documenting the limitation.

### 9.4 Custom JWT Implementation (Low-Medium)

**Finding:** The JWT implementation uses a custom HMAC-SHA256 library rather
than a battle-tested library. Missing standard claims: `nbf` (not-before),
`iss` (issuer), `aud` (audience), `jti` (unique token ID).

**Status:** Core implementation is sound — constant-time comparison, HS256
hardcoded. Missing claims are defense-in-depth improvements, not critical
vulnerabilities.

**Risk:** Token replay across services if `iss`/`aud` are not validated.
Mitigated by service-specific secret keys.

### 9.5 WebSocket Auth Optional (Low)

**Finding:** The WebSocket authenticator callback is always non-nil in
production, but the hub design allows a nil authenticator.

**Status:** Not exploitable in production. The authenticator is always set in
`cmd/api/main.go`.

**Risk:** If a future binary constructs the hub without an authenticator,
WebSocket connections would be unauthenticated. No nil check currently exists
in the hub — a defense-in-depth guard is recommended (see pentest report).

## 10. Future Improvements

### 10.1 Service-Layer Authorization

Move ownership checks from handlers to the service layer. Requires:
- Adding `userID` parameter to `GetVault`, `GetAllocations`, etc.
- Updating 60+ callers (service, scheduler, rebalance, TVL, projections)
- Updating all tests

**Recommendation:** Do as a separate PR after this security PR merges.

### 10.2 Deposit On-Chain Verification

Verify deposit transactions on-chain before crediting shares:
- Fetch `TxMeta` from Horizon
- Extract actual asset amounts from `Operations`
- Validate against expected amounts
- Enforce minimum `min_shares_out`

**Recommendation:** Requires API contract change. Design with maintainers.

### 10.3 Prompt Injection Hardening

- Add input length limits (max 10K characters)
- Separate system prompt from user input in Anthropic API calls
- Add output filtering for sensitive patterns (addresses, keys)
- Log suspicious prompt patterns for review

**Recommendation:** Implement as Intelligence service enhancement.

### 10.4 JWT Claim Enhancements

Add standard JWT claims:
- `iss` (issuer): `"nester-api"` / `"nester-intelligence"`
- `aud` (audience): Service-specific audience
- `nbf` (not-before): Token issuance time
- `jti` (JWT ID): Unique token identifier for revocation

**Recommendation:** Low priority. Implement when token revocation is needed.

### 10.5 Request ID Propagation

Add `X-Request-ID` header propagation across Go API ↔ Intelligence for
end-to-end tracing and security event correlation.

### 10.6 Security Audit Logging

Centralize security-relevant events (failed auth, IDOR attempts, rate limit
hits) into a dedicated audit log with structured fields for SIEM integration.

## Appendix A: Modified Files in This PR

| File | Change | Security Impact |
|---|---|---|
| `apps/intelligence/app/config.py` | Pydantic model_validator rejects empty JWT secret in non-development | Prevents auth bypass in production |
| `apps/api/internal/handler/vault_handler.go` | Ownership check in `getVault()` and `getAllocations()` | Prevents IDOR on vault read |
| `apps/api/internal/handler/transaction_handler.go` | Ownership check in `createTransaction()` and `getTransactionByHash()` | Prevents IDOR on transaction create/read |
| `apps/api/cmd/api/main.go` | Wires `vaultRepository` into `TransactionHandler` | Enables ownership checks |
| `apps/intelligence/tests/test_config.py` | Tests for JWT config validation | Verifies fix works in dev/staging/prod |
| `apps/api/internal/handler/vault_idor_test.go` | IDOR prevention tests for vault endpoints | Verifies ownership checks |
| `apps/api/internal/handler/transaction_handler_test.go` | IDOR prevention + regression tests for transaction endpoints | Verifies ownership checks |
