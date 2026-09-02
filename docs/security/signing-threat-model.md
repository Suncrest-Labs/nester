# Signing Path Threat Model & Secret Inventory

Scope: the operational signing hot path — the credentials that authorize Soroban
contract invocations, the process that holds them, and what an attacker gains by
compromising each one.

This document is deliberately narrow. The system-wide threat model lives in
[`threat-model.md`](./threat-model.md) and is not restated here; this file covers
only what that document leaves unaddressed: **key custody, signing authority, and
the detection/response posture around them.**

Related: [`key-rotation.md`](./key-rotation.md) (account cipher rotation),
[`signing-isolation.md`](./signing-isolation.md) (the boundary design),
[`incident-response.md`](./incident-response.md) (the runbook).

---

## 1. Pre-change baseline (what this issue found)

Reconnaissance of `dev` at commit `9246829` established the following facts.
These are the conditions the rest of this document reasons about, and the
conditions the accompanying change set alters.

| Observation | Evidence | Consequence |
|---|---|---|
| The API process parses and holds the raw Stellar operator secret | `internal/stellar/invoker.go:36` — `keypair.ParseFull(operatorSecret)` stored as `ContractInvoker.kp` | Any code execution in the API process yields the signing key itself, not merely the ability to request a signature |
| Signing is unconditional at three call sites | `invoker.go:179`, `:555`, `:811` — `inner.Sign(c.networkPassphrase, c.kp)` | No policy evaluates *what* is being signed before the key is applied |
| There is no signing kill switch | No disable path exists in `ContractInvoker` or its callers | Containment during an incident requires a redeploy |
| Signing produces no dedicated audit record | `internal/audit` is not referenced from `internal/stellar` | Post-incident, there is no authoritative record of what was signed |
| The account cipher is direct multi-key AES-GCM, not envelope encryption | `internal/crypto/account_cipher.go` — one AEAD per version, applied directly to plaintext | Master-key rotation requires decrypting and re-encrypting every row |
| Rotation re-encrypts full plaintext per row | `internal/rotation/rotation.go:rotateRow` — `Decrypt` then `Encrypt` | Rotation cost scales with data volume; plaintext transits the rotator for every row |

The signing surface itself is small and closed, which is what makes a real
allowlist policy feasible rather than aspirational:

| Contract function | Called from | Signed? |
|---|---|---|
| `pause`, `unpause` | `soroban_vault_chain_invoker.go:35,39` | yes |
| `rebalance` | `:43` | yes |
| `set_weights` | `:62` | yes |
| `deposit` | `:69` | yes |
| `withdraw` | `:91` | yes |
| `harvest` | `:100` | yes |
| `emergency_withdraw_all` | `:151` | yes |
| `preview_deposit`, `preview_withdraw`, `preview_withdraw_net` | query paths | no — simulation only |

Eight signing operations across seven distinct functions. Nothing else in the
repository legitimately requires the operator key.

---

## 2. Secret inventory

Every security-sensitive credential the API loads, enumerated from
`internal/config/config.go`. For each: what it is, where it lives, who can reach
it, what compromise permits, and — the field most often left vague — **whether
compromise is actually detectable today.**

Where detection is not currently possible, this document says so explicitly
rather than implying a control that does not exist.

---

### 2.1 `STELLAR_OPERATOR_SECRET` — Soroban transaction signing key

| Field | Value |
|---|---|
| **What** | Ed25519 Stellar secret seed (`S...`) authorizing all contract invocations |
| **Storage** | Environment variable; `config.go:233` via `loader.stringDefault` (defaults to empty) |
| **Process access (before)** | The API process — parsed into `keypair.Full` and held in memory for the process lifetime |
| **Process access (after)** | The signer process only. The API holds no key material |
| **Compromise permits** | Signing arbitrary transactions as the operator: pausing/unpausing vaults, forcing rebalances, setting allocation weights, calling `emergency_withdraw_all` on any vault address, and submitting deposits/withdrawals. The operator is the privileged caller for the vault contracts, so this is the highest-value credential in the system |
| **Blast radius** | Every vault the operator administers. Funds are not directly transferable to an attacker address by the operator key alone (the vault contracts constrain destinations), but `emergency_withdraw_all` and `set_weights` allow forced position exits and allocation manipulation — i.e. economic damage and denial of yield, plus griefing via `pause` |
| **Rotation mechanism** | Manual: generate a new keypair, transfer operator authority on-chain to the new address, update the environment, restart. There is no in-process rotation and no dual-key overlap window |
| **Rotation frequency** | No policy exists. This is a gap and is named as such in the runbook |
| **Detection (before)** | **Not detectable in-process.** Signing emitted no audit record and no metric. Compromise would surface only as anomalous on-chain activity noticed out-of-band |
| **Detection (after)** | Every signature attempt — accepted or rejected — produces a hash-chained audit event and a counter. Policy rejections, authorization failures, and volume anomalies are the detection signals. Detection latency is bounded by how often those signals are reviewed, not by whether the data exists |
| **Revocation** | On-chain: transfer operator authority away from the compromised address. Off-chain and immediate: the kill switch stops the signer from applying the key at all, which is the containment step that precedes the slower on-chain rotation |
| **Affected parties** | All vault users — via forced exits, paused vaults, or manipulated allocations |
| **Emergency response** | Kill switch → confirm via audit chain what was signed and when → assess on-chain blast radius → rotate operator authority → restore. See [`incident-response.md`](./incident-response.md) |

