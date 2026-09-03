# Data Classification Inventory

This document classifies every database column holding user data as **sensitive** (must be encrypted at rest) or **non-sensitive** (safe in plaintext). Classification drives the scope of envelope encryption in this project and is the authoritative reference for future schema changes.

> The KYC (`kyc_documents`) and offramp (`bank_accounts`, `settlements`) tables
> were removed with the fiat offramp (migrations 118–119); their historical
> classification lives in this file's git history. The envelope-encryption
> machinery below stays: webhook secrets still use it, and any future
> sensitive column should follow the same pattern.

## Classification Criteria

| Level | Label | Definition |
|-------|-------|------------|
| HIGH | Sensitive PII | Directly identifies an individual (government ID numbers, full legal name combined with identifiers). Exposure would enable identity theft or targeted harm. |
| MEDIUM | Indirect PII | User-chosen or pseudonymous identifiers that could be linked back to an individual with additional context. |
| LOW | Non-sensitive | Public identifiers, system metadata, enums, timestamps or user preferences. Exposure alone causes no material harm. |

---

## `users` table

Introduced in [migration 001](../migrations/001_initial_schema.up.sql). Core user identity and profile data.

| Column | Type | Sensitivity | Rationale |
|--------|------|-------------|-----------|
| `id` | UUID | LOW | Primary key |
| `wallet_address` | TEXT | LOW | Public on-chain identifier. Any user can derive it from a signed message. Not private by design. |
| `display_name` | TEXT | LOW | User-chosen pseudonym. May or may not correspond to a legal name. No guarantee of PII status. |
| `tier` | TEXT | LOW | Application-level user tier. |
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

## Summary

| Table | Sensitive Columns Encrypted | Non-sensitive / Not encrypted |
|-------|---------------------------|-------------------------------|
| `users` | — | All columns (wallet public, display name pseudonymous, rest metadata) |
| `webhooks` | endpoint secrets | metadata columns |

## Key Hierarchy

All encrypted fields use the same envelope-encryption hierarchy:

```
Master Key (never stored)
    └── Key-Encryption Keys (KEKs) — versioned, stored as ACCOUNT_CIPHER_KEYS
        └── Data Keys — per-encryption AES-256-GCM key (derived per cipher instance)
            └── Ciphertext — stored in `*_encrypted` columns alongside `key_version`
```

The fingerprint (blind index) uses a **separate key** from the encryption keys — either the `v1` KEK or the explicit `ACCOUNT_CIPHER_FINGERPRINT_KEY` — so that a fingerprint cannot be used to attack the ciphertext even if both are leaked in the same breach.
