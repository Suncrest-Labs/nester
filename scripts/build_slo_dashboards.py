#!/usr/bin/env python3
"""Generate the SLO Grafana dashboards (nester#1056).

The dashboards are generated rather than hand-written because every one of
them repeats the same five-panel structure — attainment, budget remaining,
burn rate, the error ratio over time, and the supporting signals — against a
different set of recorded series. Hand-maintaining that in JSON means a
threshold corrected in one dashboard and missed in another, which is the same
class of drift the recording rules exist to prevent.

The generated JSON is committed. This script is the source of truth for it;
run it after changing a target or a panel and commit the result:

    python scripts/build_slo_dashboards.py

CI checks that the committed JSON matches what this script produces, so the
two cannot silently diverge.

Every panel reads the recorded series from slo_recording.yml, never a raw
counter. That is what keeps a dashboard from disagreeing with the pager during
an incident.
"""

from __future__ import annotations

import json
import pathlib
from typing import Any

# Targets, mirrored from slo_alerts.yml. Kept in one place here so a dashboard
# cannot display a target the alerts do not enforce.
FAST_BURN_MULTIPLIER = 13.44
SLOW_BURN_MULTIPLIER = 5.6

OUT_DIR = pathlib.Path(__file__).resolve().parent.parent / "docker" / "grafana" / "dashboards"

DATASOURCE: dict[str, str] = {"type": "prometheus", "uid": "${DS_PROMETHEUS}"}


def _target(expr: str, legend: str, instant: bool = False) -> dict[str, Any]:
    return {
        "datasource": DATASOURCE,
        "editorMode": "code",
        "expr": expr,
        "legendFormat": legend,
        "range": not instant,
        "instant": instant,
        "refId": legend[:8] or "A",
    }


def _stat(
    title: str,
    expr: str,
    unit: str,
    grid: dict[str, int],
    *,
    description: str,
    thresholds: list[dict[str, Any]] | None = None,
    decimals: int = 3,
) -> dict[str, Any]:
    return {
        "type": "stat",
        "title": title,
        "description": description,
        "datasource": DATASOURCE,
        "gridPos": grid,
        "fieldConfig": {
            "defaults": {
                "unit": unit,
                "decimals": decimals,
                "mappings": [],
                "thresholds": {
                    "mode": "absolute",
                    "steps": thresholds
                    or [{"color": "green", "value": None}],
                },
            },
            "overrides": [],
        },
        "options": {
            "colorMode": "value",
            "graphMode": "area",
            "justifyMode": "auto",
            "reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
            "textMode": "auto",
        },
        "targets": [_target(expr, title, instant=True)],
    }


def _timeseries(
    title: str,
    targets: list[tuple[str, str]],
    unit: str,
    grid: dict[str, int],
    *,
    description: str,
    thresholds: list[dict[str, Any]] | None = None,
) -> dict[str, Any]:
    return {
        "type": "timeseries",
        "title": title,
        "description": description,
        "datasource": DATASOURCE,
        "gridPos": grid,
        "fieldConfig": {
            "defaults": {
                "unit": unit,
                "custom": {
                    "drawStyle": "line",
                    "lineWidth": 2,
                    "fillOpacity": 8,
                    "showPoints": "never",
                    "spanNulls": False,
                    "thresholdsStyle": {"mode": "line" if thresholds else "off"},
                },
                "thresholds": {
                    "mode": "absolute",
                    "steps": thresholds or [{"color": "green", "value": None}],
                },
            },
            "overrides": [],
        },
        "options": {
            "legend": {"displayMode": "table", "placement": "bottom", "calcs": ["lastNotNull", "max"]},
            "tooltip": {"mode": "multi", "sort": "desc"},
        },
        "targets": [_target(expr, legend) for expr, legend in targets],
    }


def _text(title: str, content: str, grid: dict[str, int]) -> dict[str, Any]:
    return {
        "type": "text",
        "title": title,
        "gridPos": grid,
        "options": {"mode": "markdown", "content": content},
    }


