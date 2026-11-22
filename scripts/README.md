# Nester Scripts

Operational scripts for the Nester API.

## Key Rotation

### `rotate-keys.sh`

Interactive key rotation rehearsal script. Guides operators through the rotation
procedures documented in `docs/security/key-rotation.md` with timing and logging.

**Usage:**
```bash
# Rotate all keys (signer, JWT, account cipher)
./scripts/rotate-keys.sh all

# Rotate specific key type
./scripts/rotate-keys.sh signer
./scripts/rotate-keys.sh jwt
./scripts/rotate-keys.sh account-cipher
```

**Prerequisites:**
- `APP_ENV=staging` (safety check; script refuses to run outside staging)
- `openssl` for key generation
- Access to secrets manager
- Database access for account cipher rotation

**Output:**
- Generates a timestamped log file: `docs/security/key-rotation-rehearsal-YYYYMMDD-HHMMSS.md`
- Prompts for operator confirmation at each step
- Records start/end times for each rotation phase

**Safety:**
- Does NOT automatically apply changes; operator performs each step manually
- Serves as a structured checklist with timing framework
- Logs are committed to the repository for audit trail

See `docs/security/key-rotation-rehearsal-template.md` for the log structure.

## See Also

- `apps/api/cmd/rotate-cipher/` - Automated re-encryption tool for account cipher rotation
- `docs/security/key-rotation.md` - Full rotation procedures
- `docs/security/key-rotation-rehearsal-20241122-staging.md` - Example rehearsal log with timings
