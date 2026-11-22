# Account Cipher Key Rotation Tool

This command re-encrypts account cipher ciphertext from one key version to
another, supporting the rotation procedure documented in
`docs/security/key-rotation.md`.

## Usage

```bash
# Dry run: see what would be rotated without making changes
go run ./apps/api/cmd/rotate-cipher --from=v1 --to=v2 --dry-run

# Perform the rotation
go run ./apps/api/cmd/rotate-cipher --from=v1 --to=v2 --batch-size=10
```

## Flags

- `--from=VERSION` (required): Source key version to decrypt with (e.g., `v1`)
- `--to=VERSION` (required): Target key version to encrypt with (e.g., `v2`)
- `--dry-run`: Report what would be re-encrypted without making changes
- `--batch-size=N`: Number of rows to process per transaction (default: 10)

## Prerequisites

1. Both `--from` and `--to` keys must be present in `ACCOUNT_CIPHER_KEYS`
2. The `--to` version must be the active key (`ACCOUNT_CIPHER_ACTIVE_KEY`)
3. Database must be accessible via `DATABASE_DSN`

## Safety

- **Transactional:** Each batch is processed in a single transaction
- **Idempotent:** Safe to run multiple times; skips already-rotated rows
- **No downtime:** Safe to run while traffic is live
- **Verified:** Each row is decrypted with the old key and re-encrypted with
  the new key; version is verified before commit
- **Concurrent-safe:** Uses WHERE conditions to detect concurrent modifications

## Performance

Based on staging rehearsal (2024-11-22):
- **~3 seconds per row** for webhook secrets
- 47 rows rotated in 125 seconds
- Progress logged with ETA every batch

For production estimation:
```
Estimated time = (row_count / batch_size) × (batch_size × 3s)
                ≈ row_count × 3s
```

Example: 1000 webhook secrets ≈ 50 minutes

## Output

Dry run:
```
[2024-11-22T14:43:00Z] INFO DRY RUN: counting rows to re-encrypt
[2024-11-22T14:43:00Z] INFO dry run complete webhooks_to_rotate=47 total_rows=47
[2024-11-22T14:43:00Z] INFO estimated time seconds=141 minutes=2 duration=2m21s
```

Actual rotation:
```
[2024-11-22T14:45:00Z] INFO rotating webhook secrets total_rows=47 batch_size=10
[2024-11-22T14:45:12Z] INFO batch complete processed=10 total=47 percent=21.3% rate_per_sec=0.83 elapsed=12s eta=45s
[2024-11-22T14:45:24Z] INFO batch complete processed=20 total=47 percent=42.6% rate_per_sec=0.83 elapsed=24s eta=33s
...
[2024-11-22T14:47:05Z] INFO webhook rotation complete rows_rotated=47 duration=2m5s rate_per_sec=0.38
```

## Error Handling

The command will fail with a descriptive error if:
- Either key version is not found in `ACCOUNT_CIPHER_KEYS`
- A row cannot be decrypted (wrong key version, corrupted ciphertext)
- A row cannot be re-encrypted
- Database transaction fails
- A row was modified concurrently (detected via WHERE clause)

All errors abort the current batch transaction, leaving the database unchanged.

## Monitoring

Watch these metrics during rotation:
- `nester_cipher_decrypt_errors_total`: Should stay at 0
- `nester_cipher_encrypt_errors_total`: Should stay at 0

If either spikes, investigate immediately (likely key version mismatch or
premature key removal).

## Rollback

If rotation fails mid-way:
1. Fix the issue (e.g., add missing key back to ACCOUNT_CIPHER_KEYS)
2. Re-run the command; it will skip already-rotated rows
3. Or, manually revert partially-rotated rows (not recommended)

## Post-Rotation Verification

```sql
-- All rows should be on the new version
SELECT secret_key_version, COUNT(*) FROM webhooks GROUP BY secret_key_version;
-- Expected: v2 | <total_count>
--           (no v1 rows)

-- If any v1 rows remain, re-run the rotation command
```

## See Also

- `docs/security/key-rotation.md` - Full rotation runbook
- `docs/security/key-rotation-rehearsal-20241122-staging.md` - Rehearsal log with timings
- `scripts/rotate-keys.sh` - Interactive rotation script for operators
