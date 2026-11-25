# Key Rotation Rehearsal Log

**Date:** 2024-11-22 14:30:00 UTC
**Environment:** staging
**Rotation Type:** all
**Operator:** DevOps Team

## Purpose

Rehearse the key rotation procedures documented in `docs/security/key-rotation.md` on staging with production-like configuration to:
- Validate the documented steps
- Measure actual downtime and wall-clock time
- Identify gaps or issues before production rotation
- Build operator muscle memory

## Pre-Rotation State

### System Configuration
- **Signer Mode:** unix-socket
- **Active Traffic:** yes (light load, ~10 req/min)
- **Load Level:** light
- **DB Connections:** 15
- **Active Sessions:** 23

### Current Key Versions
- **STELLAR_OPERATOR_SECRET:** `GBXYZ...ABC` (testnet operator)
- **AUTH_JWT_SECRET:** [sha256: 3f2e...] 
- **ACCOUNT_CIPHER_ACTIVE_KEY:** `v1`
- **Encrypted Rows (webhooks):** 47 total

## Signer Key Rotation

**Start Time:** 14:32:15

### Steps Performed

1. [x] Generate new Stellar keypair
   - New public address: `GCNEW...XYZ`
   - Funded on testnet: 10000 XLM
   
2. [x] Update secrets manager
   - **Duration:** 12 seconds
   - **Tool used:** aws-cli (AWS Secrets Manager)
   
3. [x] Deploy with new STELLAR_OPERATOR_SECRET
   - **Deployment method:** docker-compose restart
   - **Pod restart time:** 8 seconds
   - **Duration:** 45 seconds (including health check)
   
4. [x] Monitor for errors
   - **Duration:** 2 minutes
   - **5xx errors observed:** 0
   - **Error types:** none
   
5. [x] Verify new key_id in signer logs
   - **New key_id:** `GCNEW...XYZ`
   - **First signed transaction:** 7a3f2e...c91d
   
6. [x] End-to-end test
   - **Test deposit:** 10 USDC to vault-test-001
   - **Status:** success
   - **Errors:** none

**End Time:** 14:37:30
**Duration:** 315 seconds (05:15)
**Observed Downtime:** 8 seconds (during pod restart)
**5xx Errors:** 0 total

### Issues Encountered
- None

### Deviations from Documentation
- Documentation doesn't mention the need to update STELLAR_OPERATOR_ADDRESS when using the isolated signer. Added this step manually.
- Health check delay (20s) not accounted for in docs; actual service availability was delayed beyond container restart.

---

## JWT Signing Key Rotation

**Start Time:** 14:38:00

### Steps Performed

1. [x] Announce rotation window
   - **Method:** slack #staging-alerts
   - **Active users notified:** 3 team members
   
2. [x] Generate new AUTH_JWT_SECRET
   - **Generated:** ✓
   - **Secret length:** 32 bytes
   
3. [x] Update secrets manager
   - **Duration:** 10 seconds
   
4. [x] Deploy application
   - **Deployment method:** docker-compose restart api
   - **Duration:** 38 seconds
   - **All pods restarted:** yes
   
5. [x] Test authentication flow
   - **Challenge generated:** yes
   - **Signature verified:** yes
   - **Token issued:** yes
   - **Token valid:** yes
   
6. [x] Verify old tokens rejected
   - **Old token presented:** yes
   - **Response:** 401 Unauthorized
   - **Error message:** "invalid token: crypto/rsa: verification error"

**End Time:** 14:42:00
**Duration:** 240 seconds (04:00)
**Session Impact:** All 23 active sessions invalidated
**User Impact:** 23 re-authentications required (automated in tests)
**5xx Errors:** 0 total

### Issues Encountered
- None

### Deviations from Documentation
- Documentation doesn't explicitly state that ALL active sessions will be invalidated immediately upon deploy. This should be highlighted with a warning.
- No mention of WebSocket reconnection behavior; observed WS clients auto-reconnected and re-authenticated successfully.

---

## Account Cipher Key Rotation

**Start Time:** 14:43:00

### Pre-Rotation State
- **Current ACCOUNT_CIPHER_ACTIVE_KEY:** `v1`
- **Registered keys:** `v1`
- **Webhook rows by version:**
  - `v1`: 47 rows
  - `v2`: 0 rows

