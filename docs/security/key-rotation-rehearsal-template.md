# Key Rotation Rehearsal Log Template

**Date:** YYYY-MM-DD HH:MM:SS
**Environment:** staging
**Rotation Type:** [signer|jwt|account-cipher|all]
**Operator:** [your-name]

## Purpose

Rehearse the key rotation procedures documented in `docs/security/key-rotation.md` on staging with production-like configuration to:
- Validate the documented steps
- Measure actual downtime and wall-clock time
- Identify gaps or issues before production rotation
- Build operator muscle memory

## Pre-Rotation State

### System Configuration
- **Signer Mode:** [unix-socket|mtls]
- **Active Traffic:** [yes/no]
- **Load Level:** [light|moderate|heavy]
- **DB Connections:** _____
- **Active Sessions:** _____

### Current Key Versions
- **STELLAR_OPERATOR_SECRET:** `G...` (public address)
- **AUTH_JWT_SECRET:** [hash/identifier]
- **ACCOUNT_CIPHER_ACTIVE_KEY:** `v1|v2|...`
- **Encrypted Rows (webhooks):** _____ total

## Signer Key Rotation

**Start Time:** HH:MM:SS

### Steps Performed

1. [ ] Generate new Stellar keypair
   - New public address: `G...`
   - Funded on testnet: _____ XLM
   
2. [ ] Update secrets manager
   - **Duration:** _____ seconds
   - **Tool used:** [aws-cli|vault|other]
   
3. [ ] Deploy with new STELLAR_OPERATOR_SECRET
   - **Deployment method:** [kubectl|ecs|docker-compose]
   - **Pod restart time:** _____ seconds
   - **Duration:** _____ seconds
   
4. [ ] Monitor for errors
   - **Duration:** 2 minutes
   - **5xx errors observed:** _____
   - **Error types:** _____
   
5. [ ] Verify new key_id in signer logs
   - **New key_id:** _____
   - **First signed transaction:** [tx-hash]
   
6. [ ] End-to-end test
   - **Test deposit:** [amount] to [vault]
   - **Status:** [success|failed]
   - **Errors:** _____

**End Time:** HH:MM:SS
**Duration:** _____ seconds (MM:SS)
**Observed Downtime:** _____ seconds
**5xx Errors:** _____ total

### Issues Encountered
- _____

### Deviations from Documentation
- _____

---

## JWT Signing Key Rotation

**Start Time:** HH:MM:SS

### Steps Performed

1. [ ] Announce rotation window
   - **Method:** [slack|email|in-app]
   - **Active users notified:** _____
   
2. [ ] Generate new AUTH_JWT_SECRET
   - **Generated:** ✓
   - **Secret length:** 32 bytes
   
3. [ ] Update secrets manager
   - **Duration:** _____ seconds
   
4. [ ] Deploy application
   - **Deployment method:** _____
   - **Duration:** _____ seconds
   - **All pods restarted:** [yes/no]
   
5. [ ] Test authentication flow
   - **Challenge generated:** [yes/no]
   - **Signature verified:** [yes/no]
   - **Token issued:** [yes/no]
   - **Token valid:** [yes/no]
   
6. [ ] Verify old tokens rejected
   - **Old token presented:** [yes/no]
   - **Response:** [401 Unauthorized]
   - **Error message:** _____

**End Time:** HH:MM:SS
**Duration:** _____ seconds
**Session Impact:** All _____ active sessions invalidated
**User Impact:** _____ re-authentications required
**5xx Errors:** _____ total

### Issues Encountered
- _____

### Deviations from Documentation
- _____

---

## Account Cipher Key Rotation

**Start Time:** HH:MM:SS

### Pre-Rotation State
- **Current ACCOUNT_CIPHER_ACTIVE_KEY:** `v1`
- **Registered keys:** `v1`
- **Webhook rows by version:**
  - `v1`: _____ rows
  - `v2`: 0 rows

### Steps Performed

1. [ ] Generate new v2 key
   - **Generated:** ✓
   - **Key length:** 32 bytes
   
2. [ ] Add v2 alongside v1 (v1 still active)
   - **Config:**
     ```
     ACCOUNT_CIPHER_KEYS=v1:<old>,v2:<new>
     ACCOUNT_CIPHER_ACTIVE_KEY=v1
     ```
   - **Duration:** _____ seconds
   