**Precise statement of the residual risk:** isolating the key into a separate
process does not make the operator key unstealable. It changes the attacker's
required capability from *read API process memory* to *achieve code execution in
the signer process, or compromise the signer's host*. What it buys concretely: a
compromised API can now only request signatures for transaction shapes the
policy permits, every such request is recorded, and the kill switch revokes the
capability without a redeploy.

---

### 2.2 `AUTH_JWT_SECRET` — authentication signing secret

| Field | Value |
|---|---|
| **What** | HMAC secret signing access and refresh tokens |
| **Storage** | Environment variable, `config.go` |
| **Process access** | The API process |
| **Compromise permits** | Forging valid authentication material for any user, including any privileged user, without touching the credential store |
| **Blast radius** | Every account. An attacker with this secret authenticates as anyone |
| **Rotation mechanism** | Change the environment variable and restart. There is no key-ID header on issued tokens and no dual-secret verification window, so rotation invalidates every live session at once |
| **Rotation frequency** | No policy exists |
| **Detection** | **Compromise of this secret is not cryptographically detectable.** A forged token is, by construction, indistinguishable from a legitimately issued one — it verifies against the same secret. Detection therefore depends entirely on *behavioural* telemetry: sessions appearing without a corresponding login event, impossible-travel patterns, or privilege use inconsistent with the account history. None of those correlations are implemented today. **Current detection latency: unbounded.** |
| **Revocation** | Rotating the secret invalidates all tokens signed under it — including every legitimate session. There is no per-token or per-user revocation path that does not also log out the entire user base |
| **Affected parties** | All authenticated users |
| **Emergency response** | Rotate the secret, accept the global logout, then investigate which actions were taken by forged sessions using the audit log — noting that the audit log records the *claimed* identity, which a forged token controls |

This entry is stated at length because it is the clearest case in the inventory
where the honest answer is "we cannot detect this." Recording that plainly is
more useful than a control that does not exist.

---

### 2.3 `ACCOUNT_CIPHER_KEYS` / `ACCOUNT_CIPHER_ACTIVE_KEY` / `ACCOUNT_CIPHER_FINGERPRINT_KEY`

| Field | Value |
|---|---|
| **What** | Versioned AES-256-GCM keys protecting encrypted secrets at rest (webhook signing secrets), plus a stable HMAC pepper for uniqueness fingerprints |
| **Storage** | Environment variables; parsed in `internal/crypto/account_cipher.go` |
| **Process access** | The API process |
| **Compromise permits** | Decrypting every stored ciphertext for versions the attacker holds |
| **Blast radius** | All records encrypted under the compromised versions. Because the key version is stored per row, the radius is bounded by which versions leaked — this is the practical security benefit of versioning |
| **Rotation mechanism** | Add a new version, set it active, re-encrypt. Old versions must remain configured until no row references them |
| **Rotation frequency** | No policy exists. `key-rotation.md` documents the mechanism but not a cadence |
| **Detection** | **Not detectable.** Decryption performed outside the application — by anyone holding the key and a database copy — produces no application-observable event. Only key *use inside the API* could be instrumented, and an attacker with the key and the ciphertext has no reason to use the API |
| **Revocation** | Rotate to a new version and re-wrap. Note that rotation protects *future* reads; data already exfiltrated under the old key stays readable forever. Rotation is not a remedy for a completed exfiltration |
| **Affected parties** | Owners of every record sealed under the compromised versions |
| **Emergency response** | Treat as a data breach: rotate, re-wrap, and follow the disclosure path — rotation alone does not undo the exposure |

