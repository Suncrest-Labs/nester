# Runbook — Vault Asset Composition Divergence

**Alert:** `VaultAssetCompositionDivergence` (page)
**Dashboard:** [SLO — Money path integrity](/d/nester-slo-money-path/money-path-integrity)
**Issue:** nester#1421

---

## What the alert means

**A vault's reported `total_assets` does not match the sum of what it actually holds.**

This is the independent check that catches exploits and accounting bugs. Unlike `ReconciliationDivergence` (which compares our database against the chain), this compares **chain against chain**:

- **total_assets()**: What the vault contract reports
- **actual composition**: USDC SAC balance + sum of adapter positions

When these disagree beyond the configured basis point threshold, one of three things has happened:

1. **The vault's accounting is broken** — a bug in the `total_assets` calculation
2. **An exploit has drained funds** without updating the vault's internal balance
3. **An adapter is misreporting positions** — deployed amount doesn't match reality

This pages immediately because money is missing or miscounted, and the vault's own view of its holdings cannot be trusted.

---

## First three actions

**1. Identify the affected vaults and the magnitude of divergence.**

Query `reconciliation_findings` for recent composition mismatches:

```sql
SELECT 
  vault_id,
  entity_id,
  recorded_value,  -- total_assets (what vault reports)
  on_chain_value,  -- USDC + adapters (what it actually holds)
  details->>'divergence_bps' as divergence_bps,
  details->>'usdc_balance' as usdc_balance,
  details->>'adapter_total' as adapter_total,
  severity,
  observed_at
FROM reconciliation_findings
WHERE entity_type = 'vault_composition'
  AND observed_at > NOW() - INTERVAL '1 hour'
ORDER BY observed_at DESC;
```

Alternatively, check logs for composition divergence warnings:

```bash
grep "vault asset composition divergence detected" <api logs>
```

**2. Calculate the missing or excess amount.**

```
delta = |total_assets - (USDC_balance + adapter_total)|
divergence_bps = (delta / total_assets) * 10000
```

- Small divergence (< 50 bps): Likely rounding or dust
- Large divergence (> 100 bps): Serious accounting error or exploit
- total_assets = 0 but actual > 0: Vault thinks it's empty but holds funds (critical)
- total_assets > 0 but actual = 0: Vault thinks it has funds but they're gone (exploit scenario)

**3. Check recent vault activity for the affected vault(s).**

```sql
SELECT type, amount, status, created_at
FROM transactions
WHERE vault_id = '<vault_id>'
  AND created_at > NOW() - INTERVAL '24 hours'
ORDER BY created_at DESC
LIMIT 50;
```

Look for:
- Large withdrawals or deposits immediately before divergence appeared
- Rebalance operations
- Failed transactions that may have partially executed
- Admin operations (pause, unpause, rebalance)

---

## Cause A — Rounding or dust accumulation

**Symptoms:** Divergence is very small (< 10 bps), affects multiple vaults, appeared gradually.

Small discrepancies can accumulate from:
- Rounding differences between vault internal accounting and SAC balance precision
- Yield accrual not yet reflected in `total_assets`
- Adapter position updates lagging actual deployment

**Diagnosis:**

1. Check if divergence is consistent across vaults:

   ```sql
   SELECT 
     COUNT(*) as affected_vaults,
     AVG((details->>'divergence_bps')::numeric) as avg_divergence_bps,
     MAX((details->>'divergence_bps')::numeric) as max_divergence_bps
   FROM reconciliation_findings
   WHERE entity_type = 'vault_composition'
     AND observed_at > NOW() - INTERVAL '1 hour';
   ```

2. If many vaults have similar small divergences, this may be systemic rounding.

**Resolution:**

- If divergence is < 10 bps and stable: Consider raising `DivergenceThresholdBPS` in config
- If divergence is growing: This is not rounding; escalate to Cause B or C

---

## Cause B — Vault accounting bug

**Symptoms:** Divergence appeared after a vault contract upgrade or affects vaults with recent rebalance operations.

The vault's `total_assets` calculation may not correctly account for:
- Deployed adapter positions
- Idle USDC balance
- Yield accrued but not yet reflected
- Fees or penalties not yet applied

**Diagnosis:**

1. Read the values directly from the chain:

   ```bash
   # Requires soroban CLI and vault address
   soroban contract invoke \
     --id <vault_address> \
     --network mainnet \
     -- total_assets
   
   soroban contract invoke \
     --id <usdc_sac_address> \
     --network mainnet \
     -- balance --id <vault_address>
   ```

2. Compare against what the reconciler saw (from logs or DB).

3. Check vault's adapter allocations:

   ```bash
   soroban contract invoke \
     --id <vault_address> \
     --network mainnet \
     -- get_sources
   
   # For each source:
   soroban contract invoke \
     --id <vault_address> \
     --network mainnet \
     -- get_source_allocation --source_id <source_id>
   ```

