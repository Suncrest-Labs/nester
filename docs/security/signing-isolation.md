# Signing Isolation Architecture

How Nester's Soroban transaction signing key is held, what the signer will and
will not sign, and how signing is halted during an incident.

Companion documents: [`signing-threat-model.md`](./signing-threat-model.md)
(what this defends against and what it does not),
[`incident-response.md`](./incident-response.md) (the runbook),
[`key-rotation.md`](./key-rotation.md) (account cipher rotation).

---

## 1. The problem

Before this change, `stellar.ContractInvoker` parsed `STELLAR_OPERATOR_SECRET`
into a `keypair.Full` and held it for the lifetime of the API process
(`invoker.go:36`), applying it unconditionally at three call sites. Two
consequences followed:

1. **Code execution in the API yielded the key itself**, not merely the ability
   to request a signature. An attacker with a read primitive against process
   memory, a heap dump, or an RCE walked away with the operator secret and could
   sign anything, anywhere, for as long as the key remained valid.
2. **Nothing evaluated what was being signed.** There was no policy layer, so
   any transaction the code could be induced to build would be signed.

## 2. Why a separate process, and not a KMS

Two architectures were considered against the actual deployment model
(`docker-compose.yml`: API, Postgres, Redis as separate
containers; no cloud IAM, no workload identity provider).

| | Separate signer process | Cloud KMS / HSM |
|---|---|---|
| Deployable in the current model | Yes — another container, socket volume shared with the API | No — requires a cloud IAM identity the deployment does not have |
| Key extractable by API compromise | No | No |
| Key extractable by signer-host compromise | Yes | No |
| Operational cost | A process to run and monitor | Provider dependency, per-call cost, network dependency on the hot path |

**A separate process was chosen.** A KMS would provide strictly stronger key
custody — the key would be non-extractable even from the signer host — but
Nester has no cloud IAM binding to authenticate to one, and adding a stub or
"local KMS" implementation would be exactly the fake security control this work
is meant to avoid. The `signing.Backend` interface is the seam: a real
KMS-backed implementation can replace `stellar.SigningBackend` without touching
the policy, audit, or kill-switch layers above it.

**This is a real but bounded improvement, and the boundary is stated plainly in
the threat model rather than overclaimed here.**

## 3. Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ API container                                               │
│                                                             │
│  handler ──▶ service ──▶ ContractInvoker                    │
│                              │                              │
│                         builds + simulates                  │
│                         (no key needed)                     │
│                              │                              │
│                         RemoteSigner ──▶ signing.Client     │
│                                                │            │
│  env: DATABASE_DSN, AUTH_JWT_SECRET,           │            │
│       ACCOUNT_CIPHER_KEYS, provider keys       │            │
│  env: STELLAR_OPERATOR_SECRET — ABSENT         │            │
└────────────────────────────────────────────────┼────────────┘
                                                 │
                              unix socket 0660, or mTLS
                              carries: typed Intent
                              never:   raw bytes, hashes, XDR to sign
                                                 │
