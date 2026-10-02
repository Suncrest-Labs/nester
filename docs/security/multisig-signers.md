# Mainnet Multisig: Signer Set, Key Custody & Rotation

This is the source of truth for **who can sign** for Nester's mainnet
administrative authority, **how their keys are held**, and **how a signer is
replaced** when they leave or a key is compromised. It backs item 2
("Multisig Live") of the [Launch Checklist](../LAUNCH_CHECKLIST.md).

> **Never commit secrets here.** This document records public account
> addresses (`G…`), roles and custody *types* only. Seed phrases, PINs and
> recovery material are never written to the repo, chat, or tickets.

## 1. What the multisig controls

| Authority | Mechanism | Notes |
| --- | --- | --- |
| Vault `Admin` role (fees, pause, cooldowns, harvest interval, allocations) | Stellar multisig account holds the contract `Admin` role | Changed via `transfer_admin` / `accept_admin` |
| Contract upgrades | `propose_upgrade` → timelock delay → `execute_upgrade` | Proposal and execution both require the multisig |
| Treasury | Treasury address is the multisig account | Fee receipts and withdrawals |
| Role grants (`Manager`, keepers, etc.) | `grant_role` / `revoke_role` by `Admin` | Prefer narrow roles over `Admin` |

Day-to-day operational keys (rebalance keeper, API signer) are **not** part of
the multisig and are covered in [signing-isolation.md](signing-isolation.md)
and [key-rotation.md](key-rotation.md).

## 2. Signing policy

| Parameter | Value |
| --- | --- |
| Multisig account | `G…` _(fill in at launch)_ |
| Threshold (medium/high) | **M-of-N** _(fill in; recommended 3-of-5)_ |
| Master key weight | **0** (disabled) after signers are added |
| Signer weights | 1 each, so threshold = number of required approvers |
| Timelock delay (upgrades) | _(fill in; must match deployed timelock config)_ |

Rules:

- No single party, organisation, or device may hold `M` or more keys.
- Signers must be independent people; no two signers share an employer-managed
  device, password manager, or physical location.
- The master key weight must be 0; verify with the Stellar account lookup
  before every launch gate review.

## 3. Signer registry

Maintained by the Security Council lead. Update this table in the **same PR**
as any signer change (see §5). Leave no row stale.

| # | Role / holder | Public key | Custody | Added | Last verified |
| --- | --- | --- | --- | --- | --- |
| 1 | _Core maintainer A_ | `G…` | Hardware wallet (Ledger) | _YYYY-MM-DD_ | _YYYY-MM-DD_ |
| 2 | _Core maintainer B_ | `G…` | Hardware wallet (Ledger) | _YYYY-MM-DD_ | _YYYY-MM-DD_ |
| 3 | _Security Council member_ | `G…` | Hardware wallet (Trezor) | _YYYY-MM-DD_ | _YYYY-MM-DD_ |
| 4 | _Independent advisor_ | `G…` | Hardware wallet | _YYYY-MM-DD_ | _YYYY-MM-DD_ |
| 5 | _Custody provider_ | `G…` | Institutional custody (policy-gated MPC/HSM) | _YYYY-MM-DD_ | _YYYY-MM-DD_ |

## 4. Key custody standards

### Hardware wallet signers

- Key generated **on the device**; the seed is never imported from, or typed
  into, a computer or phone.
- Device PIN set by the holder; firmware kept at the vendor's current
  release and verified via the vendor's own tooling.
- Recovery phrase written on durable (metal) backup, stored separately from
  the device in a locked location known only to the holder, and never
  photographed or digitised.
- Transactions are signed only after verifying the **full transaction
  contents on the device screen** (destination, operation, contract function,
  hash), never from the host UI alone.
- Holder keeps the device on their person or in locked storage; no shared
  devices.

### Custody-provider signer

- Signing is policy-gated: the provider only signs requests that match an
  allowlisted multisig account and are approved by the provider's own
  quorum.
- Access to the provider console requires SSO + hardware-key MFA; the
  console account list is reviewed at every quarterly verification (§6).