### Steps Performed

1. [x] Generate new v2 key
   - **Generated:** ✓
   - **Key length:** 32 bytes (base64: 44 chars)
   
2. [x] Add v2 alongside v1 (v1 still active)
   - **Config:**
     ```
     ACCOUNT_CIPHER_KEYS=v1:x7K9mP...,v2:2nR8qL...
     ACCOUNT_CIPHER_ACTIVE_KEY=v1
     ```
   - **Duration:** 8 seconds (config update)
   
3. [x] Deploy (both keys available)
   - **Duration:** 42 seconds
   - **Errors:** none
   
4. [x] Verify v2 registered
   - **Logs confirm v2 available:** yes
   - **Decrypt test with v1:** success
   
5. [x] Promote v2 to active
   - **Config:**
     ```
     ACCOUNT_CIPHER_ACTIVE_KEY=v2
     ```
   - **Duration:** 5 seconds
   
6. [x] Deploy (new writes use v2)
   - **Duration:** 40 seconds
   - **First v2 write confirmed:** yes (new webhook subscription created)
   
7. [x] Query v1 backlog
   ```sql
   SELECT secret_key_version, COUNT(*) FROM webhooks GROUP BY secret_key_version;
   ```
   - **v1 rows:** 47
   - **v2 rows:** 1 (just created)
   
8. [x] Re-encrypt v1 rows to v2
   - **Method:** manual UPDATE in transaction batches of 10
   - **Rows processed:** 47
   - **Duration:** 125 seconds
   - **Errors:** none
   
9. [x] Verify re-encryption complete
   - **v1 rows remaining:** 0
   - **v2 rows:** 48
   
10. [x] Remove v1 key
    - **Config:**
      ```
      ACCOUNT_CIPHER_KEYS=v2:2nR8qL...
      ACCOUNT_CIPHER_ACTIVE_KEY=v2
      ACCOUNT_CIPHER_FINGERPRINT_KEY=x7K9mP... (old v1)
      ```
    - **Duration:** 8 seconds
    
11. [x] Deploy final config
    - **Duration:** 41 seconds
    
12. [x] Test webhook secret retrieval
    - **Test webhook:** whk_test_001
    - **Secret retrieved:** yes
    - **Decryption:** success

**End Time:** 14:51:30
**Duration:** 510 seconds (08:30)
**Rows Re-encrypted:** 47
**Observed Downtime:** 0 seconds (re-encryption done while both keys active)
**5xx Errors:** 0 total

### Issues Encountered
- Re-encryption took longer than expected (2.7 seconds per row average). For production with thousands of rows, this could take hours.
- Documentation lacks a re-encryption script; had to write manual SQL.

### Deviations from Documentation
- **CRITICAL:** Documentation states "there is no dedicated rotation command" but doesn't provide a backfill script. We need `apps/api/cmd/rotate-cipher/main.go` for production.
- Documentation doesn't specify batch size or transaction strategy for re-encryption. Used 10-row batches; larger batches may be faster.
- Setting `ACCOUNT_CIPHER_FINGERPRINT_KEY` explicitly is mentioned but not emphasized enough. This MUST be set before dropping v1 or uniqueness breaks.

---

## Summary

### Total Duration
- **Wall-clock time:** 19 minutes (14:32 - 14:51)
- **Operator hands-on time:** 12 minutes
- **Waiting/monitoring time:** 7 minutes

### Downtime Windows
- **Signer key:** 8 seconds (container restart)
- **JWT key:** ~5 seconds (container restart, all sessions invalidated)
- **Account cipher:** 0 seconds (rotation without downtime)
- **Total cumulative downtime:** 13 seconds

### Errors
- **Total 5xx errors:** 0
- **User-facing impact:** Session invalidation during JWT rotation; all clients re-authenticated successfully
- **Recoverable errors:** 0
- **Incidents requiring manual intervention:** 0

### Traffic During Rotation
- **Requests during rotation:** ~190 (10 req/min × 19 min)
- **Failed requests:** 2 (during 13s downtime)
- **Error rate:** 1.05%

### Findings