┌────────────────────────────────────────────────┼────────────┐
│ signer container                               ▼            │
│                                                             │
│  signing.Server                                             │
│    1. authenticate caller  ──▶ reject → 401, audited        │
│    2. kill switch          ──▶ engaged → 503, audited       │
│    3. Intent.Validate      ──▶ reject → 422, audited        │
│    4. Policy.Evaluate      ──▶ reject → 422, audited        │
│    5. ReplayGuard.Observe  ──▶ reject → 422, audited        │
│    6. SigningBackend.BuildAndSign                           │
│         rebuilds the transaction from the intent            │
│         simulates, patches fee, signs                       │
│                                                             │
│  env: STELLAR_OPERATOR_SECRET                               │
│  env: DATABASE_DSN — ABSENT (no database access at all)     │
│  returns: a signed envelope. Never key material.            │
└─────────────────────────────────────────────────────────────┘
```

Two properties of this layout are worth stating explicitly:

- **The signer cannot broadcast.** It returns a signed envelope; submission
  stays with the API. A compromised signer therefore cannot quietly push
  transactions without the API observing them.
- **The signer has no database credentials.** Its audit events go to a
  structured log stream. This means the signer cannot write to the tamper-evident
  chain directly, which is a deliberate trade: giving the signer a database
  connection would hand an attacker who compromised it a second, far broader
  capability.

## 4. What the signer will sign

The signing boundary carries a **typed transaction intent**, never bytes.

A `Sign(raw []byte)` primitive would make the signer a general signing oracle:
anyone who reached it could obtain a signature over anything, and moving the key
into its own process would buy almost nothing. Instead the caller describes what
it wants done, and **the signer rebuilds the transaction itself** and signs what
it built. A signature can only ever cover a transaction the signer constructed
from an intent it approved.

### Authorized operations

The complete set, mirroring the eight call sites in
`internal/service/soroban_vault_chain_invoker.go`:

| Operation | Shape | Arguments |
|---|---|---|
| `pause` | `void` | operator address as caller |
| `unpause` | `void` | operator address as caller |
| `rebalance` | `void` | operator address as caller |
| `emergency_withdraw_all` | `void` | operator address as caller |
| `deposit` | `i128_pair` | amount, minimum shares out |
| `withdraw` | `i128_pair` | shares, minimum assets out |
| `harvest` | `address_bool` | user address, compound flag |
| `set_weights` | `weights` | vector of (protocol, weight_bps) |

Adding a signable operation is a code change in the signer, reviewed as such.
It is deliberately not configuration the API can influence at runtime.

### Validation, in order

**Structural** (`Intent.Validate`) — invariants of the protocol:

- The operation is one of the eight above; anything else is `unknown_operation`.
- The declared shape matches the operation. A `pause` intent carrying an amount
  is `shape_mismatch` — this is what stops a caller naming a permitted
  no-argument function and attaching arguments to it.
- Addresses are well-formed strkeys of the right kind (`C…` for contracts,
  `G…` for accounts).
- Amounts are positive; slippage guards are non-negative.
- Weight vectors are non-empty, bounded to 32 entries, free of duplicate
  protocols, each entry within 0–10000 bps, and summing to exactly 10000.

**Policy** (`Policy.Evaluate`) — deployment choices:

- The network passphrase matches the signer's. This is what prevents a
  testnet-configured caller obtaining a mainnet-valid signature.
- The contract address is in the allowlist.
- The operation is permitted by this deployment (a subset of the eight).
- For value-moving operations, the amount is within
  `SIGNER_MAX_AMOUNT_STROOPS`.
- The intent has not expired (`SIGNER_MAX_INTENT_AGE`, default 2 minutes) and is
  not dated implausibly far in the future (`SIGNER_CLOCK_SKEW`, default 30s).

**Replay** (`ReplayGuard`) — an intent ID already signed is refused.

**Empty allowlists permit nothing.** A signer configured with no contracts, or
no operations, signs nothing. Misconfiguration fails closed.

### The limit worth understanding

The function allowlist is not the valuable part — the application legitimately
calls all eight, so an attacker impersonating the API can request them too. The
value is in **the amount bound, the contract allowlist, and the expiry**: they
cap what a single compromised request can do, and every attempt is recorded.

## 5. Caller authorization

Two transports, both stronger than a shared secret held in the same environment
as the caller it authenticates.

### Unix domain socket (preferred, co-located)

The socket is created with restrictive permissions (`SIGNER_SOCKET_MODE`,
default `0660`). Only processes running as a user in the socket's group can
connect at all, so an unauthorized caller is refused by the kernel before any
application code runs. World-writable modes are rejected at startup.

### Mutual TLS (networked)

`RequireAndVerifyClientCert` is not configurable — a signer accepting
unauthenticated TLS would have encryption and no authorization. The client CA
pool starts **empty** rather than from the system roots, so only the configured
issuer can authenticate; inheriting the public root store would let any
publicly-trusted certificate in. TLS 1.3 minimum. Caller identity is the client
certificate common name.

### Why not a shared secret

`SIGNER_SHARED_SECRET` in the API's environment would authenticate "whoever
holds the API's environment" — precisely the attacker the boundary exists to
contain. It is not used.

## 6. Kill switch

**Mechanism:** a sentinel file at `SIGNER_KILL_SWITCH_PATH`. Its presence
disables signing.

**Why a file:** it requires host- or orchestrator-level access to the signer,
not an application credential. The authority to halt signing should not be
obtainable by compromising the application whose signing you are halting. It
also works when the database is down, when Redis is down, and when the API is
entirely compromised.

**Fail-closed:** three states — `enabled` (no sentinel), `disabled` (sentinel
present), and `unknown` (the sentinel could not be read: permission denied, I/O
error, broken mount). **`unknown` blocks signing.** Being unable to confirm that
signing is permitted is not permission to proceed.

**Behaviour under failure:**

| Condition | Signing |
|---|---|
| Database unavailable | Continues — the switch does not depend on it |
| Redis unavailable | Continues |
| Sentinel volume unmounted | **Blocked** (`unknown`, fail-closed) |
| Signer restarts | State is re-read from disk; a sentinel written before the restart still disables signing |
| API restarts | Unaffected; the switch lives with the signer |

**Activation** (documented fully in the runbook):

```bash
docker compose exec signer touch /var/run/nester-signer/signing.disabled
```

Or, if the signer container is unreachable, write the sentinel directly to the
host path backing the volume. **Recovery** is removing it — a separate,
deliberate action carrying the same authority requirement.

Every activation, every refusal, and every recovery is audited.

## 7. Latency budget

Signing is on the operational hot path. The boundary adds:

| Stage | Budget | Notes |
|---|---|---|
| API → signer transport | < 2 ms | Unix socket; no TLS handshake per call with keep-alive |
| Caller authentication | < 1 ms | Socket permissions are enforced by the kernel at connect |
| Structural validation | < 1 ms | In-memory field checks |
| Policy evaluation | < 1 ms | Map lookups and integer comparisons |
| Replay guard | < 1 ms | In-memory map with an amortised sweep |
| **Boundary overhead subtotal** | **< 5 ms** | Everything this change adds |
| Build + simulate (RPC) | 100–2000 ms | Pre-existing; dominated by the Soroban RPC round trip |
| Ed25519 signature | < 1 ms | |

**The boundary adds single-digit milliseconds to an operation already dominated
by a network round trip to the Soroban RPC node.** The client timeout is 15
seconds (`DefaultClientTimeout`), sized to accommodate simulation while still
surfacing a hung signer.

Measured per-request latency is recorded on every audit event (`latency_ms`) and
aggregated in the signer counters (mean, max, sample count), so the budget above
can be checked against production rather than assumed.

## 8. Configuration

### Signer process

| Variable | Required | Purpose |
|---|---|---|
| `STELLAR_OPERATOR_SECRET` | yes | The signing key. **Only this process.** |
| `STELLAR_RPC_URL` | yes | Soroban RPC for simulation |
| `STELLAR_HORIZON_URL` | yes | Horizon, for sequence numbers |
| `STELLAR_NETWORK_PASSPHRASE` | yes | Pins the network |
| `SIGNER_KILL_SWITCH_PATH` | yes | Sentinel path. No default — a signer that cannot be halted must not start |
| `SIGNER_ALLOWED_CONTRACTS` | yes | Comma-separated allowlist. No default |
| `SIGNER_SOCKET_PATH` | one of | Unix socket path |
| `SIGNER_LISTEN_ADDR` | one of | TCP address for mTLS |
| `SIGNER_SOCKET_MODE` | no | Default `0660`; world-writable rejected |
| `SIGNER_ALLOWED_OPERATIONS` | no | Defaults to all eight |
| `SIGNER_MAX_AMOUNT_STROOPS` | no | Per-transaction cap; `0` disables the check |
| `SIGNER_MAX_INTENT_AGE` | no | Default `2m` |
| `SIGNER_CLOCK_SKEW` | no | Default `30s` |
| `SIGNER_TLS_CERT_FILE` / `_KEY_FILE` / `_CLIENT_CA_FILE` | mTLS only | |

### API process

The API **must not** have `STELLAR_OPERATOR_SECRET` set in an isolated
deployment. It needs the operator's *public* address to build transactions
against the right source account — public data that grants no signing
capability.

### Migration path

`stellar.LocalSigner` preserves the previous in-process custody for local
development and for deployments that have not yet split out the signer. It is
**not** the recommended production configuration, and the security posture
described here assumes a `RemoteSigner`. Startup logs which mode is active
precisely so this is visible in production rather than assumed.

## 9. Observability

Emitted per signing request, with deliberately low-cardinality labels
(operation, outcome, and rejection category are all small closed sets):

| Signal | Use |
|---|---|
| Requests by operation | Baseline volume |
| Signatures by operation | Successful signing rate |
| Rejections by category | **The primary compromise signal** — a burst of `contract_not_allowed` or `amount_out_of_policy` is what an attacker probing the policy looks like |
| Authorization failures | Someone reaching the socket who should not |
| Kill-switch refusals | Requests arriving while signing is halted |
| Latency (mean, max) | Budget verification |

Wallet addresses, user IDs, transaction hashes, and intent IDs are **not** used
as metric labels — they are unbounded. They live in the audit record instead,
which is where an investigator needs them.

## 10. Honest limitations

- **The signer host is now the target.** Code execution in the signer process,
  or root on its host, yields the key. The boundary raises the required
  capability; it does not eliminate the target.
- **A compromised API can still request policy-permitted transactions.** The
  defences there are the amount bound, volume anomaly detection, and the kill
  switch — not the policy allowlist.
- **The replay guard is process-local and in-memory.** It does not survive a
  restart and does not coordinate across replicas. That is sufficient for the
  current single-signer deployment; a multi-replica deployment needs a shared
  store, and the interface is small enough to swap.
- **The signer's audit events go to a log stream, not the hash chain**, because
  it deliberately has no database credentials. Chain-backed signing audit
  requires either giving the signer a database connection (widening its
  compromise blast radius) or having the API mirror signer events into the chain
  (which a compromised API could then omit). The current trade is documented
  rather than silently resolved.
- **Signals are emitted, not alerted.** Wiring them into a paging system is the
  recommended follow-up.