def _dashboard(uid: str, title: str, description: str, panels: list[dict[str, Any]]) -> dict[str, Any]:
    for index, panel in enumerate(panels, start=1):
        panel["id"] = index

    return {
        "__inputs": [],
        "__requires": [],
        "annotations": {"list": []},
        "editable": False,
        "graphTooltip": 1,
        "links": [],
        "panels": panels,
        "refresh": "30s",
        "schemaVersion": 39,
        "tags": ["slo", "nester"],
        "templating": {
            "list": [
                {
                    "current": {},
                    "hide": 0,
                    "includeAll": False,
                    "label": "Data source",
                    "multi": False,
                    "name": "DS_PROMETHEUS",
                    "options": [],
                    "query": "prometheus",
                    "refresh": 1,
                    "type": "datasource",
                }
            ]
        },
        "time": {"from": "now-6h", "to": "now"},
        "timezone": "utc",
        "title": title,
        "description": description,
        "uid": uid,
        "version": 1,
        "weekStart": "",
    }


def _budget_panels(
    ratio_28d: str,
    budget: float,
    ratio_1h: str,
    ratio_6h: str,
    *,
    row: int,
) -> list[dict[str, Any]]:
    """The four panels every SLO dashboard opens with.

    Attainment, budget remaining, and the two burn rates. These are the
    numbers the error-budget policy is applied to, so they lead.
    """
    # Budget remaining as a fraction: 1 - (observed / allowed). Negative means
    # the budget is overspent, which is deliberately shown rather than clamped
    # at zero — "how far past" is what the policy escalation depends on.
    remaining = f"1 - ({ratio_28d} / {budget})"

    return [
        _stat(
            "Attainment (28d)",
            f"1 - {ratio_28d}",
            "percentunit",
            {"h": 5, "w": 6, "x": 0, "y": row},
            description=(
                "Observed success ratio over the rolling 28-day window. "
                "Compare against the target in the panel to the right."
            ),
            decimals=4,
            thresholds=[
                {"color": "red", "value": None},
                {"color": "green", "value": 1 - budget},
            ],
        ),
        _stat(
            "Error budget remaining",
            remaining,
            "percentunit",
            {"h": 5, "w": 6, "x": 6, "y": row},
            description=(
                "Share of the 28-day error budget still unspent. Negative "
                "means the budget is overspent and the error-budget policy "
                "applies — see docs/observability/error-budget-policy.md."
            ),
            decimals=1,
            thresholds=[
                {"color": "red", "value": None},
                {"color": "orange", "value": 0.25},
                {"color": "green", "value": 0.5},
            ],
        ),
        _stat(
            "Burn rate (1h)",
            f"{ratio_1h} / {budget}",
            "none",
            {"h": 5, "w": 6, "x": 12, "y": row},
            description=(
                f"Multiples of the sustainable burn rate over the last hour. "
                f"1x exhausts the budget in exactly 28 days. The fast-burn "
                f"alert pages above {FAST_BURN_MULTIPLIER}x."
            ),
            decimals=2,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": SLOW_BURN_MULTIPLIER},
                {"color": "red", "value": FAST_BURN_MULTIPLIER},
            ],
        ),
        _stat(
            "Burn rate (6h)",
            f"{ratio_6h} / {budget}",
            "none",
            {"h": 5, "w": 6, "x": 18, "y": row},
            description=(
                f"Multiples of the sustainable burn rate over six hours. The "
                f"slow-burn alert opens a ticket above {SLOW_BURN_MULTIPLIER}x."
            ),
            decimals=2,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": SLOW_BURN_MULTIPLIER},
                {"color": "red", "value": FAST_BURN_MULTIPLIER},
            ],
        ),
    ]