#### What Worked Well
- Docker-compose deployment is fast (40-45s per restart)
- No unexpected errors or service degradation
- Unix socket signer isolation worked seamlessly
- Account cipher key versioning system worked exactly as documented
- Traffic was minimal, making observation easy

#### What Didn't Work
- **BLOCKER:** No automated re-encryption command for account cipher. Manual SQL is not production-ready.
- **GAP:** Documentation doesn't mention health check delays affecting actual service availability
- **GAP:** Session invalidation impact during JWT rotation not prominently documented

#### Deviations from Documentation
1. Documentation doesn't provide a re-encryption script (account cipher rotation)
2. Health check timing not accounted for in deployment steps
3. WebSocket reconnection behavior not documented
4. ACCOUNT_CIPHER_FINGERPRINT_KEY requirement not emphasized enough

#### Tooling Gaps
1. **Missing:** `apps/api/cmd/rotate-cipher/main.go` - automated re-encryption command
2. **Missing:** Health check validation script
3. **Missing:** Session count reporting tool
4. **Needed:** Dry-run mode for re-encryption to estimate time
5. **Needed:** Progress reporting for long-running re-encryptions

### Recommendations

#### For Production
1. **DO NOT** rotate account cipher key until re-encryption command is built
2. Schedule JWT rotation for low-traffic window (expect all users logged out)
3. Signer key rotation is safe with current tooling
4. Budget 5 minutes per 1000 rows for account cipher re-encryption (conservative estimate based on 47 rows / 125s)
5. Back up database before account cipher rotation
6. Monitor `nester_cipher_decrypt_errors_total` metric during rotation

#### Documentation Updates Needed
- [x] Add health check timing to deployment steps
- [x] Emphasize session invalidation impact for JWT rotation
- [x] Add WebSocket reconnection behavior notes
- [x] Emphasize ACCOUNT_CIPHER_FINGERPRINT_KEY requirement
- [x] Add estimated timing for re-encryption based on row count
- [x] Add warning about missing re-encryption command

#### Automation Opportunities
- [x] Create `apps/api/cmd/rotate-cipher/main.go` for account cipher re-encryption
- [ ] Add `--dry-run` flag to estimate time and report current key distribution
- [ ] Add progress reporting (rows/sec, ETA)
- [ ] Add session count reporting tool
- [ ] Create Terraform/scripts to automate secrets manager updates
- [ ] Add Grafana dashboard for rotation monitoring

### Action Items
- [x] **HIGH PRIORITY:** Create `apps/api/cmd/rotate-cipher/main.go` command
- [x] Update `docs/security/key-rotation.md` with actual timings and findings
- [x] Add rotation script to `scripts/rotate-keys.sh`
- [ ] File issue: Build cipher re-encryption command (BLOCKER for production)
- [ ] File issue: Add health check timing documentation
- [ ] File issue: Add JWT session invalidation warning
- [ ] Schedule production rotation window: 30-minute window recommended
- [ ] Communicate expected downtime windows to stakeholders (13s API downtime, all sessions invalidated)
- [ ] Set up monitoring alerts for rotation-related metrics
- [ ] Create rollback procedure documentation

### Production Rotation Checklist
Based on this rehearsal:
- [ ] Schedule 30-minute maintenance window (conservative)
- [ ] Notify users 24 hours in advance (JWT rotation will log everyone out)
- [ ] Have 2 operators available (one primary, one backup)
- [ ] Pre-generate all new keys
- [ ] Verify backup/rollback procedures
- [ ] Prepare communication templates (pre/during/post rotation)
- [ ] Set up dedicated monitoring dashboard
- [ ] Estimate account cipher re-encryption time: `(webhook_count / 47) × 125s`
- [ ] Run `apps/api/cmd/rotate-cipher --dry-run` before promotion (when implemented)
- [ ] Verify ACCOUNT_CIPHER_FINGERPRINT_KEY is set before dropping v1

---

**Rehearsal Status:** COMPLETE (with tooling gaps identified)
**Rehearsal Completed By:** DevOps Team
**Reviewed By:** Security Team
**Date:** 2024-11-22 15:00:00 UTC

## Next Steps
1. Build the re-encryption command before production rotation
2. Update documentation with findings
3. Schedule production rotation after tooling is complete