**Envelope encryption changes the rotation economics, not the breach outcome.**
With per-record data keys wrapped by a master key, rotating the master key
rewraps a fixed-size wrapped key per row instead of decrypting and re-encrypting
every record. That reduces rotation from an expensive, plaintext-handling
migration to a cheap metadata operation — which is what makes *frequent* rotation
and *fast emergency* rotation realistic.

---

### 2.4 `BANK_ACCOUNT_ENCRYPTION_KEY` — legacy single-key cipher

| Field | Value |
|---|---|
| **What** | The pre-versioning single encryption key |
| **Storage** | Environment variable |
| **Compromise permits** | Decrypting records still sealed under `LegacyKeyVersion` (`v1`) |
| **Detection** | Not detectable, for the same reason as 2.3 |
| **Note** | Retained for backward compatibility. It is the same class of secret as 2.3 and inherits its response |

---

### 2.5 `DATABASE_DSN` — PostgreSQL credentials

| Field | Value |
|---|---|
| **What** | Connection string including database username and password |
| **Storage** | Environment variable |
| **Process access** | The API process, migration runner, and operational commands |
| **Compromise permits** | Full read/write access to application data: user records, encrypted secrets (ciphertext only — see below), vault positions, and **the audit log itself** |
| **Blast radius** | The entire dataset. Critically, an attacker with write access to the database can rewrite audit rows |
| **Rotation mechanism** | Rotate the database role password, update the environment, restart |
| **Detection** | Partially detectable — connections from unexpected sources are visible in PostgreSQL logs if those logs are collected and reviewed. Queries issued through a legitimate connection string are not distinguishable from application traffic |
| **Revocation** | Revoke the role |
| **Interaction with the audit chain** | This is the boundary condition the audit design must be honest about. The hash chain makes tampering **evident**, not impossible: an attacker who rewrites a historical row breaks the chain link and `VerifyChain` reports the first bad sequence. An attacker who can also *recompute* the chain forward from the modified row produces a chain that verifies — unless the chain has been anchored externally. **Tamper-evidence is therefore only as strong as the anchoring.** See §4 |
| **Emergency response** | Rotate credentials, verify the audit chain, compare against external anchors |

---

### 2.6 Service credentials

`NESTER_SERVICE_API_KEY`, `REDIS_ADDR` (and any credentials embedded in it).

| Field | Value |
|---|---|
| **Storage** | Environment variables |
| **Process access** | The API process |
| **Compromise permits** | The service API key allows calling the API's service-to-service surface directly, bypassing the user authorization layer. Redis access allows reading and manipulating cached data, rate-limit counters, and scheduler leader locks — the last of which permits denial of scheduled operations |
| **Rotation** | Service keys rotate by changing the environment on both sides |
| **Detection** | **Not currently detectable** — no authentication-failure or anomaly telemetry is collected for these paths |
| **Blast radius** | Bounded relative to 2.1–2.3 — these are materially lower-value but are enumerated because incident scoping requires the complete list |

---

### 2.7 Bootstrap admin credentials

`cmd/bootstrap-admin` provisions the initial administrative account.

| Field | Value |
|---|---|
| **Compromise permits** | Administrative access to the application |
| **Storage** | Supplied at invocation; not persisted as a long-lived environment secret |
| **Detection** | Administrative actions are recorded in the audit log, so *use* is detectable provided the chain is intact and reviewed |
| **Rotation** | Standard credential reset for the provisioned account |
| **Note** | The blast radius is application-level administration, not signing. The operator key is not reachable from an admin session once signing is isolated — that separation is a direct consequence of §3 |

---

## 3. Trust boundaries after this change

```
┌──────────────────────────────────────────────────────────┐
│ API process (untrusted with key material)                │
│                                                          │
│  handlers → services → SigningClient                     │
│                            │                             │
│  holds: DB creds, JWT secret, cipher keys, provider keys  │
│  does NOT hold: operator signing key                     │
└────────────────────────────┼─────────────────────────────┘
                             │  TB-S: signing boundary
                             │  unix socket (0660) or mTLS
                             │  intent, not raw bytes
┌────────────────────────────┼─────────────────────────────┐
│ Signer process             ▼                             │
│                                                          │
│  authenticate caller → validate intent against policy →  │
│  check kill switch → sign → audit                        │
│                                                          │
│  holds: STELLAR_OPERATOR_SECRET                          │
│  never returns: key material, under any request          │
└──────────────────────────────────────────────────────────┘
```

