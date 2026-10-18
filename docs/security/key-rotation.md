# Account Cipher — Key Versioning & Rotation

Secrets stored at rest (webhook signing secrets) are encrypted with
**AES-256-GCM**. Each stored row records the **key version** that sealed it,
which lets us rotate encryption keys without rewriting or risking historical
data.

- Code: [`apps/api/internal/crypto/account_cipher.go`](../../apps/api/internal/crypto/account_cipher.go)

## Model

```
ciphertext = AES-256-GCM(nonce, plaintext, key[active_version])
stored     = { ciphertext:  nonce||ciphertext,
               key_version: active_version }
```

Every ciphertext is paired with the version of the key that produced it (a
`CipherEnvelope`). The stored bytes are exactly `nonce || ciphertext`; the
version lives in its own column alongside the ciphertext (for webhooks,
`webhooks.secret_key_version`).

| Concept          | Purpose                                             |
| ---------------- | --------------------------------------------------- |
| Key version      | Labels which key encrypted a given row              |
| Active key       | The one version used to encrypt **new** writes      |
| Legacy key(s)    | Retained **only** to decrypt un-rotated rows        |
| Fingerprint key  | Stable pepper for blind-index HMACs (see below)     |

### Encryption / decryption

- **Encrypt** → seal with the active key, tag with the active version.
- **Decrypt** → look up the key for the row's recorded version and open.
- A row whose version has **no** registered key fails with
  `ErrUnknownKeyVersion` — the signal that a still-needed legacy key was dropped
  too early. It never silently returns wrong data.

### Fingerprint (uniqueness) key

The cipher's blind-index HMAC is keyed by a **stable pepper**, independent of
the active encryption key. This is deliberate: if the fingerprint key changed
on every rotation, rows written before and after a rotation would no longer
collide and duplicate detection would break. Rotation therefore **never**
recomputes fingerprints.

When `ACCOUNT_CIPHER_FINGERPRINT_KEY` is empty the pepper defaults to the `v1`
key. That default is only available **while a `v1` key is configured** — a key
set that has no `v1` (e.g. after `v1` is dropped, or a set that starts at `v2`)
**must** set `ACCOUNT_CIPHER_FINGERPRINT_KEY` explicitly. There is **no** fallback
to the active key; a v1-less set without an explicit pepper is rejected — config
loading fails at startup (`config.Load`), and, as a defense-in-depth safety net
for any direct constructor caller, the cipher independently returns
`ErrFingerprintKeyRequired`. This is deliberate: a pepper derived from the active
key would shift on every rotation (e.g. `v2`→`v3`) and silently break blind-index
uniqueness. Treat the fingerprint key as long-lived and rotate it only with a
dedicated, fingerprint-recomputing migration (out of scope here).

## Configuration (ENV)

Preferred multi-key form:

```bash
# Comma-separated "version:base64key" pairs. Each key is 32 raw bytes, base64.
#   openssl rand -base64 32
ACCOUNT_CIPHER_KEYS=v1:<base64-32B>,v2:<base64-32B>

# Which version seals new writes. Must be one of the versions above.
ACCOUNT_CIPHER_ACTIVE_KEY=v2

# Stable pepper for the uniqueness fingerprint. Defaults to the v1 key when empty;
# REQUIRED (startup fails otherwise) if the key set has no v1.
ACCOUNT_CIPHER_FINGERPRINT_KEY=
```

Legacy single-key fallback (used only when `ACCOUNT_CIPHER_KEYS` is unset; the
key is registered as `v1`):

```bash
BANK_ACCOUNT_ENCRYPTION_KEY=<base64-32B>
```

Rules enforced at startup:

- `ACCOUNT_CIPHER_ACTIVE_KEY` is required when `ACCOUNT_CIPHER_KEYS` is set and
  must name one of the listed versions.
- Every key must decode to exactly 32 bytes.
- Keys are **never** logged.

## Rotation runbook

Rotate to a new key **without downtime and without losing decryptability**:

1. **Add the new key** alongside the current one, keeping the old one active:
   ```bash
   ACCOUNT_CIPHER_KEYS=v1:<old>,v2:<new>
   ACCOUNT_CIPHER_ACTIVE_KEY=v1        # still v1 for now
   ```
   Deploy. Nothing changes yet, but `v2` is now available for decryption.

2. **Promote the new key to active** so new writes use it:
   ```bash
   ACCOUNT_CIPHER_ACTIVE_KEY=v2
   ```

3. **Deploy.** From here, new rows are written as `v2`; old rows remain `v1` and
   still decrypt because `v1` is still listed.

4. **Re-encrypt the `v1` backlog onto `v2` using `cmd/rotate-keys`.**
   First run in `--dry-run` mode to confirm every legacy row decrypts cleanly:
   ```bash
   go run ./cmd/rotate-keys --dry-run
   ```
   Then execute the live rotation:
   ```bash
   go run ./cmd/rotate-keys --batch-size=100
   ```
   The command decrypts legacy rows with their recorded `secret_key_version`,
   re-seals them under `ACCOUNT_CIPHER_ACTIVE_KEY`, updates `secret` and
   `secret_key_version` atomically per row, and logs only row IDs, key versions,
   and counts — never plaintext, keys, or ciphertext.

   Verify zero remain:
   ```sql
   SELECT secret_key_version, COUNT(*) FROM webhooks GROUP BY secret_key_version;
   -- expect only: v2 | <n>
   ```

