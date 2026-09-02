# Incident Response: Signing Key Compromise

**Audience: the on-call engineer, at 3am, who did not write this code.**

Commands are literal. Where a value must be substituted it is written in
`ANGLE_BRACKETS`. Where a step depends on infrastructure this repository does
not control, that is stated rather than glossed over.

Companion documents: [`signing-isolation.md`](./signing-isolation.md)
(architecture), [`signing-threat-model.md`](./signing-threat-model.md)
(blast radius per credential), [`key-rotation.md`](./key-rotation.md)
(account cipher rotation).

---

## 0. If you read nothing else

**Stop signing first. Investigate second.** Containment is one command and is
safe to run on suspicion — a false alarm costs you paused vault operations, a
delayed true alarm costs funds.

```bash
docker compose exec signer touch /var/run/nester-signer/signing.disabled
```

Confirm it took effect:

```bash
docker compose exec signer cat /var/run/nester-signer/signing.disabled
curl --unix-socket /var/run/nester-signer/signer.sock http://signer/v1/health
# expect: "kill_switch":"disabled"
```

Then work through §B onward.

---

## A. Detection

### A.1 What compromise looks like

| Signal | Where | What it suggests |
|---|---|---|
| Burst of `rejection=contract_not_allowed` | Signer log stream, `stream=signing_audit` | Someone is probing the policy with contracts the application never calls |
| Burst of `rejection=amount_out_of_policy` | Same | Attempted large-value extraction |
| Any `rejection=unknown_operation` | Same | The API is requesting a function it never legitimately requests |
| `outcome=unauthorized` | Same | Something reached the signer socket that should not have |
| Signing volume far above baseline | Signer counters | Automated abuse of policy-permitted operations |
| `emergency_withdraw_all` outside a planned incident | Signer audit, `operation` field | High-signal: this is rare in normal operation |
| On-chain activity with no matching audit event | Compare chain against audit | **The key is being used outside the signer.** Treat as confirmed compromise |

### A.2 Query the signing audit stream

The signer writes structured JSON to stdout with `stream=signing_audit`.

```bash
# Everything the signer has done in the last hour
docker compose logs signer --since 1h | grep signing_audit

# Rejections only — the highest-value signal
docker compose logs signer --since 1h | grep signing_audit | grep -v '"outcome":"signed"'

# Count by rejection category
docker compose logs signer --since 24h | grep signing_audit \
  | grep -o '"rejection":"[^"]*"' | sort | uniq -c | sort -rn

# Signing volume per hour, to compare against baseline
docker compose logs signer --since 24h | grep '"outcome":"signed"' \
  | grep -o '"occurred_at":"[^:]*' | sort | uniq -c
```

### A.3 Current detection latency — stated honestly

**These signals are recorded synchronously and are not currently alerted.**
There is no paging pipeline wired to them in this repository. Detection latency
is therefore bounded by how often somebody looks, not by how quickly the data
appears.

What that means in practice:

| Compromise | Detectable? | Realistic latency today |
|---|---|---|
| Signer abuse via the API | Yes — audit events and counters | Whenever the stream is reviewed |
| Key used outside the signer | Yes — chain activity with no audit event | Only via deliberate reconciliation |
| `AUTH_JWT_SECRET` forgery | **No** — a forged token is cryptographically identical to a real one | Unbounded |
| Cipher key exfiltration | **No** — offline decryption produces no application event | Unbounded |

Wiring A.1 into alerting is the highest-value follow-up to this work.

---

## B. Immediate containment

### B.1 Activate the kill switch

```bash
docker compose exec signer touch /var/run/nester-signer/signing.disabled
```

If the signer container is unreachable, write the sentinel directly to the host
path backing the volume — the switch is a file, and it does not depend on the
signer being healthy:

```bash
touch /var/lib/nester/signer-run/signing.disabled
```

**Authority required:** shell access to the signer container or its host, or the
orchestrator permission to exec into it. This is deliberately *not* an
application credential — the ability to halt signing must not be obtainable by
compromising the application you are halting.

**Verify** (do not skip — an unverified kill switch is not containment):

```bash
curl --unix-socket /var/run/nester-signer/signer.sock http://signer/v1/health
```