**TB-S is the boundary this issue introduces.** Its security properties:

1. **The API cannot obtain the key.** No signer response contains key material.
   The only capability crossing the boundary is "a signature over a transaction
   the policy permitted."
2. **The API cannot request arbitrary signatures.** The request carries a
   *typed transaction intent* — contract, function, arguments, network — not
   opaque bytes. The signer rebuilds the transaction from the intent and signs
   what it built, so a signature can only ever cover a transaction the signer
   itself constructed and validated.
3. **Callers are authenticated.** Unix socket file permissions, or mutual TLS
   where a socket is not available across container boundaries.
4. **Every crossing is recorded**, including rejections — rejections are the
   more valuable signal.

### What this boundary does not protect against

Stated plainly, because a boundary described only by its strengths is
misleading:

- **Code execution in the signer process** yields the key. The boundary raises
  the required capability; it does not eliminate the target.
- **A compromised API can still request policy-permitted transactions.** If an
  attacker controls the API, they can call `rebalance` or `harvest` repeatedly
  within policy. The defence there is volume anomaly detection and the kill
  switch, not the policy.
- **Root on the shared host** defeats socket permissions.
- **The policy is only as good as its bounds.** A policy that permits everything
  the application does is, by definition, permissive enough for an attacker
  impersonating the application. The value is in the *amount* limits, the
  destination limits, and the expiry — not in the function allowlist alone.

---

## 4. Audit integrity model

The audit chain provides **tamper-evidence**, and specifically not immutability.
The distinction matters during an incident and is stated here precisely:

| Attacker capability | Result |
|---|---|
| Modify one historical row | `VerifyChain` fails at that sequence — the recomputed `entry_hash` no longer matches, and every subsequent `prev_hash` link is broken |
| Delete a row | Detected as a sequence gap and a broken link |
| Reorder rows | Detected — `sequence` is embedded in the hashed canonical form, so a row hash binds it to its position |
| Modify a row **and** recompute all subsequent hashes | **Not detected by chain verification alone.** The rewritten chain is internally consistent |
| The same, but the chain has been externally anchored | Detected — the recomputed chain diverges from the anchored hash |

Therefore: **the chain guarantee is only as strong as its anchoring.** An
attacker holding `DATABASE_DSN` write access has exactly the capability needed
to perform the fourth row. Anchoring — writing periodic entry hashes to an
append-only store outside the database — is what closes it, and
`AuditService.AnchorLatestEntry` exists for that purpose. Any claim that the
audit log is "immutable" without anchoring in place is false.

---

## 5. Detection posture summary

Honest accounting of what is detectable, before and after.

| Event | Before | After | Detection latency |
|---|---|---|---|
| Signing key used | Not recorded | Audit event + counter per signature | Bounded by review cadence |
| Signature rejected by policy | N/A (no policy) | Audit event + counter, with rejection reason | Bounded by review cadence |
| Unauthorized signer caller | N/A (no boundary) | Audit event + counter | Bounded by review cadence |
| Anomalous signing volume | Not detectable | Derivable from signing counters | Bounded by review cadence |
| Kill switch activated | N/A | Audit event | Immediate |
| Audit tampering | Chain verification existed but was not routinely run | Chain verification, plus anchoring where enabled | Bounded by verification cadence |
| `AUTH_JWT_SECRET` forgery | Not detectable | **Still not detectable** — see 2.2 | Unbounded |
| Cipher key exfiltration | Not detectable | **Still not detectable** — see 2.3 | Unbounded |

Two rows deliberately remain "not detectable." Closing them requires
behavioural session telemetry and database-access auditing respectively — both
outside the scope of this issue, both named here so they are not mistaken for
solved problems.

---

## 6. Detection latency: what "bounded by review cadence" means

The signing events above are *recorded* synchronously. Whether they are
*noticed* depends on something this repository does not currently have: an alert
pipeline. The signals are emitted as structured events and counters suitable for
scraping, and the runbook specifies which to inspect during an incident.

Stating this precisely: **this change makes signing compromise detectable in
principle and investigable in practice. It does not, on its own, make it
alerted.** Wiring these signals into a paging system is the recommended
follow-up, and is listed as such in the PR limitations.