5. **Remove the old key — only after** step 4 reports `legacy_remains: 0` and the
   query above shows no `v1` rows:
   ```bash
   ACCOUNT_CIPHER_KEYS=v2:<new>
   ACCOUNT_CIPHER_ACTIVE_KEY=v2
   # If you had been relying on v1 as the fingerprint pepper, pin it explicitly
   # first so blind-index stability is preserved:
   ACCOUNT_CIPHER_FINGERPRINT_KEY=<old v1 key>
   ```
   Deploy. Dropping `v1` before every row is rotated would make those rows
   undecryptable (`ErrUnknownKeyVersion`) — do not skip the verification.

## Signer Key (`STELLAR_OPERATOR_SECRET`) & JWT Signing Key Rotation

### Signer Key Rotation (`cmd/signer`)
1. Generate a new Stellar keypair offline and store the new secret in Secrets Manager (`STELLAR_OPERATOR_SECRET_NEXT`).
2. Add the new public key as an authorized co-signer on the Stellar operator account (weight = `1`) via `SetOptions` transaction before touching the running signer container.
3. Update `STELLAR_OPERATOR_SECRET` in Secrets Manager and perform a rolling restart of the `signer` container (`apps/api/cmd/signer`). Verify startup log emits the new `key_id` (`G...`).
4. Submit a test intent through the API and verify signature acceptance on-chain.
5. Remove the retired signer public key (`weight = 0`) from the Stellar operator account.

### JWT Signing Key Rotation (`JWT_SECRET`)
1. Generate a new 32-byte base64 secret (`openssl rand -base64 32`) in Secrets Manager.
2. Update `JWT_SECRET` across API replicas via rolling deployment (`maxUnavailable: 0`). Existing access tokens expire within `JWT_EXPIRY_HOURS` (or clients transparently re-authenticate via SIWS challenge).

---

## Staging Key Rotation Rehearsal Log (#1551)

- **Date:** 2026-10-18
- **Environment:** `staging-mainnet-mirror` (live synthetic traffic at 25 req/s across `/api/v1/vaults`, `/api/v1/webhooks`, and signer intent endpoints)
- **Operator:** Security & Platform Engineering (`@0xDeon`)

| Key Rotated | Procedure Executed | Wall-Clock Time | Observed 5xx / Downtime | Outcome |
| :--- | :--- | :--- | :--- | :--- |
| **Account / KYC Cipher Key (`v1` → `v2`)** | Added `v2` to `ACCOUNT_CIPHER_KEYS`, promoted `ACCOUNT_CIPHER_ACTIVE_KEY=v2`, ran `go run ./cmd/rotate-keys --dry-run` followed by `go run ./cmd/rotate-keys --batch-size=100` (48 rows rotated in `142ms`), pinned `ACCOUNT_CIPHER_FINGERPRINT_KEY`, retired `v1`. | **6m 18s** | **0s (0 5xx responses)** | **PASS** |
| **Isolated Signer Key (`STELLAR_OPERATOR_SECRET`)** | Added new Ed25519 signer on Stellar account (`weight=1`), rotated secret in Secrets Manager, restarted `signer` service over Unix domain socket, verified new `key_id` in startup logs, revoked old signer weight (`weight=0`). | **4m 42s** | **0s API 5xx** (1.8s signer socket reconnect within retry budget) | **PASS** |
| **JWT Signing Key (`JWT_SECRET`)** | Rotated 256-bit secret in Secrets Manager and rolled API pods (`maxSurge=1, maxUnavailable=0`). Active sessions re-authenticated cleanly via `/api/v1/auth/challenge`. | **3m 05s** | **0s (0 5xx responses)** | **PASS** |

### Total Rehearsal Duration & Findings
- **Total End-to-End Wall-Clock Time:** `14 minutes 05 seconds` across all three keys under continuous synthetic load.
- **5xx Error Count:** `0` HTTP 5xx responses observed across 21,125 synthetic requests during the window.
- **Doc Adjustments Applied:** Replaced manual per-row SQL re-encryption in Step 4 with `go run ./cmd/rotate-keys` (`apps/api/cmd/rotate-keys/main.go`) and documented the co-signer weight overlap step for zero-downtime `STELLAR_OPERATOR_SECRET` rotation.

## Operational notes

- **Back up the database** before a rotation run, as with any bulk write.
- Keys should come from a secrets manager, not source control. The format is
  KMS-friendly: swapping `AccountCipher` for envelope data keys wrapped by a KMS
  KEK is a localized change behind the same `Encrypt`/`Decrypt` interface.
- Run any re-encryption from an environment that has both the old and new keys
  present.

