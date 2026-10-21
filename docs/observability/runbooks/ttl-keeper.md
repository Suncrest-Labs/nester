# Runbook: TTL Keeper & Contract Entry Maintenance

## Overview

Soroban smart contracts on Stellar require storage rent to maintain live ledger entries. In the Nester protocol, global vault instance state and user position keys (principal, shares, tenure) expire unless their Time-To-Live (TTL) is periodically extended.

The **TTL Keeper** (`apps/api/internal/scheduler/ttl_keeper.go`) is a leader-elected background job that:
1. Queries Soroban RPC `getLedgerEntries` for each contract instance and known user key to inspect `liveUntilLedgerSeq`.
2. Extends entries within 7 days (~120,960 ledgers) of expiration to 30 days (~518,400 ledgers) using `ExtendFootprintTTLOp` or contract `extend_ttl` entrypoints.
3. Exports the gauge `nester_contract_entry_ttl_ledgers_remaining{contract,kind}` to Prometheus.

If an entry drops below 7 days, the alert `ContractEntryTTLLow` fires.

---

## How to Fund the Keeper Account

The TTL Keeper signs and submits transactions using the operator key configured via `OPERATOR_SECRET` (or a remote signer). Every extension transaction consumes a small network resource fee and base fee in XLM.

### 1. Identify the Keeper Address

Run the following command or check the API startup logs:
```bash
# Via stellar CLI or API environment:
echo $OPERATOR_ADDRESS
# Or derived from secret:
stellar keys address keeper
```

### 2. Check Keeper Account Balance

```bash
# Query Horizon for native XLM balance:
curl -s "https://horizon-testnet.stellar.org/accounts/<KEEPER_ADDRESS>" | jq '.balances[] | select(.asset_type=="native")'
```

Ensure the balance has at least **10 XLM** on testnet, or **50 XLM** on mainnet to maintain sufficient buffer for rent extensions.

### 3. Funding on Testnet

Use Stellar Friendbot to fund the keeper account:
```bash
curl -X POST "https://friendbot.stellar.org?addr=<KEEPER_ADDRESS>"
```
Or with the Stellar CLI:
```bash
stellar keys fund <KEEPER_ADDRESS> --network testnet
```

### 4. Funding on Mainnet

Transfer native XLM from the protocol operational treasury to the keeper account:
```bash
stellar tx send \
  --source treasury-hot \
  --destination <KEEPER_ADDRESS> \
  --amount 100 \
  --network mainnet
```

---

## Troubleshooting `ContractEntryTTLLow` Alert

When `ContractEntryTTLLow` fires:
1. **Check Keeper Account Balance**: If the keeper account has insufficient XLM to pay transaction fees, transactions fail. Follow the funding steps above.
2. **Verify Leader Status**: Ensure an API pod holds the scheduler leadership lock (`SELECT * FROM leader_locks;`).
3. **Inspect Logs**: Search API logs for `ttl keeper`:
   ```bash
   kubectl logs -l app=nester-api | grep "ttl keeper"
   ```
4. **Manual Extension**: If an entry is close to expiration and the keeper cannot run immediately, extend it manually:
   ```bash
   stellar contract invoke \
     --id <CONTRACT_ID> \
     --source <OPERATOR_SECRET> \
     --network testnet \
     -- extend_ttl
   ```
