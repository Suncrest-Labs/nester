#!/usr/bin/env bash
# Key rotation script for staging rehearsal
# Usage: ./scripts/rotate-keys.sh [signer|jwt|account-cipher|all]
#
# This script assists with the key rotation procedure documented in
# docs/security/key-rotation.md. It does NOT automatically rotate keys
# (that would be dangerous), but provides a structured checklist and
# timing framework for manual rotation rehearsal.

set -euo pipefail

ROTATION_TYPE="${1:-all}"
LOG_FILE="docs/security/key-rotation-rehearsal-$(date +%Y%m%d-%H%M%S).md"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $*" | tee -a "$LOG_FILE"
}

warn() {
    echo -e "${YELLOW}[$(date +'%Y-%m-%d %H:%M:%S')] WARNING:${NC} $*" | tee -a "$LOG_FILE"
}

error() {
    echo -e "${RED}[$(date +'%Y-%m-%d %H:%M:%S')] ERROR:${NC} $*" | tee -a "$LOG_FILE"
}

prompt() {
    echo -e "${YELLOW}$*${NC}"
    read -p "Press ENTER when complete or Ctrl+C to abort: "
}

check_environment() {
    if [[ "${APP_ENV:-}" != "staging" ]]; then
        error "APP_ENV must be 'staging' for rehearsal"
        error "Current APP_ENV: ${APP_ENV:-not set}"
        exit 1
    fi
    log "Environment check: staging confirmed"
}

generate_key() {
    openssl rand -base64 32
}

init_log() {
    cat > "$LOG_FILE" <<EOF
# Key Rotation Rehearsal Log

**Date:** $(date +'%Y-%m-%d %H:%M:%S %Z')
**Environment:** staging
**Rotation Type:** $ROTATION_TYPE
**Operator:** ${USER:-unknown}

## Pre-Rotation State

EOF
    log "Rehearsal log initialized: $LOG_FILE"
}

rotate_signer_key() {
    local start_time=$(date +%s)
    
    log "=== SIGNER KEY ROTATION START ==="
    log "Rotating STELLAR_OPERATOR_SECRET"
    
    echo -e "\n## Signer Key Rotation\n" >> "$LOG_FILE"
    echo "**Start Time:** $(date +'%Y-%m-%d %H:%M:%S')" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
    
    prompt "Step 1: Generate new Stellar keypair and fund it on testnet"
    
    local new_public
    read -p "Enter new operator PUBLIC address (G...): " new_public
    log "New operator address: $new_public"
    echo "- New operator address: \`$new_public\`" >> "$LOG_FILE"
    
    prompt "Step 2: Update secrets manager with new STELLAR_OPERATOR_SECRET"
    
    prompt "Step 3: Deploy with NEW key but KEEP old key in STELLAR_OPERATOR_ADDRESS for transition"
    
    log "Monitoring for 5xx errors..."
    prompt "Step 4: Monitor logs and metrics for 5xx errors (check for 2 minutes)"
    
    prompt "Step 5: Verify signer is signing with new key (check signer logs for new key_id)"
    
    prompt "Step 6: Test a deposit to confirm end-to-end signing works"
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    log "Signer key rotation complete"
    log "Duration: ${duration}s ($(date -d@${duration} -u +%M:%S))"
    
    echo "" >> "$LOG_FILE"
    echo "**End Time:** $(date +'%Y-%m-%d %H:%M:%S')" >> "$LOG_FILE"
    echo "**Duration:** ${duration}s" >> "$LOG_FILE"
    echo "**Observed Downtime:** _____ seconds" >> "$LOG_FILE"
    echo "**5xx Errors:** _____ total" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
}

rotate_jwt_key() {
    local start_time=$(date +%s)
    
    log "=== JWT SIGNING KEY ROTATION START ==="
    log "Rotating AUTH_JWT_SECRET"
    
    echo -e "\n## JWT Signing Key Rotation\n" >> "$LOG_FILE"
    echo "**Start Time:** $(date +'%Y-%m-%d %H:%M:%S')" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
    
    warn "JWT rotation will INVALIDATE all active sessions"
    warn "Users will need to re-authenticate"
    
    prompt "Step 1: Announce rotation window to users (optional for staging)"
    
    log "Generating new JWT secret..."
    local new_jwt=$(generate_key)
    log "New JWT secret generated (first 8 chars): ${new_jwt:0:8}..."
    echo "- New JWT secret generated" >> "$LOG_FILE"
    
    prompt "Step 2: Update secrets manager with new AUTH_JWT_SECRET: ${new_jwt:0:8}..."
    
    prompt "Step 3: Deploy application"
    
    log "All sessions invalidated at deploy time"
    
    prompt "Step 4: Test authentication flow (generate challenge, verify, get token)"
    
    prompt "Step 5: Verify old tokens are rejected (should get 401)"
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    log "JWT key rotation complete"
    log "Duration: ${duration}s"
    
    echo "" >> "$LOG_FILE"
    echo "**End Time:** $(date +'%Y-%m-%d %H:%M:%S')" >> "$LOG_FILE"
    echo "**Duration:** ${duration}s" >> "$LOG_FILE"
    echo "**Session Impact:** All active sessions invalidated" >> "$LOG_FILE"
    echo "**5xx Errors:** _____ total" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
}

