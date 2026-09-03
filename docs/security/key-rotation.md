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

4. **Re-encrypt the `v1` backlog onto `v2`.** There is no dedicated rotation
   command; the backlog is small (one secret per webhook subscription), so
   re-encryption is a one-off, per-row migration: decrypt with `v1`, seal with
   `v2`, update the row and its `secret_key_version` in the same transaction.
   Log only counts and row IDs — never plaintext, keys, or ciphertext.

   Verify zero remain:
   ```sql
   SELECT secret_key_version, COUNT(*) FROM webhooks GROUP BY secret_key_version;
   -- expect only: v2 | <n>
   ```

5. **Remove the old key — only after** step 4 reports everything on `v2` and the
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

## Operational notes

- **Back up the database** before a rotation run, as with any bulk write.
- Keys should come from a secrets manager, not source control. The format is
  KMS-friendly: swapping `AccountCipher` for envelope data keys wrapped by a KMS
  KEK is a localized change behind the same `Encrypt`/`Decrypt` interface.
- Run any re-encryption from an environment that has both the old and new keys
  present.