def build_api_dashboard() -> dict[str, Any]:
    budget = 0.001  # 99.9%

    panels: list[dict[str, Any]] = [
        _text(
            "API availability — 99.9% over 28 days",
            (
                "**SLI** non-5xx over eligible requests. 4xx and unmatched routes "
                "(`route=\"other\"`) are excluded from both numerator and denominator, "
                "so a client cannot manufacture a breach with invalid requests.\n\n"
                "**Budget** 0.1% = ~40 minutes per 28 days.\n\n"
                "**Runbook** `docs/observability/runbooks/api-availability.md`"
            ),
            {"h": 4, "w": 24, "x": 0, "y": 0},
        )
    ]

    panels += _budget_panels(
        "api:availability:error_ratio_rate28d",
        budget,
        "api:availability:error_ratio_rate1h",
        "api:availability:error_ratio_rate6h",
        row=4,
    )

    panels += [
        _timeseries(
            "5xx ratio vs burn thresholds",
            [
                ("api:availability:error_ratio_rate5m", "5m"),
                ("api:availability:error_ratio_rate1h", "1h"),
                ("api:availability:error_ratio_rate6h", "6h"),
            ],
            "percentunit",
            {"h": 9, "w": 16, "x": 0, "y": 9},
            description=(
                "The ratio the alerts fire on. Fast burn pages when the 1h and "
                "5m series both exceed 1.344%; slow burn tickets when 6h and "
                "30m both exceed 0.56%."
            ),
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": budget * SLOW_BURN_MULTIPLIER},
                {"color": "red", "value": budget * FAST_BURN_MULTIPLIER},
            ],
        ),
        _timeseries(
            "Eligible request rate",
            [("api:availability:eligible_rate5m", "eligible/s")],
            "reqps",
            {"h": 9, "w": 8, "x": 16, "y": 9},
            description=(
                "Traffic the SLI is computed over. Plotted beside the ratio "
                "because a ratio improving while volume collapses is a load-"
                "shedding or outage signal, not a recovery."
            ),
        ),
        _timeseries(
            "Requests by status class",
            [
                (
                    'sum by (status_class) (rate(nester_http_requests_total{route!="other"}[5m]))',
                    "{{status_class}}",
                )
            ],
            "reqps",
            {"h": 8, "w": 12, "x": 0, "y": 18},
            description="Where the traffic is landing. 4xx is excluded from the SLI but shown here.",
        ),
        _timeseries(
            "Slowest routes (p95)",
            [
                (
                    "topk(5, histogram_quantile(0.95, sum by (route, le) "
                    "(rate(nester_http_request_duration_seconds_bucket[5m]))))",
                    "{{route}}",
                )
            ],
            "s",
            {"h": 8, "w": 12, "x": 12, "y": 18},
            description=(
                "Latency by route pattern, for locating which handler is "
                "responsible when availability degrades."
            ),
        ),
        _timeseries(
            "Dependency health",
            [
                (
                    "sum by (upstream) (rate(nester_outbound_requests_total{status_class=\"5xx\"}[5m]))",
                    "{{upstream}} 5xx/s",
                ),
                (
                    "sum by (upstream, kind) (rate(nester_outbound_errors_total[5m]))",
                    "{{upstream}} {{kind}}/s",
                ),
                ("sum(rate(nester_redis_errors_total[5m]))", "redis errors/s"),
            ],
            "reqps",
            {"h": 8, "w": 24, "x": 0, "y": 26},
            description=(
                "Upstream failures, which are the usual cause of a 5xx spike. "
                "Transport errors (timeout, dns, connect) never carry a status "
                "code, so they are counted separately."
            ),
        ),
    ]

    return _dashboard(
        "nester-slo-api",
        "SLO — API availability",
        "Availability SLO, error budget, and burn rate for the Go API (nester#1056).",
        panels,
    )


