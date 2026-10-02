# Mainnet On-Call Rotation and Paging Setup

This document defines the on-call rotation, escalation policies, and paging tool integrations (PagerDuty and Opsgenie) for Nester's mainnet money-path alerts.

## 1. Scope & Critical Alerts

The following critical alerts page the on-call engineer immediately:
- `ReconciliationDivergence`: Ledger disagreeing with chain state.
- `ReconciliationStalled` / `BalanceReconciliationStalled`: Liveness failure of reconciliation loops.
- `HaltedVaultIncident`: Critical vaults halted unexpectedly or emergency actions triggered.
- `ReadinessProbeFailure`: Core API or dependency health check failures.

## 2. On-Call Rotation Schedule

- **Rotation Type**: Weekly handoff (Mondays at 09:00 UTC).
- **Tiers**: Primary (pages immediately) -> Secondary (pages after 15 minutes if unacknowledged).
- **Escalation**: PagerDuty / Opsgenie escalation policy `nester-mainnet-money-path-escalation`.

## 3. PagerDuty & Opsgenie Integration Config

Configured in Alertmanager (`docker/alertmanager/alertmanager.yml`) via webhooks and integration keys:
- `PAGERDUTY_URL` / `OPSGENIE_API_KEY` environment variables.