Expect `"kill_switch":"disabled"`. If you see `"unknown"`, the signer cannot
read the sentinel — signing is still blocked (it fails closed), but investigate
the volume mount.

Prove it end to end by attempting a real operation through the API; it must fail
with a signing-disabled error, and the signer must log
`outcome=disabled, rejection=kill_switch_active`.

### B.2 Stop background workers

Scheduled work (rebalancer, harvest engine, recurring deposits) will otherwise
keep generating signing requests, filling the audit stream with noise and
obscuring the attacker's activity.

```bash
docker compose exec api sh -c 'export REBALANCER_ENABLED=false HARVEST_ENGINE_ENABLED=false'
# The reliable form is to restart the API with these unset in its environment:
docker compose stop api
```

Stopping the API entirely is acceptable and often correct. The kill switch has
already removed the signing capability, so this is about noise reduction and
limiting other API-reachable damage.

### B.3 Preserve evidence — before restarting anything

**Do this before any restart.** Container logs do not survive a recreate.

```bash
mkdir -p /tmp/nester-incident-$(date +%Y%m%d-%H%M)
cd /tmp/nester-incident-*

docker compose logs signer --no-color > signer.log
docker compose logs api --no-color > api.log
docker compose ps -a > containers.txt
docker compose exec -T postgres pg_dump -U nester --table=audit_logs nester > audit_logs.sql
cp /var/lib/nester/signer-run/signing.disabled killswitch-sentinel.txt 2>/dev/null || true
```

Record, in your own notes: when you were paged, what you saw, when the kill
switch went in, and who you have told.

### B.4 Do not rotate yet

Rotation destroys the ability to correlate "which key signed this" while you are
still establishing blast radius. Containment (B.1) has already removed the
capability. Rotate in §D, after §C.

---

## C. Blast radius assessment

Answer these before rotating. The audit stream and the chain are the two
sources; the interesting cases are where they disagree.

### C.1 Which key, and which version

```bash
docker compose logs signer | grep signing_audit | grep -o '"key_id":"[^"]*"' | sort -u
```

`key_id` is the operator's public Stellar address. If more than one appears, a
key change happened during the window — establish when and why.

### C.2 What was signed, and when

```bash
# Every successful signature, oldest first
docker compose logs signer --since 48h | grep '"outcome":"signed"' \
  | sed 's/.*"occurred_at":"\([^"]*\)".*"operation":"\([^"]*\)".*"contract_address":"\([^"]*\)".*/\1 \2 \3/' \
  | sort
```

For each, record: the `intent_id`, the `operation`, the `contract_address`, the
`intent_hash`, and the `occurred_at`. The `intent_hash` is what lets you prove
later which request produced a given signature.

### C.3 The critical reconciliation

**Compare on-chain operator activity against the audit stream.**

Fetch the operator account's recent transactions from Horizon:

```bash
curl -s "$STELLAR_HORIZON_URL/accounts/OPERATOR_PUBLIC_ADDRESS/transactions?order=desc&limit=200" \
  | jq -r '._embedded.records[] | "\(.created_at) \(.hash)"'
```

Then:

- **Chain activity with a matching audit event** — signed through the signer.
  Expected; assess whether the *request* was legitimate.
- **Chain activity with no matching audit event** — **the key was used outside
  the signer.** This means key exfiltration, not merely API compromise. Escalate
  immediately and treat §D as urgent rather than sequenced.
- **Audit events with no chain activity** — signed but not submitted, or
  submission failed. Note the `intent_hash` values; a signed-but-unsubmitted
  envelope may still be broadcast by whoever holds it, until its timebound
  expires (5 minutes from signing).

### C.4 Affected scope

From the operations identified:

| Operation seen | Assess |
|---|---|
| `deposit` / `withdraw` | Which vault, what amount, whose funds moved |
| `emergency_withdraw_all` | Which vault fully exited; all holders in that vault are affected |
| `set_weights` | Allocation manipulation; check whether weights still match intent |
| `pause` / `unpause` | Availability impact; users unable to transact |
| `rebalance` | Possible value extraction through repeated forced rebalancing |
| `harvest` | Which user address was named in the `address` field |