def build_flow_dashboard() -> dict[str, Any]:
    success_budget = 0.005  # 99.5%
    latency_budget = 0.01  # 99%

    panels: list[dict[str, Any]] = [
        _text(
            "Deposits and withdrawals — 99.5% success, 99% under 30s",
            (
                "**Success SLI** succeeded over (succeeded + failed_chain + "
                "failed_internal). User cancellations and rejected (invalid) "
                "requests are excluded from both halves.\n\n"
                "**Latency SLI** share of successful settlements completing within "
                "30s (~6 ledgers).\n\n"
                "Deposits and withdrawals carry separate budgets: averaging them "
                "would let healthy deposit volume hide a withdrawal outage.\n\n"
                "**Runbooks** `flow-success.md`, `flow-latency.md`"
            ),
            {"h": 5, "w": 24, "x": 0, "y": 0},
        )
    ]

    panels += _budget_panels(
        "max(flow:success:error_ratio_rate28d)",
        success_budget,
        "max(flow:success:error_ratio_rate1h)",
        "max(flow:success:error_ratio_rate6h)",
        row=5,
    )

    panels += [
        _timeseries(
            "Failure ratio by flow",
            [
                ("flow:success:error_ratio_rate5m", "{{flow}} 5m"),
                ("flow:success:error_ratio_rate1h", "{{flow}} 1h"),
            ],
            "percentunit",
            {"h": 9, "w": 12, "x": 0, "y": 10},
            description="Per-flow failure ratio against the 6.72% fast and 2.8% slow thresholds.",
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": success_budget * SLOW_BURN_MULTIPLIER},
                {"color": "red", "value": success_budget * FAST_BURN_MULTIPLIER},
            ],
        ),
        _timeseries(
            "Attempts by outcome",
            [
                (
                    "sum by (flow, outcome) (rate(nester_flow_attempts_total[5m]))",
                    "{{flow}} {{outcome}}",
                )
            ],
            "reqps",
            {"h": 9, "w": 12, "x": 12, "y": 10},
            description=(
                "The whole picture including excluded outcomes. A spike in "
                "`cancelled` points at the signing prompt; a spike in "
                "`rejected` at a client sending invalid requests. Neither "
                "burns budget, but both are worth seeing."
            ),
        ),
        _timeseries(
            "Settlement latency p95",
            [("flow:latency:p95_5m", "{{flow}} p95")],
            "s",
            {"h": 8, "w": 12, "x": 0, "y": 19},
            description="Successful settlements only. The 30s line is the latency SLI threshold.",
            thresholds=[
                {"color": "green", "value": None},
                {"color": "red", "value": 30},
            ],
        ),
        _timeseries(
            "Share of settlements slower than 30s",
            [
                ("flow:latency:error_ratio_rate5m", "{{flow}} 5m"),
                ("flow:latency:error_ratio_rate1h", "{{flow}} 1h"),
            ],
            "percentunit",
            {"h": 8, "w": 12, "x": 12, "y": 19},
            description="The latency SLI itself, counted as slow events rather than as a percentile.",
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": latency_budget * SLOW_BURN_MULTIPLIER},
                {"color": "red", "value": latency_budget * FAST_BURN_MULTIPLIER},
            ],
        ),
        _timeseries(
            "Soroban RPC health",
            [
                (
                    'sum by (status_class) (rate(nester_outbound_requests_total{upstream="soroban_rpc"}[5m]))',
                    "soroban {{status_class}}",
                ),
                (
                    'sum by (kind) (rate(nester_outbound_errors_total{upstream="soroban_rpc"}[5m]))',
                    "soroban {{kind}}",
                ),
                (
                    'histogram_quantile(0.95, sum by (le) '
                    '(rate(nester_outbound_request_duration_seconds_bucket{upstream="soroban_rpc"}[5m])))',
                    "soroban p95",
                ),
            ],
            "reqps",
            {"h": 8, "w": 24, "x": 0, "y": 27},
            description=(
                "The dependency that decides whether a flow failure is ours. "
                "The runbook's first branch is exactly this panel."
            ),
        ),
    ]

    return _dashboard(
        "nester-slo-flow",
        "SLO — Deposits and withdrawals",
        "Success and latency SLOs, budgets, and burn rates for the money paths (nester#1056).",
        panels,
    )


