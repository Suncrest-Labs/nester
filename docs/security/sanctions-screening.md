# Wallet Sanctions Screening Policy

## Overview

On Mainnet, Nester screens Stellar wallet addresses (`G...` and `C...` strkeys) against sanctioned digital-currency address lists at two mandatory enforcement checkpoints:

1. **Authentication (`POST /api/v1/auth/verify`)**: Before issuing a session or JWT access token (`AuthService.VerifyAndIssue`).
2. **Deposit Intent & Execution (`GET /api/v1/vaults/{id}/preview-deposit` and `POST /api/v1/vaults/{id}/deposit`)**: Before building deposit intents or recording deposits (`VaultService.PreviewDeposit` and `VaultService.RecordDeposit`).

## Architecture (`apps/api/internal/compliance`)

The screening subsystem implements the pluggable `compliance.Screener` (`service.WalletSanctionsScreener`) interface via `StandardScreener`, combining three layers:

1. **Static Seed List**: Immediate in-memory blocklist of known sanctioned Stellar addresses configured at startup.
2. **Daily OFAC SDN XML Refresh**: Automated parsing of the U.S. Treasury OFAC Specially Designated Nationals (`sdn.xml`) feed (`ParseOFACSDNXML`), extracting all valid Stellar public and contract strkeys (`G...` and `C...`).
3. **Optional Commercial Provider**: Pluggable `compliance.Provider` hook for real-time risk scoring (e.g. Chainalysis / TRM Labs).

## Enforcement, Audit Logging, and Alerting

- **Generic User Response**: Blocked requests return `HTTP 403 Forbidden` (`WALLET_RESTRICTED`) with the generic message `"request cannot be processed for this wallet address"` so watch-list specifics are never leaked to the caller.
- **Audit Trail**: Every hit is recorded in the append-only audit log (`compliance.sanctions_blocked`) with the wallet address, checkpoint (`auth_verify` or `deposit`), and source (`static_sanctions_list`, `ofac_sdn`, or `external_provider`).
- **Metrics & Paging**:
  - `nester_compliance_sanctions_hits_total`: Incremented on every blocked wallet address and accompanied by an `ERROR`-level compliance alert log.
  - `nester_compliance_sanctions_refresh_stale_alerts_total`: Incremented by `CheckStaleness()` when the OFAC SDN XML feed has not been refreshed within `DefaultMaxStaleness` (36 hours).

## Per-Network Configuration

Screening is configurable per network (`Config.Enabled`):
- **Mainnet**: Enabled by default (`Enabled: true`).
- **Testnet / Local Development**: Configurable (`Enabled: false` by default unless testing compliance flows).