rotate_account_cipher() {
    local start_time=$(date +%s)
    
    log "=== ACCOUNT CIPHER KEY ROTATION START ==="
    log "Rotating ACCOUNT_CIPHER_KEYS (webhook secrets, bank accounts)"
    
    echo -e "\n## Account Cipher Key Rotation\n" >> "$LOG_FILE"
    echo "**Start Time:** $(date +'%Y-%m-%d %H:%M:%S')" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
    
    log "Current active version: ${ACCOUNT_CIPHER_ACTIVE_KEY:-v1}"
    echo "- Current active version: \`${ACCOUNT_CIPHER_ACTIVE_KEY:-v1}\`" >> "$LOG_FILE"
    
    log "Generating new key..."
    local new_key=$(generate_key)
    log "New v2 key generated (first 8 chars): ${new_key:0:8}..."
    
    prompt "Step 1: Add v2 key alongside v1, keeping v1 active
    ACCOUNT_CIPHER_KEYS=v1:<old_key>,v2:${new_key:0:8}...
    ACCOUNT_CIPHER_ACTIVE_KEY=v1"
    
    prompt "Step 2: Deploy with both keys. Nothing changes yet."
    
    log "Verifying v2 key is registered..."
    prompt "Step 3: Check logs confirm v2 key available for decryption"
    
    prompt "Step 4: Promote v2 to active
    ACCOUNT_CIPHER_ACTIVE_KEY=v2"
    
    prompt "Step 5: Deploy. New writes now use v2."
    
    log "Checking for v1 backlog to re-encrypt..."
    prompt "Step 6: Query database for rows still on v1:
    SELECT secret_key_version, COUNT(*) FROM webhooks GROUP BY secret_key_version;
    Record counts:"
    
    read -p "  v1 count: " v1_count
    read -p "  v2 count: " v2_count
    
    echo "- v1 rows: $v1_count" >> "$LOG_FILE"
    echo "- v2 rows: $v2_count" >> "$LOG_FILE"
    
    if [[ "$v1_count" -gt 0 ]]; then
        warn "Re-encryption needed for $v1_count rows"
        prompt "Step 7: Re-encrypt v1 rows to v2 (manual UPDATE or backfill script)"
        
        prompt "Step 8: Verify re-encryption complete (v1 count should be 0)"
        
        log "Final verification..."
        prompt "Step 9: Confirm zero v1 rows remain"
    else
        log "No v1 backlog found, skipping re-encryption"
    fi
    
    prompt "Step 10: Remove v1 key ONLY after all rows on v2
    ACCOUNT_CIPHER_KEYS=v2:${new_key:0:8}...
    ACCOUNT_CIPHER_ACTIVE_KEY=v2
    ACCOUNT_CIPHER_FINGERPRINT_KEY=<old_v1_key>"
    
    prompt "Step 11: Deploy final config"
    
    prompt "Step 12: Test webhook secret retrieval"
    
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))
    
    log "Account cipher key rotation complete"
    log "Duration: ${duration}s"
    
    echo "" >> "$LOG_FILE"
    echo "**End Time:** $(date +'%Y-%m-%d %H:%M:%S')" >> "$LOG_FILE"
    echo "**Duration:** ${duration}s" >> "$LOG_FILE"
    echo "**Rows Re-encrypted:** $v1_count" >> "$LOG_FILE"
    echo "**Observed Downtime:** _____ seconds" >> "$LOG_FILE"
    echo "**5xx Errors:** _____ total" >> "$LOG_FILE"
    echo "" >> "$LOG_FILE"
}

finalize_log() {
    cat >> "$LOG_FILE" <<EOF

## Summary

### Total Duration
- **Wall-clock time:** _____ minutes

### Downtime Windows
- **Signer key:** _____ seconds
- **JWT key:** _____ seconds (all sessions invalidated)
- **Account cipher:** _____ seconds

### Errors
- **Total 5xx errors:** _____
- **User-facing impact:** _____

### Deviations from Documentation
- _____

### Recommendations
- _____

### Action Items
- [ ] Update docs/security/key-rotation.md with actual timings
- [ ] Update runbook with any missing steps
- [ ] File issues for tooling improvements
- [ ] Schedule production rotation window

---
**Rehearsal Complete:** $(date +'%Y-%m-%d %H:%M:%S')
EOF
    
    log "Rehearsal log finalized: $LOG_FILE"
    log "Review the log, fill in blanks, and commit to repository"
}

main() {
    init_log
    check_environment
    
    log "Starting key rotation rehearsal: $ROTATION_TYPE"
    
    case "$ROTATION_TYPE" in
        signer)
            rotate_signer_key
            ;;
        jwt)
            rotate_jwt_key
            ;;
        account-cipher)
            rotate_account_cipher
            ;;
        all)
            rotate_signer_key
            rotate_jwt_key
            rotate_account_cipher
            ;;
        *)
            error "Unknown rotation type: $ROTATION_TYPE"
            error "Usage: $0 [signer|jwt|account-cipher|all]"
            exit 1
            ;;
    esac
    
    finalize_log
    
    echo ""
    log "========================================="
    log "Rehearsal complete!"
    log "Log saved to: $LOG_FILE"
    log "========================================="
    echo ""
    log "Next steps:"
    log "1. Review and complete the log file"
    log "2. Update docs/security/key-rotation.md with findings"
    log "3. Commit the rehearsal log"
    log "4. Schedule production rotation based on observed timings"
}

main