def build_balance_dashboard() -> dict[str, Any]:
    panels = [
        _text(
            "Balance freshness — staleness budget 5 minutes",
            (
                "**SLI** how far behind the chain the indexed view is, in "
                "ledgers and in seconds.\n\n"
                "This is a gauge, not a ratio of events, so it has no burn rate "
                "and no error budget — forcing one would produce a number that "
                "means nothing during an incident.\n\n"
                "**Read data staleness first.** It is the number the staleness "
                "budget, the page, and the `X-Indexer-Stale` header the API "
                "returns to clients are all stated against, and it is the only "
                "one that keeps climbing when the indexer has stopped "
                "completely.\n\n"
                "**Runbook** `docs/observability/runbooks/balance-freshness.md`"
            ),
            {"h": 5, "w": 24, "x": 0, "y": 0},
        ),
        _stat(
            "Data staleness",
            "indexer:freshness:lag_seconds",
            "s",
            {"h": 5, "w": 6, "x": 0, "y": 5},
            description=(
                "Age of the indexed view: time since the last freshness sample "
                "plus that sample's ledger lag. Pages above the budget (300s), "
                "and above it the API reports every response as stale."
            ),
            decimals=0,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": 120},
                {"color": "red", "value": 300},
            ],
        ),
        _stat(
            "Indexer lag",
            "indexer:freshness:lag_ledgers",
            "none",
            {"h": 5, "w": 6, "x": 6, "y": 5},
            description=(
                "Ledgers behind the tip. Pages above 60 (~5 minutes). No data "
                "here means the indexer has not reported a position at all — "
                "read data staleness instead."
            ),
            decimals=0,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": 30},
                {"color": "red", "value": 60},
            ],
        ),
        _stat(
            "Lag sample age",
            "indexer:freshness:sample_age_seconds",
            "s",
            {"h": 5, "w": 6, "x": 12, "y": 5},
            description=(
                "Seconds since the indexer last reported a position. Near zero "
                "means running but behind; climbing means stopped. This is the "
                "panel that tells the two apart."
            ),
            decimals=0,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": 120},
                {"color": "red", "value": 300},
            ],
        ),
        _stat(
            "Sample errors",
            "indexer:freshness:sample_error_rate5m",
            "none",
            {"h": 5, "w": 6, "x": 18, "y": 5},
            description="Failed lag samples per second. Non-zero means the indexer loop is erroring.",
            thresholds=[
                {"color": "green", "value": None},
                {"color": "red", "value": 0.001},
            ],
        ),
        _timeseries(
            "Data staleness against the budget",
            [
                ("indexer:freshness:lag_seconds", "staleness"),
                ("indexer:freshness:budget_seconds", "budget"),
            ],
            "s",
            {"h": 9, "w": 12, "x": 0, "y": 10},
            description=(
                "Staleness plotted against the budget the API is actually "
                "enforcing, so the crossing point is exactly where clients "
                "start being told their balances are stale. Recovery shows as "
                "a sharp drop back under the budget line."
            ),
            thresholds=[
                {"color": "green", "value": None},
                {"color": "red", "value": 300},
            ],
        ),
        _timeseries(
            "Indexer lag over time",
            [("indexer:freshness:lag_ledgers", "lag (ledgers)")],
            "none",
            {"h": 9, "w": 12, "x": 12, "y": 10},
            description="A sawtooth is normal (the cursor advances in steps). A monotonic climb is a stall.",
            thresholds=[
                {"color": "green", "value": None},
                {"color": "red", "value": 60},
            ],
        ),
        _timeseries(
            "Freshness signal age",
            [("indexer:freshness:sample_age_seconds", "sample age")],
            "s",
            {"h": 9, "w": 24, "x": 0, "y": 19},
            description=(
                "Should sit near zero and reset every poll. A steady climb means "
                "the indexer stopped reporting, and the ledger lag beside it is "
                "frozen at whatever it last read."
            ),
            thresholds=[
                {"color": "green", "value": None},
                {"color": "red", "value": 300},
            ],
        ),
    ]

    return _dashboard(
        "nester-slo-balance",
        "SLO — Balance freshness",
        "Indexer lag in ledgers and seconds against the staleness budget (nester#1056, nester#1088).",
        panels,
    )