- The provider's API credentials never live in this repository or the API
  environment.

### Everyone

- Each signer holds exactly one key on this multisig.
- Loss, theft, suspected exposure, or a lost/replaced device is reported to
  the Security Council **immediately** (see
  [incident-response.md](incident-response.md)); do not wait to confirm.

## 5. Signer rotation procedure

Use the same procedure for planned rotation (departure, role change, device
replacement) and for compromise. The only difference is urgency (§5.3).

### 5.1 Planned rotation (signer departs or changes device)

1. **Open a tracked change** naming the outgoing signer, the incoming signer,
   and the reason. Two Security Council members approve it.
2. **Incoming signer generates a fresh key** on a new hardware wallet and
   sends the public key (`G…`) over a verified channel. Confirm the address by
   a second channel (e.g. read it back on a call).
3. **Add the new signer first** (weight 1) with a multisig `SetOptions`
   transaction signed by `M` current signers. At this point there are `N+1`
   signers; the threshold is unchanged.
4. **Verify the new key can sign**: have it co-sign a harmless transaction
   (e.g. a no-op `bump_sequence` on the multisig account in a test
   envelope) before removing anyone.
5. **Remove the outgoing signer** (weight 0) with a second `SetOptions`
   transaction signed by `M` signers that does **not** include the outgoing
   key.
6. **Re-check the threshold.** Confirm the account still has at least `M`
   active signers and that no reachable subset smaller than `M` can meet the
   threshold.
7. **Verify on-chain**: fetch the account from Horizon and confirm the signer
   list, weights, thresholds and master weight match §2.
8. **Update §3** in a PR, with the new key, custody type and date. Link the
   transaction hashes.
9. **Offboard the old holder**: they destroy or hand over their device for
   wiping and revoke any custody-provider console access.

### 5.2 Contract-level roles

If the departing person held an individual contract role (not via the
multisig), revoke it in the same change window with `revoke_role`. If the
multisig account itself must change (e.g. a new account is created),
use `transfer_admin` followed by `accept_admin` from the new account, and
re-point the treasury address.

### 5.3 Compromise (key exposed, device lost/stolen, signer coerced)

Time matters; reduce the signer's power **first**, tidy up second.

1. **Declare an incident** and page the Security Council.
2. **Do not use the suspect key** for anything, including to remove itself
   if another path exists.
3. **If fewer than `M` keys are compromised**, the attacker cannot sign alone.
   Proceed immediately with the §5.1 steps 2–8 with the compromised key
   excluded from signing. Skip step 4's delay when needed, but still verify
   the new key before relying on it.
4. **If `M` or more keys may be compromised**, treat the protocol as under
   active attack:
   - Pause deposits and withdrawals using the emergency switches (see
     [MAINNET_LAUNCH_ROLLBACK.md](../MAINNET_LAUNCH_ROLLBACK.md)) and the
     vault `pause`, if a non-compromised path still exists.
   - Watch the timelock queue; **cancel** any pending upgrade the attacker
     proposed (`cancel_upgrade`) while the delay is running. This is why the
     upgrade timelock must never be shortened to near zero.
   - Move treasury funds to a new account if signing is still possible.
5. **Preserve evidence** (transaction hashes, device state, access logs)
   before wiping anything.
6. **Post-incident**: publish the signer changes, update §3, and add a
   follow-up to the [launch readiness register](../launch-readiness-register.md).

## 6. Verification cadence

| Check | Frequency | Owner |
| --- | --- | --- |
| Compare on-chain signer list, weights, thresholds with §2/§3 | Before launch; monthly after | Security Council lead |
| Each signer proves control (co-signs a test envelope) | Quarterly | Each signer |
| Review custody-provider console users | Quarterly | Security Council lead |
| Rotation drill on testnet (add + remove a signer end to end) | Before launch; every 6 months | Core maintainers |
| Confirm no signer has left the organisation unremoved | Monthly | Security Council lead |

Record each completed check (date, who, result) in the
[launch readiness register](../launch-readiness-register.md) while launch is
in progress, and in the team's ops log afterwards.