4. Manually sum USDC + adapters. Does it match `total_assets`?

**Resolution:**

- If manual calculation agrees with reconciler but disagrees with `total_assets`: **Vault contract bug**. Pause affected vaults immediately and escalate to contract team.
- If manual calculation matches `total_assets` but reconciler diverges: Check reconciler logic (USDC reader, adapter reader).

---

## Cause C — Exploit or unauthorized withdrawal

**Symptoms:** Large sudden divergence (> 100 bps), `total_assets` > actual composition (vault thinks it has more than it does), no corresponding transactions in our DB.

This is the worst case: funds have left the vault without updating `total_assets`.

**Diagnosis:**

1. Check recent vault transactions on-chain via Horizon:

   ```bash
   curl "https://horizon.stellar.org/accounts/<vault_address>/transactions?limit=50&order=desc"
   ```

2. Look for:
   - Unauthorized withdrawals (transfers from vault to unknown addresses)
   - Contract invocations we didn't initiate
   - Admin key compromise (if vault has emergency withdrawal function)

3. Check if USDC balance dropped suddenly:

   ```sql
   SELECT created_at, amount, type, tx_hash
   FROM vault_balance_changes
   WHERE vault_id = '<vault_id>'
     AND created_at > NOW() - INTERVAL '48 hours'
   ORDER BY created_at DESC;
   ```

**Resolution:**

**If exploitation is confirmed:**

1. **Pause all affected vaults immediately** via admin endpoint:

   ```bash
   curl -X POST https://api.nester.example/api/v1/admin/vaults/<vault_id>/pause \
     -H "Authorization: Bearer <admin_token>"
   ```

2. **Freeze deposits and withdrawals** for all vaults using the same contract version.

3. **Notify security team and stakeholders immediately.** This is a user-funds-at-risk incident.

4. **Preserve evidence:** Export all recent transactions, logs, and reconciliation findings before they roll over.

5. **Coordinate with Stellar validators** if chain-level rollback is needed (extremely rare, requires network consensus).

---

## Cause D — Adapter misreporting positions

**Symptoms:** Divergence correlates with specific adapter protocols, `adapter_total` from reconciler doesn't match on-chain `get_source_allocation`.

An adapter is reporting incorrect deployed amounts, either:
- Adapter contract bug in position tracking
- Adapter protocol experienced a loss not yet reflected in reported position
- Adapter was exploited independently of the vault

**Diagnosis:**

1. Break down divergence by adapter:

   Check `details->>'adapter_total'` in findings and compare against individual adapter positions read from vault.

2. For each adapter, verify the reported position matches what the underlying protocol shows:

   ```bash
   # Read from adapter contract or underlying protocol
   # (specific commands depend on adapter implementation)
   ```

3. Check adapter events for unexpected withdrawals or losses:

   ```sql
   SELECT *
   FROM adapter_events
   WHERE vault_id = '<vault_id>'
     AND created_at > NOW() - INTERVAL '24 hours'
   ORDER BY created_at DESC;
   ```

**Resolution:**

- If one adapter consistently misreports: **Pause that adapter** and investigate its position tracking logic.
- If adapter suffered a loss: Update vault accounting to reflect the loss and investigate why loss wasn't propagated to `total_assets`.

---

## Verifying recovery

Do not resolve until all three conditions are met:

```promql
# 1. No new composition divergences in last 15 minutes
sum(increase(nester_reconcile_divergences_total{kind="composition_mismatch"}[15m])) == 0

# 2. Reconciler is running and completing passes
reconcile:balance:last_run_age_seconds < 600

# 3. Affected vaults now agree (query reconciliation_findings)
```

Re-check the affected vaults manually:

```sql
SELECT 
  vault_id,
  details->>'divergence_bps' as divergence_bps,
  observed_at
FROM reconciliation_findings
WHERE entity_type = 'vault_composition'
  AND vault_id IN ('<affected_vault_ids>')
ORDER BY observed_at DESC
LIMIT 1;
```

If divergence persists after supposed fix, the root cause was misdiagnosed — return to First Actions.

---

## Related

- [Money-path integrity](money-path-integrity.md) — Reconciliation overview and balance divergence alerts
- [Balance freshness](balance-freshness.md) — Indexer lag (can affect vault balance views)
- [Error Budget Policy](../error-budget-policy.md) — When money-path issues exhaust error budget

---

## Configuration

**Environment variables:**

- `RECONCILE_ENABLED`: Must be `true` for this check to run
- `RECONCILE_INTERVAL`: Cadence of composition checks (default 5m)
- `USDC_CONTRACT_ADDRESS`: USDC SAC address (required for balance reads)
- Threshold is set in comparator construction: `DivergenceThresholdBPS` (default 10 bp = 0.1%)

**Metric:**

```promql
nester_reconcile_divergences_total{kind="composition_mismatch"}
```

**Alert threshold:** Any divergence detected within a 5-minute window pages immediately.