def build_probe_dashboard() -> dict[str, Any]:
    panels = [
        _text(
            "Synthetic probes — staging",
            (
                "Probes exercise deposit, withdrawal, and balance read on a "
                "schedule, so a path that is broken while nobody is using it "
                "is found by us rather than by the first user who tries "
                "it.\n\n"
                "No error budget: a probe is a scheduled event, not a ratio "
                "over traffic, so there is nothing to burn. Alerts are "
                "absolute and ticket-severity — these run against staging, so "
                "a failure means a path is broken before it reaches "
                "production.\n\n"
                "**Runbook** `docs/observability/runbooks/synthetic-probes.md`"
            ),
            {"h": 5, "w": 24, "x": 0, "y": 0},
        ),
        _stat(
            "Probes passing",
            "sum(nester_probe_success)",
            "none",
            {"h": 5, "w": 8, "x": 0, "y": 5},
            description="Count of probes whose last run succeeded.",
            decimals=0,
            thresholds=[{"color": "red", "value": None}, {"color": "green", "value": 1}],
        ),
        _stat(
            "Oldest probe result",
            "max(time() - nester_probe_last_run_timestamp_seconds)",
            "s",
            {"h": 5, "w": 8, "x": 8, "y": 5},
            description=(
                "Age of the least recent probe result. Climbing past 30 "
                "minutes means the runner stopped — the success gauges beside "
                "it are then stale and must not be read as healthy."
            ),
            decimals=0,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": 900},
                {"color": "red", "value": 1800},
            ],
        ),
        _stat(
            "Slowest probe",
            "max(nester_probe_duration_seconds)",
            "s",
            {"h": 5, "w": 8, "x": 16, "y": 5},
            description="Duration of the slowest probe. Tickets above 60s.",
            decimals=1,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": 30},
                {"color": "red", "value": 60},
            ],
        ),
        _timeseries(
            "Probe outcome",
            [("nester_probe_success", "{{probe}}")],
            "none",
            {"h": 9, "w": 12, "x": 0, "y": 10},
            description="1 is passing, 0 is failing, per probe.",
        ),
        _timeseries(
            "Probe duration",
            [("nester_probe_duration_seconds", "{{probe}}")],
            "s",
            {"h": 9, "w": 12, "x": 12, "y": 10},
            description="Latency per probe. A rising trend precedes an outright failure.",
            thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 60}],
        ),
        _timeseries(
            "Failure reasons",
            [("nester_probe_last_reason", "{{probe}}: {{reason}}")],
            "none",
            {"h": 8, "w": 12, "x": 0, "y": 19},
            description=(
                "Bounded reason enum — timeout, connection, http_4xx, "
                "http_5xx, bad_payload, assertion, unknown. The detail behind "
                "a reason is in the workflow log, never in a label."
            ),
        ),
        _timeseries(
            "Result age",
            [("time() - nester_probe_last_run_timestamp_seconds", "{{probe}}")],
            "s",
            {"h": 8, "w": 12, "x": 12, "y": 19},
            description="Should sawtooth with the schedule. A steady climb means the runner stopped.",
            thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1800}],
        ),
    ]

    return _dashboard(
        "nester-slo-probes",
        "SLO — Synthetic probes",
        "Staging synthetic probe results for deposit, withdrawal, and balance (nester#1056).",
        panels,
    )


