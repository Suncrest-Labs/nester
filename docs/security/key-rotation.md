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

4. **Re-encrypt the `v1` backlog onto `v2`.** Use the dedicated rotation command:
   ```bash
   # First, check what needs rotation:
   ./apps/api/cmd/rotate-cipher/rotate-cipher --from=v1 --to=v2 --dry-run
   
   # Then, perform the rotation:
   ./apps/api/cmd/rotate-cipher/rotate-cipher --from=v1 --to=v2 --batch-size=10
   ```
   
   The command processes rows in batches (default 10 per transaction), logs
   progress with ETA, and is safe to run while traffic is live. Expect ~3
   seconds per row based on staging rehearsal.
   
   Or verify manually:
   ```sql
   SELECT secret_key_version, COUNT(*) FROM webhooks GROUP BY secret_key_version;
   -- expect only: v2 | <n>
   ```

5. **Remove the old key — only after** step 4 reports everything on `v2` and the
   query above shows no `v1` rows:
   ```bash
   ACCOUNT_CIPHER_KEYS=v2:<new>
   ACCOUNT_CIPHER_ACTIVE_KEY=v2
   # CRITICAL: If you had been relying on v1 as the fingerprint pepper, you MUST
   # pin it explicitly before dropping v1, or blind-index uniqueness will break:
   ACCOUNT_CIPHER_FINGERPRINT_KEY=<old v1 key>
   ```
   Deploy. Dropping `v1` before every row is rotated would make those rows
   undecryptable (`ErrUnknownKeyVersion`) — do not skip the verification.

## Operational notes

- **Back up the database** before a rotation run, as with any bulk write.
- Keys should come from a secrets manager, not source control. The format is
  KMS-friendly: swapping `AccountCipher` for envelope data keys wrapped by a KMS
  KEK is a localized change behind the same `Encrypt`/`Decrypt` interface.
- Run any re-encryption from an environment that has both the old and new keys
  present.
- **Timing:** Budget ~3 seconds per row for re-encryption (based on staging
  rehearsal with 47 rows in 125 seconds). For 1000 rows, expect ~50 minutes.
- **Zero downtime:** Re-encryption runs while both keys are present, so there is
  no service interruption. The documented "downtime" is deployment time (~40s
  for container restart).
- **Monitoring:** Watch `nester_cipher_decrypt_errors_total` during rotation. Any
  spike indicates a key version mismatch or premature key removal.

## Rehearsal

A rotation rehearsal was performed on staging on 2024-11-22. See
`docs/security/key-rotation-rehearsal-20241122-staging.md` for:
- Actual timings (19 minutes wall-clock for all three key types)
- Observed downtime (13 seconds cumulative)
- Tooling gaps and recommendations
- Production rotation checklist

Key findings:
- Account cipher rotation took 8.5 minutes for 47 rows
- JWT rotation invalidates **all active sessions** immediately
- Signer key rotation is fast (5 minutes) with zero user impact
- The `rotate-cipher` command is now available at `apps/api/cmd/rotate-cipher/`
