# Data Classification Inventory

This document classifies every database column holding user data as **sensitive** (must be encrypted at rest) or **non-sensitive** (safe in plaintext). Classification drives the scope of envelope encryption in this project and is the authoritative reference for future schema changes.

## Classification Criteria

| Level | Label | Definition |
|-------|-------|------------|
| HIGH | Sensitive PII | Directly identifies an individual (government ID numbers, full legal name combined with identifiers). Exposure would enable identity theft or targeted harm. |
| MEDIUM | Indirect PII | User-chosen or pseudonymous identifiers that could be linked back to an individual with additional context. |
| LOW | Non-sensitive | Public identifiers, system metadata, enums, timestamps or user preferences. Exposure alone causes no material harm. |

---

## `kyc_documents` table

Introduced in [migration 032](../migrations/032_kyc_workflow.up.sql). Stores uploaded identity documents submitted during KYC verification.

| Column | Type | Sensitivity | Rationale |
|--------|------|-------------|-----------|
| `id` | UUID | LOW | Primary key, not identifying |
| `user_id` | UUID | LOW | Foreign key to `users` |
| `id_type` | TEXT | LOW | Document type (e.g. "passport", "drivers_license") — not identifying alone |
| `id_number` | TEXT | **HIGH** | Government-issued ID number. Direct PII that uniquely identifies an individual. **Must be encrypted.** |
| `front_object_key` | TEXT | **HIGH** | S3 key for the front scan of the ID document. The object itself contains PII (full name, photo, ID number, date of birth). The key structure can reveal the mapping between user and document. **Must be encrypted.** |
| `back_object_key` | TEXT | **HIGH** | S3 key for the back scan of the ID document. Same reasoning as `front_object_key`. **Must be encrypted.** |
| `submitted_at` | TIMESTAMPTZ | LOW | Submission timestamp, not PII |
| `id_number_encrypted` | BYTEA | — | AES-256-GCM ciphertext of `id_number`. Added by migration 069. The column storing the encrypted value is itself opaque. |
| `id_number_fingerprint` | TEXT | — | HMAC-SHA256 blind index for exact-match deduplication of ID numbers. Stores a keyed hash, not the plaintext. Uniqueness is enforced via a unique constraint. |
| `front_object_key_encrypted` | BYTEA | — | AES-256-GCM ciphertext of `front_object_key`. Added by migration 069. |
| `back_object_key_encrypted` | BYTEA | — | AES-256-GCM ciphertext of `back_object_key`. Added by migration 069. |
| `key_version` | VARCHAR(32) | — | Records which encryption key version sealed the ciphertext columns. Enables non-destructive key rotation. |

### Blind Indexes

| Column | Source Field | Algorithm | Purpose | Limitation |
|--------|-------------|-----------|---------|------------|
| `id_number_fingerprint` | `id_number` (normalized) | HMAC-SHA256 keyed with the cipher's fingerprint key (same key used by account cipher) | Exact-match uniqueness and lookup without decryption | Exact-match only; no prefix, range or fuzzy search. Keyed HMAC prevents the fingerprint from being reversed or used to attack the encryption key. |

---

## `users` table

Introduced in [migration 001](../migrations/001_initial_schema.up.sql). Core user identity and profile data.

| Column | Type | Sensitivity | Rationale |
|--------|------|-------------|-----------|
| `id` | UUID | LOW | Primary key |
| `wallet_address` | TEXT | LOW | Public on-chain identifier. Any user can derive it from a signed message. Not private by design. |
| `display_name` | TEXT | LOW | User-chosen pseudonym. May or may not correspond to a legal name. No guarantee of PII status. |
| `kyc_status` | TEXT | LOW | Enum: `unverified` / `pending` / `verified` / `rejected`. Not identifying. |
| `tier` | TEXT | LOW | Application-level user tier. |
| `kyc_submitted_at` | TIMESTAMPTZ | LOW | Timestamp, not identifying. |
| `kyc_reviewed_at` | TIMESTAMPTZ | LOW | Timestamp, not identifying. |
| `kyc_rejection_reason` | TEXT | LOW | Internal admin-facing note. Not PII. |
| `risk_profile` | TEXT | LOW | User's risk preference: `conservative` / `moderate` / `aggressive`. Not PII. |
| `savings_goal` | TEXT | LOW | Free-text savings goal; user-supplied and not inherently identifying. |
| `onboarding_completed` | BOOLEAN | LOW | Onboarding flag. |
| `last_login_at` | TIMESTAMPTZ | LOW | Login timestamp. |
| `created_at` / `updated_at` | TIMESTAMPTZ | LOW | System timestamps. |

### Fields explicitly NOT encrypted (with reason)

- **`wallet_address`**: Public by design; used as a primary lookup key across the system. Encrypting it would break every wallet-based query without meaningful security gain.
- **`display_name`**: User-chosen pseudonym; not guaranteed to be real PII. Encrypting would add complexity with no security benefit.
- **All remaining fields**: Either system metadata, enums, timestamps, or user preferences with no PII value.

---

## `bank_accounts` table

Already encrypted per issue #799 (PR #799). Documented here for completeness.

| Column | Sensitivity | Status |
|--------|-------------|--------|
| `account_number` (plaintext) | HIGH | Encrypted via `account_cipher.go` |
| `account_number_encrypted` | — | AES-256-GCM ciphertext |
| `account_number_fingerprint` | — | HMAC-SHA256 blind index for uniqueness |
| `account_last4` | LOW | Last 4 digits for display; not encrypted |
| `key_version` | — | Key version used for encryption |

---

## Summary

| Table | Sensitive Columns Encrypted | Non-sensitive / Not encrypted |
|-------|---------------------------|-------------------------------|
| `kyc_documents` | `id_number`, `front_object_key`, `back_object_key` | `id`, `user_id`, `id_type`, `submitted_at` |
| `users` | — | All columns (wallet public, display name pseudonymous, rest metadata) |
| `bank_accounts` | `account_number` | `account_last4`, metadata columns |

## Key Hierarchy

All encrypted fields use the same envelope-encryption hierarchy established for bank accounts:

```
Master Key (never stored)
    └── Key-Encryption Keys (KEKs) — versioned, stored as ACCOUNT_CIPHER_KEYS
        └── Data Keys — per-encryption AES-256-GCM key (derived per cipher instance)
            └── Ciphertext — stored in `*_encrypted` columns alongside `key_version`
```

The fingerprint (blind index) uses a **separate key** from the encryption keys — either the `v1` KEK or the explicit `ACCOUNT_CIPHER_FINGERPRINT_KEY` — so that the fingerprint cannot be used to attack the ciphertext even if both are leaked in the same breach.

## Rotation

Key rotation rewraps ciphertext for every encrypted field across all tables in a single pass. See `cmd/rotate_keys/main.go` and `internal/rotation/rotation.go`.