3. [ ] Deploy (both keys available)
   - **Duration:** _____ seconds
   - **Errors:** _____
   
4. [ ] Verify v2 registered
   - **Logs confirm v2 available:** [yes/no]
   - **Decrypt test with v1:** [success|failed]
   
5. [ ] Promote v2 to active
   - **Config:**
     ```
     ACCOUNT_CIPHER_ACTIVE_KEY=v2
     ```
   - **Duration:** _____ seconds
   
6. [ ] Deploy (new writes use v2)
   - **Duration:** _____ seconds
   - **First v2 write confirmed:** [yes/no]
   
7. [ ] Query v1 backlog
   ```sql
   SELECT secret_key_version, COUNT(*) FROM webhooks GROUP BY secret_key_version;
   ```
   - **v1 rows:** _____
   - **v2 rows:** _____
   
8. [ ] Re-encrypt v1 rows to v2
   - **Method:** [manual UPDATE|backfill script]
   - **Rows processed:** _____
   - **Duration:** _____ seconds
   - **Errors:** _____
   
9. [ ] Verify re-encryption complete
   - **v1 rows remaining:** 0
   - **v2 rows:** _____
   
10. [ ] Remove v1 key
    - **Config:**
      ```
      ACCOUNT_CIPHER_KEYS=v2:<new>
      ACCOUNT_CIPHER_ACTIVE_KEY=v2
      ACCOUNT_CIPHER_FINGERPRINT_KEY=<old_v1_key>
      ```
    - **Duration:** _____ seconds
    
11. [ ] Deploy final config
    - **Duration:** _____ seconds
    
12. [ ] Test webhook secret retrieval
    - **Test webhook:** [webhook-id]
    - **Secret retrieved:** [yes/no]
    - **Decryption:** [success|failed]

**End Time:** HH:MM:SS
**Duration:** _____ seconds
**Rows Re-encrypted:** _____
**Observed Downtime:** _____ seconds
**5xx Errors:** _____ total

### Issues Encountered
- _____

### Deviations from Documentation
- _____

---

## Summary

### Total Duration
- **Wall-clock time:** _____ minutes
- **Operator hands-on time:** _____ minutes
- **Waiting/monitoring time:** _____ minutes

### Downtime Windows
- **Signer key:** _____ seconds
- **JWT key:** _____ seconds (all sessions invalidated)
- **Account cipher:** _____ seconds
- **Total cumulative downtime:** _____ seconds

### Errors
- **Total 5xx errors:** _____
- **User-facing impact:** _____
- **Recoverable errors:** _____
- **Incidents requiring manual intervention:** _____

### Traffic During Rotation
- **Requests during rotation:** _____
- **Failed requests:** _____
- **Error rate:** _____%

### Findings

#### What Worked Well
- _____

#### What Didn't Work
- _____

#### Deviations from Documentation
- _____

#### Tooling Gaps
- _____

### Recommendations

#### For Production
- _____

#### Documentation Updates Needed
- [ ] _____
- [ ] _____

#### Automation Opportunities
- [ ] _____
- [ ] _____

### Action Items
- [ ] Update `docs/security/key-rotation.md` with actual timings and findings
- [ ] File issues for identified tooling gaps
- [ ] Create re-encryption script for account cipher backlog
- [ ] Schedule production rotation window based on observed timings
- [ ] Communicate expected downtime windows to stakeholders
- [ ] Set up monitoring alerts for rotation-related metrics

### Production Rotation Checklist
Based on this rehearsal:
- [ ] Schedule _____ minute maintenance window
- [ ] Notify users _____ hours in advance
- [ ] Have _____ operators available
- [ ] Pre-generate all new keys
- [ ] Verify backup/rollback procedures
- [ ] Prepare communication templates
- [ ] Set up dedicated monitoring dashboard

---

**Rehearsal Status:** [COMPLETE|PARTIAL|FAILED]
**Rehearsal Completed By:** [operator-name]
**Reviewed By:** [reviewer-name]
**Date:** YYYY-MM-DD HH:MM:SS