Identify affected users by mapping contract addresses to vaults and vaults to
positions. Record the list — §F depends on it.

### C.5 Replay assessment

Check for repeated `intent_hash` values, and for `rejection=intent_replayed`
events (the guard catching an attempt):

```bash
docker compose logs signer --since 48h | grep signing_audit \
  | grep -o '"intent_hash":"[^"]*"' | sort | uniq -d
```

Duplicates in *signed* events would indicate a replay that succeeded — expected
to be empty, because the guard refuses them. `intent_replayed` rejections
indicate attempts.

---

## D. Key rotation

**Authority required:** whoever holds the operator key material and the ability
to submit a `set_options` / authority-transfer transaction for the vault
contracts. This is a deliberately small group; if you are not one of them,
escalate rather than improvise.

### D.1 Emergency operator key rotation

The kill switch has already stopped signing, so you are working without time
pressure on this step.

1. **Generate a new keypair** on a trusted machine, not on the compromised host:

   ```bash
   # Any Stellar SDK; the value must never be written to disk unencrypted
   # or pasted into a chat client.
   ```

2. **Fund the new account** so it can pay transaction fees.

3. **Transfer operator authority on-chain** to the new address, for every vault
   and strategy contract in `SIGNER_ALLOWED_CONTRACTS`. **This step is
   contract-specific and is not scripted in this repository** — consult the
   contract's admin interface. Until it completes, the old key remains the
   authorized operator, which is why containment must stay in place.

4. **Verify the transfer** by reading the operator address back from each
   contract before proceeding.

5. **Update the signer environment** with the new `STELLAR_OPERATOR_SECRET`, and
   the API with the new operator *public* address.

6. **Restart the signer**, leaving the kill switch engaged:

   ```bash
   docker compose up -d --force-recreate signer
   curl --unix-socket /var/run/nester-signer/signer.sock http://signer/v1/health
   # confirm the new key_id, and kill_switch still "disabled"
   ```

7. **Retire the old key.** Once authority has moved, the old key can no longer
   act as operator. Remove it from every environment, secret store, and backup.

### D.2 Related credential rotation

Rotate these if the API host was compromised, since they shared its environment:

| Credential | Effect of rotating |
|---|---|
| `AUTH_JWT_SECRET` | **Logs out every user.** No per-token revocation exists. Accept it — a forged token is undetectable, so rotation is the only remedy |
| `DATABASE_DSN` | Requires an API restart |
| `INTELLIGENCE_SERVICE_API_KEY`, `NESTER_SERVICE_API_KEY` | Update on both sides together |
| `ACCOUNT_CIPHER_KEYS` | See D.3 — different procedure, do not simply replace |

### D.3 Account cipher rotation

**Never delete an old key version before every record has migrated off it.**
Doing so renders those records permanently undecryptable, with no remedy short
of restoring the key.

Add the new version alongside the existing ones and make it active:

```bash
ACCOUNT_CIPHER_KEYS="v1:OLD_KEY_B64,v2:NEW_KEY_B64"
ACCOUNT_CIPHER_ACTIVE_KEY="v2"
# ACCOUNT_CIPHER_FINGERPRINT_KEY must NOT change — it keeps blind-index
# uniqueness working across the rotation.
```

Run the rotation, then confirm it drained before retiring anything:

```bash
docker compose run --rm api /app/rotate_keys
```

The rotator refuses to report success while rows remain pending, and
`VerifyRetirable` refuses to clear a version still in use. Both are there so that
this step cannot be completed prematurely.

**Rotation protects future reads. It does not undo an exfiltration that has
already happened** — data copied under the old key stays readable to whoever
copied it. If cipher keys leaked, this is a data breach and §F applies.

---

## E. Recovery

Work through in order. Do not skip the verification steps.

### E.1 Pre-flight

- [ ] Operator authority has moved to the new key, verified on-chain
- [ ] The old key is removed from every environment and secret store
- [ ] Signer restarted and reporting the new `key_id`
- [ ] `SIGNER_ALLOWED_CONTRACTS` still lists exactly the intended contracts
- [ ] `SIGNER_MAX_AMOUNT_STROOPS` is set and correct
- [ ] The compromise vector is understood and closed — **if you cannot say how
      they got in, do not re-enable signing**

