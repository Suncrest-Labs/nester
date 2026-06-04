# Nester Audit Threat Model

## Scope

- vault
- vault_token
- nester
- treasury
- allocation_strategy
- access_control
- timelock

## Primary Risks

- Privilege escalation through admin or governance paths
- Reentrancy in deposit, withdraw, yield, and rebalance flows
- Integer overflow or precision loss in accounting logic
- Fee manipulation, rounding drift, or inconsistent share accounting
- Unsafe cross-contract calls or stale state transitions

## Review Focus

- Access control boundaries and role checks
- Contract upgrade and timelock behavior
- Rebalance routing and allocation changes
- Yield accounting, fees, and share mint/burn flows
- Error handling and state consistency on failed calls

## Triage Policy

- Critical and high findings block launch
- Medium findings are fixed before or shortly after launch
- Low and informational findings are tracked as backlog items