def build_money_path_dashboard() -> dict[str, Any]:
    panels = [
        _text(
            "Money path integrity — reconciliation and in-flight submissions",
            (
                "The deposit/withdrawal SLO answers *are transactions "
                "succeeding*. Balance freshness answers *is the indexer "
                "keeping up*. This board answers what neither can: **does our "
                "record of the money still agree with the chain, and is "
                "anything stuck in flight** (nester#1108).\n\n"
                "**Read the liveness panels first.** A divergence count of "
                "zero means one of two opposite things — *we checked and "
                "everything agrees*, or *we did not check*. The reconciler "
                "age and failed-pass panels are what tell those apart. If the "
                "reconciler is stalled, treat divergence silence as unknown, "
                "not clean.\n\n"
                "**Runbook** `docs/observability/runbooks/money-path-integrity.md`"
            ),
            {"h": 6, "w": 24, "x": 0, "y": 0},
        ),
        # Liveness first, deliberately: these gate the interpretation of every
        # panel below them.
        _stat(
            "Reconciler last run",
            "reconcile:health:last_run_age_seconds",
            "s",
            {"h": 5, "w": 6, "x": 0, "y": 6},
            description=(
                "Seconds since a pass completed, against a 15s interval. Pages "
                "above 600. If this is climbing, the divergence panels below "
                "are meaningless."
            ),
            decimals=0,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": 120},
                {"color": "red", "value": 600},
            ],
        ),
        _stat(
            "Failed passes",
            "reconcile:health:failed_run_rate5m",
            "none",
            {"h": 5, "w": 6, "x": 6, "y": 6},
            description=(
                "Passes per second that aborted before inspecting anything. "
                "Non-zero means the divergence count is falsely clean."
            ),
            thresholds=[
                {"color": "green", "value": None},
                {"color": "red", "value": 0.001},
            ],
        ),
        _stat(
            "Divergences (1h)",
            "reconcile:divergence:increase1h",
            "none",
            {"h": 5, "w": 6, "x": 12, "y": 6},
            description=(
                "Findings where our record and the chain disagree. Pages above "
                "zero — there is no acceptable steady-state rate."
            ),
            decimals=0,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "red", "value": 1},
            ],
        ),
        _stat(
            "Oldest pending submission",
            "submission:pending:oldest_age_seconds",
            "s",
            {"h": 5, "w": 6, "x": 18, "y": 6},
            description=(
                "Age of the oldest transaction awaiting a terminal status. "
                "Pages above 1800. This is money the user cannot see or reach."
            ),
            decimals=0,
            thresholds=[
                {"color": "green", "value": None},
                {"color": "orange", "value": 600},
                {"color": "red", "value": 1800},
            ],
        ),
        _timeseries(
            "Divergences by kind",
            [("reconcile:divergence:rate5m", "{{kind}}")],
            "none",
            {"h": 9, "w": 12, "x": 0, "y": 11},
            description=(
                "Split by kind because they are not equally serious. mismatch "
                "means both sides have the record and the values differ — a "
                "displayed balance is wrong, and re-indexing will not fix it. "
                "missing/extra mean a record exists on one side only. stuck "
                "means a submission has not reached a terminal state; a low "
                "steady rate is expected, a growing one is not."
            ),
        ),
        _timeseries(
            "Reconciliation passes by outcome",
            [("sum by (outcome) (rate(nester_reconcile_runs_total[5m]))", "{{outcome}}")],
            "none",
            {"h": 9, "w": 12, "x": 12, "y": 11},
            description=(
                "completed vs failed, summed across both loops (the tx "
                "poller and the balance reconciler, nester#1082). A healthy "
                "system shows a flat completed line and no failed line. "
                "failed means passes are running but completing nothing — a "
                "database error on the list-pending query, or the balance "
                "sweep unable to list vaults or reach the chain."
            ),
        ),
        _timeseries(
            "Pending submission backlog",
            [
                ("submission:pending:count", "queue depth"),
                ("submission:pending:oldest_age_seconds", "oldest age (s)"),
            ],
            "none",
            {"h": 9, "w": 12, "x": 0, "y": 20},
            description=(
                "Depth alone is ambiguous — a large queue that drains is "
                "healthy, a small one that never drains is not. Watch whether "
                "the oldest-age line resets or climbs monotonically; a climb "
                "with steady depth means the same transactions are stuck."
            ),
        ),
        _timeseries(
            "Reconciler age over time",
            [
                ("reconcile:health:last_run_age_seconds", "tx poller age"),
                ("reconcile:balance:last_run_age_seconds", "balance reconciler age"),
            ],
            "s",
            {"h": 9, "w": 12, "x": 12, "y": 20},
            description=(
                "Both loops should sawtooth near zero, resetting every pass. "
                "A monotonic climb means that loop stopped, and its integrity "
                "signals on this board froze with it. The tx poller pages "
                "above 600 (15s interval); the balance reconciler "
                "(nester#1082) pages above 1800 (5m interval). The series are "
                "separate deliberately: one loop's health must not vouch for "
                "the other's."
            ),
            thresholds=[
                {"color": "green", "value": None},
                {"color": "red", "value": 600},
            ],
        ),
    ]

    return _dashboard(
        "nester-slo-money-path",
        "SLO — Money path integrity",
        "Reconciliation divergences and in-flight submission backlog (nester#1108).",
        panels,
    )


DASHBOARDS = {
    "slo-api-availability.json": build_api_dashboard,
    "slo-deposits-withdrawals.json": build_flow_dashboard,
    "slo-balance-freshness.json": build_balance_dashboard,
    "slo-money-path-integrity.json": build_money_path_dashboard,
    "slo-synthetic-probes.json": build_probe_dashboard,
}


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)

    for filename, builder in DASHBOARDS.items():
        path = OUT_DIR / filename
        # sort_keys so the committed JSON is stable and a regeneration produces
        # no diff unless something actually changed.
        payload = json.dumps(builder(), indent=2, sort_keys=True) + "\n"
        # newline="" disables the platform line-ending translation Python
        # applies by default. Without it this writes CRLF on Windows and LF on
        # Linux, so the committed files and the CI-regenerated ones differ on
        # every line and the drift check fails for a reason that has nothing
        # to do with the dashboards.
        with open(path, "w", encoding="utf-8", newline="") as handle:
            handle.write(payload)
        print(f"wrote {path.relative_to(OUT_DIR.parent.parent.parent)}")


if __name__ == "__main__":
    main()