### E.2 Verify audit integrity

```sql
-- Against the application database
SELECT * FROM audit_logs ORDER BY sequence DESC LIMIT 20;
```

Run chain verification through the audit service (`VerifyChain`). If it reports
a break, **the audit log was tampered with** — preserve the database, escalate,
and compare against the external anchor log
(`AnchorConfig.FilePath`, default `audit_anchors.log`).

Note precisely what this proves: the chain detects modification, deletion, and
reordering. An attacker with database write access who *recomputed* the chain
forward produces something internally consistent — only the external anchors
catch that. Check them.

### E.3 Restore signing

```bash
docker compose exec signer rm /var/run/nester-signer/signing.disabled
curl --unix-socket /var/run/nester-signer/signer.sock http://signer/v1/health
# expect "kill_switch":"enabled"
```

### E.4 Verify with a bounded operation

Do not declare recovery on a health check alone. Exercise one low-value,
reversible operation — a `rebalance` on a test vault, or a minimal `deposit` —
and confirm:

- The signer logs `outcome=signed` with the **new** `key_id`
- The transaction appears on-chain
- Latency is within the documented budget

### E.5 Restore workers

```bash
docker compose up -d api
```

Re-enable `REBALANCER_ENABLED` and `HARVEST_ENGINE_ENABLED`. Watch the signing
audit stream for the first few cycles.

### E.6 Post-recovery watch

For 24 hours: signing volume against baseline, rejection counts, and any
authorization failures.

---

## F. Communication

### F.1 Internal

| When | Who | What |
|---|---|---|
| Immediately on suspicion | Engineering on-call, security lead | "Possible signing key compromise, kill switch engaged, investigating" |
| Within 1 hour | Engineering lead, product lead | Blast radius from §C: which vaults, which users, what moved |
| Before rotating | Whoever holds operator key authority | Rotation is not unilateral |
| On recovery | All of the above | What happened, what was done, what remains open |

Escalate immediately, without waiting for the hourly update, if §C.3 shows
**chain activity with no matching audit event** — that is key exfiltration, not
API compromise, and it changes the response.

### F.2 External

Disclose to affected users when their funds, positions, or personal data were
affected. Legal and regulatory obligations vary by jurisdiction and are **not**
determined by this document — involve whoever owns that call.

**Do share:** that an incident occurred, which functionality was affected, what
you have done, what users should do, and when they will hear more.

**Do not share, while the incident is open:** the specific vulnerability or
exploitation path, key material or identifiers beyond what is already public
on-chain, internal hostnames or infrastructure detail, or speculation about
attribution.

### F.3 Timeline expectations

| Point | Target |
|---|---|
| Containment (kill switch) | Minutes from detection |
| Internal notification | Within 1 hour |
| Blast radius established | Within 4 hours |
| Affected-user notification | Within 24 hours of confirming impact |
| Post-incident review | Within 1 week |

---

## G. Post-incident

- [ ] Write the timeline: detection → containment → assessment → rotation →
      recovery, with real timestamps
- [ ] Record what the runbook got wrong or left out, and fix it here
- [ ] Close the vector that allowed the compromise
- [ ] Re-run the game day (`docs/security/game-day.md`) against the updated
      runbook
- [ ] If detection was slow, wire the §A.1 signals into alerting

---

## Appendix: quick reference

```bash
# HALT SIGNING
docker compose exec signer touch /var/run/nester-signer/signing.disabled

# CHECK STATE
curl --unix-socket /var/run/nester-signer/signer.sock http://signer/v1/health

# RECENT SIGNING ACTIVITY
docker compose logs signer --since 1h | grep signing_audit

# REJECTIONS ONLY (compromise signal)
docker compose logs signer --since 1h | grep signing_audit | grep -v '"outcome":"signed"'

# ON-CHAIN OPERATOR ACTIVITY (reconcile against the above)
curl -s "$STELLAR_HORIZON_URL/accounts/OPERATOR_ADDRESS/transactions?order=desc&limit=200"

# RESTORE SIGNING (only after §E.1)
docker compose exec signer rm /var/run/nester-signer/signing.disabled
```
