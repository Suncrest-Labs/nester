#!/usr/bin/env python3
"""Read-only Soroban Mainnet RPC poller soak harness.

Exercises the same read-only RPC pattern as apps/api/internal/stellar/poller.go
(getLatestLedger cold start with offset + periodic getEvents polling) against a
Soroban RPC endpoint for a configurable duration (default 24h for staging soak).

Collects lag p50/p95/p99, RPC latency p50/p95/p99, error counts (including HTTP
429 rate limits and retention-window errors), request rate vs provider limits,
cursor continuity, gap alerts, and processed_events deduplication stats.
"""

from __future__ import annotations

import argparse
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
import json
import math
import os
import re
import sys
import time
from typing import Any, Callable
import urllib.error
import urllib.request

DEFAULT_RPC_URL = "https://mainnet.sorobanrpc.com"
DEFAULT_POLL_INTERVAL_S = 6.0  # Matches IndexerPollInterval in apps/api/internal/stellar/indexer.go
DEFAULT_REQUEST_TIMEOUT_S = 8.0  # Matches IndexerRequestTimeout
DEFAULT_COLD_START_OFFSET = 12  # Matches DefaultColdStartOffset in poller.go
APPROX_LEDGER_CLOSE_SECONDS = 5.0
DEFAULT_GAP_ALERT_LEDGERS = 50
DEFAULT_PROVIDER_MAX_RPS = 10.0

_DURATION_PART_RE = re.compile(r"(\d+(?:\.\d+)?)([hms])")


def parse_duration(raw: str) -> float:
    """Parse a human duration such as '24h', '30m', '45s', or '1h30m' into seconds."""
    text = raw.strip().lower()
    if not text:
        raise ValueError("duration must not be empty")
    if text.isdigit():
        return float(text)

    pos = 0
    total = 0.0
    multipliers = {"h": 3600.0, "m": 60.0, "s": 1.0}
    for match in _DURATION_PART_RE.finditer(text):
        if match.start() != pos:
            raise ValueError(f"invalid duration format: {raw!r}")
        total += float(match.group(1)) * multipliers[match.group(2)]
        pos = match.end()

    if pos != len(text) or total <= 0:
        raise ValueError(f"invalid duration format: {raw!r}")
    return total


def percentile(values: list[float], pct: float) -> float:
    """Compute linear-interpolated percentile (0-100). Returns 0.0 for empty input."""
    if not values:
        return 0.0
    ordered = sorted(values)
    if len(ordered) == 1:
        return float(ordered[0])
    rank = (pct / 100.0) * (len(ordered) - 1)
    lo = math.floor(rank)
    hi = math.ceil(rank)
    if lo == hi:
        return float(ordered[lo])
    frac = rank - lo
    return float(ordered[lo] + (ordered[hi] - ordered[lo]) * frac)


@dataclass
class RPCResponse:
    result: dict[str, Any] | None = None
    status_code: int = 0
    latency_ms: float = 0.0
    error_kind: str = ""
    error_message: str = ""

    @property
    def ok(self) -> bool:
        return self.error_kind == "" and self.result is not None


def classify_rpc_error(message: str, status_code: int = 0) -> str:
    lower = message.lower()
    if status_code == 429 or "rate limit" in lower or "too many requests" in lower:
        return "rate_limit"
    if (
        "retention" in lower
        or "startledger" in lower
        or "start ledger" in lower
        or "ledger is older" in lower
        or "oldest ledger" in lower
        or "out of range" in lower
    ):
        return "retention"
    if "timed out" in lower or "timeout" in lower:
        return "timeout"
    if status_code >= 400:
        return "http_error"
    return "rpc_error"


class SorobanRPCClient:
    """Minimal read-only Soroban JSON-RPC client using Python stdlib."""

    def __init__(self, rpc_url: str, timeout_s: float = DEFAULT_REQUEST_TIMEOUT_S) -> None:
        self.rpc_url = rpc_url
        self.timeout_s = timeout_s
        self._req_id = 0

    def call(self, method: str, params: dict[str, Any] | None = None) -> RPCResponse:
        self._req_id += 1
        payload: dict[str, Any] = {
            "jsonrpc": "2.0",
            "id": self._req_id,
            "method": method,
        }
        if params is not None:
            payload["params"] = params

        body = json.dumps(payload).encode("utf-8")
        req = urllib.request.Request(
            self.rpc_url,
            data=body,
            headers={
                "Content-Type": "application/json",
                "User-Agent": "nester-mainnet-rpc-soak/1.0",
            },
            method="POST",
        )

        start = time.monotonic()
        try:
            with urllib.request.urlopen(req, timeout=self.timeout_s) as resp:
                latency_ms = (time.monotonic() - start) * 1000.0
                raw = resp.read().decode("utf-8", errors="replace")
                status = getattr(resp, "status", 200)
                data = json.loads(raw)
        except urllib.error.HTTPError as exc:
            latency_ms = (time.monotonic() - start) * 1000.0
            msg = f"HTTP {exc.code}: {exc.reason}"
            return RPCResponse(
                status_code=exc.code,
                latency_ms=latency_ms,
                error_kind=classify_rpc_error(msg, status_code=exc.code),
                error_message=msg,
            )
        except urllib.error.URLError as exc:
            latency_ms = (time.monotonic() - start) * 1000.0
            msg = str(exc.reason)
            kind = "timeout" if "timed out" in msg.lower() else "network_error"
            return RPCResponse(
                latency_ms=latency_ms,
                error_kind=kind,
                error_message=msg,
            )
        except TimeoutError as exc:
            latency_ms = (time.monotonic() - start) * 1000.0
            return RPCResponse(
                latency_ms=latency_ms,
                error_kind="timeout",
                error_message=str(exc) or "request timed out",
            )
        except json.JSONDecodeError as exc:
            latency_ms = (time.monotonic() - start) * 1000.0
            return RPCResponse(
                latency_ms=latency_ms,
                error_kind="rpc_error",
                error_message=f"invalid JSON response: {exc}",
            )

        if "error" in data and data["error"] is not None:
            err_obj = data["error"]
            msg = err_obj.get("message", str(err_obj)) if isinstance(err_obj, dict) else str(err_obj)
            return RPCResponse(
                status_code=status,
                latency_ms=latency_ms,
                error_kind=classify_rpc_error(msg, status_code=status),
                error_message=msg,
            )

        result = data.get("result")
        if not isinstance(result, dict):
            return RPCResponse(
                status_code=status,
                latency_ms=latency_ms,
                error_kind="rpc_error",
                error_message="missing result object in JSON-RPC response",
            )

        return RPCResponse(result=result, status_code=status, latency_ms=latency_ms)


@dataclass
class SoakConfig:
    rpc_url: str = DEFAULT_RPC_URL
    duration_seconds: float = 24 * 3600.0
    poll_interval_seconds: float = DEFAULT_POLL_INTERVAL_S
    request_timeout_seconds: float = DEFAULT_REQUEST_TIMEOUT_S
    cold_start_offset: int = DEFAULT_COLD_START_OFFSET
    gap_alert_ledgers: int = DEFAULT_GAP_ALERT_LEDGERS
    provider_max_rps: float = DEFAULT_PROVIDER_MAX_RPS
    contract_ids: list[str] = field(default_factory=list)
    page_limit: int = 100


@dataclass
class SoakReport:
    rpc_url: str
    started_at: str
    ended_at: str
    duration_seconds: float
    poll_interval_seconds: float
    start_ledger: int
    end_ledger: int
    latest_network_ledger: int
    cursor_advances: int
    cursor_continuous: bool
    total_polls: int
    successful_polls: int
    failed_polls: int
    total_requests: int
    request_rate_rps: float
    request_rate_rpm: float
    provider_max_rps: float
    within_rate_limit: bool
    events_fetched: int
    unique_events_processed: int
    duplicate_events_suppressed: int
    duplicate_applications: int
    lag_ledgers_p50: float
    lag_ledgers_p95: float
    lag_ledgers_p99: float
    lag_ledgers_max: float
    lag_seconds_p50: float
    lag_seconds_p95: float
    lag_seconds_p99: float
    rpc_latency_ms_p50: float
    rpc_latency_ms_p95: float
    rpc_latency_ms_p99: float
    error_counts: dict[str, int]
    gap_alert_count: int
    gap_alerts: list[dict[str, Any]]
    passed: bool
    failure_reasons: list[str]


class SoakRunner:
    """Runs the read-only mainnet RPC poller soak loop and produces a SoakReport."""

    def __init__(
        self,
        config: SoakConfig,
        rpc_client: Any | None = None,
        monotonic_fn: Callable[[], float] = time.monotonic,
        sleep_fn: Callable[[float], None] = time.sleep,
        now_fn: Callable[[], datetime] = lambda: datetime.now(timezone.utc),
    ) -> None:
        self.config = config
        self.rpc = rpc_client or SorobanRPCClient(config.rpc_url, config.request_timeout_seconds)
        self._monotonic = monotonic_fn
        self._sleep = sleep_fn
        self._now = now_fn

    def run(self) -> SoakReport:
        started_at = self._now()
        start_mono = self._monotonic()
        deadline = start_mono + self.config.duration_seconds

        error_counts = {
            "total": 0,
            "rate_limit": 0,
            "retention": 0,
            "timeout": 0,
            "http_error": 0,
            "rpc_error": 0,
            "network_error": 0,
        }
        latencies_ms: list[float] = []
        lag_samples: list[float] = []
        gap_alerts: list[dict[str, Any]] = []
        processed_events: set[str] = set()

        total_requests = 0
        total_polls = 0
        successful_polls = 0
        failed_polls = 0
        events_fetched = 0
        duplicate_events_suppressed = 0
        duplicate_applications = 0

        start_ledger = 0
        cursor = 0
        latest_network_ledger = 0
        cursor_advances = 0
        cursor_regressed = False

        # Cold start: getLatestLedger and rewind by cold_start_offset (never ledger 0).
        total_requests += 1
        latest_resp = self.rpc.call("getLatestLedger")
        if latest_resp.latency_ms > 0:
            latencies_ms.append(latest_resp.latency_ms)

        if latest_resp.ok and latest_resp.result is not None:
            latest_network_ledger = int(latest_resp.result.get("sequence", 0))
            start_ledger = max(1, latest_network_ledger - self.config.cold_start_offset)
            cursor = start_ledger
        else:
            error_counts["total"] += 1
            kind = latest_resp.error_kind or "rpc_error"
            error_counts[kind] = error_counts.get(kind, 0) + 1

        while cursor > 0:
            now_mono = self._monotonic()
            if total_polls > 0 and now_mono >= deadline:
                break

            total_polls += 1
            total_requests += 1

            params: dict[str, Any] = {
                "startLedger": cursor,
                "pagination": {"limit": self.config.page_limit},
            }
            if self.config.contract_ids:
                params["filters"] = [{"type": "contract", "contractIds": self.config.contract_ids}]

            resp = self.rpc.call("getEvents", params)
            if resp.latency_ms > 0:
                latencies_ms.append(resp.latency_ms)

            if not resp.ok or resp.result is None:
                failed_polls += 1
                error_counts["total"] += 1
                kind = resp.error_kind or "rpc_error"
                error_counts[kind] = error_counts.get(kind, 0) + 1
            else:
                successful_polls += 1
                rpc_latest = int(resp.result.get("latestLedger", latest_network_ledger))
                if rpc_latest > latest_network_ledger:
                    latest_network_ledger = rpc_latest

                events = resp.result.get("events") or []
                max_event_ledger = cursor
                for event in events:
                    events_fetched += 1
                    event_id = str(event.get("id", ""))
                    if event_id:
                        if event_id in processed_events:
                            duplicate_events_suppressed += 1
                        else:
                            processed_events.add(event_id)
                    ev_ledger = int(event.get("ledger", 0))
                    if ev_ledger > max_event_ledger:
                        max_event_ledger = ev_ledger

                # Advance cursor monotonically, matching EventPoller semantics.
                next_cursor = max(cursor, max_event_ledger)
                if rpc_latest > next_cursor:
                    next_cursor = rpc_latest

                if next_cursor < cursor:
                    cursor_regressed = True
                elif next_cursor > cursor:
                    gap = next_cursor - cursor
                    if gap > self.config.gap_alert_ledgers:
                        gap_alerts.append(
                            {
                                "from_ledger": cursor,
                                "to_ledger": next_cursor,
                                "gap_ledgers": gap,
                                "timestamp": self._now().isoformat(),
                            }
                        )
                    cursor = next_cursor
                    cursor_advances += 1

                lag = float(max(0, latest_network_ledger - cursor))
                lag_samples.append(lag)

            remaining = deadline - self._monotonic()
            if remaining <= 0:
                break
            self._sleep(min(self.config.poll_interval_seconds, remaining))

        ended_at = self._now()
        elapsed = max(self._monotonic() - start_mono, 1e-6)
        rps = total_requests / elapsed
        rpm = rps * 60.0
        within_rate_limit = rps <= self.config.provider_max_rps

        cursor_continuous = (
            start_ledger > 0
            and not cursor_regressed
            and (cursor > start_ledger or successful_polls > 0)
        )

        lag_p50 = percentile(lag_samples, 50)
        lag_p95 = percentile(lag_samples, 95)
        lag_p99 = percentile(lag_samples, 99)
        lag_max = max(lag_samples) if lag_samples else 0.0

        failure_reasons: list[str] = []
        if successful_polls == 0:
            failure_reasons.append("no successful getEvents polls completed")
        if not cursor_continuous:
            failure_reasons.append("cursor did not advance continuously")
        if duplicate_applications > 0:
            failure_reasons.append(f"detected {duplicate_applications} duplicate event applications")
        if len(gap_alerts) > 0:
            failure_reasons.append(f"{len(gap_alerts)} gap alert(s) fired")
        if not within_rate_limit:
            failure_reasons.append(
                f"request rate {rps:.3f} rps exceeded provider limit {self.config.provider_max_rps:.3f} rps"
            )
        if error_counts["rate_limit"] > 0:
            failure_reasons.append(f"received {error_counts['rate_limit']} rate-limit (429) error(s)")
        if error_counts["retention"] > 0:
            failure_reasons.append(f"received {error_counts['retention']} retention-window error(s)")

        return SoakReport(
            rpc_url=self.config.rpc_url,
            started_at=started_at.isoformat(),
            ended_at=ended_at.isoformat(),
            duration_seconds=round(elapsed, 3),
            poll_interval_seconds=self.config.poll_interval_seconds,
            start_ledger=start_ledger,
            end_ledger=cursor,
            latest_network_ledger=latest_network_ledger,
            cursor_advances=cursor_advances,
            cursor_continuous=cursor_continuous,
            total_polls=total_polls,
            successful_polls=successful_polls,
            failed_polls=failed_polls,
            total_requests=total_requests,
            request_rate_rps=round(rps, 4),
            request_rate_rpm=round(rpm, 2),
            provider_max_rps=self.config.provider_max_rps,
            within_rate_limit=within_rate_limit,
            events_fetched=events_fetched,
            unique_events_processed=len(processed_events),
            duplicate_events_suppressed=duplicate_events_suppressed,
            duplicate_applications=duplicate_applications,
            lag_ledgers_p50=round(lag_p50, 2),
            lag_ledgers_p95=round(lag_p95, 2),
            lag_ledgers_p99=round(lag_p99, 2),
            lag_ledgers_max=round(lag_max, 2),
            lag_seconds_p50=round(lag_p50 * APPROX_LEDGER_CLOSE_SECONDS, 2),
            lag_seconds_p95=round(lag_p95 * APPROX_LEDGER_CLOSE_SECONDS, 2),
            lag_seconds_p99=round(lag_p99 * APPROX_LEDGER_CLOSE_SECONDS, 2),
            rpc_latency_ms_p50=round(percentile(latencies_ms, 50), 2),
            rpc_latency_ms_p95=round(percentile(latencies_ms, 95), 2),
            rpc_latency_ms_p99=round(percentile(latencies_ms, 99), 2),
            error_counts=error_counts,
            gap_alert_count=len(gap_alerts),
            gap_alerts=gap_alerts,
            passed=len(failure_reasons) == 0,
            failure_reasons=failure_reasons,
        )


def render_markdown_report(report: SoakReport) -> str:
    """Render a human-readable Markdown soak report."""
    status = "PASS" if report.passed else "FAIL"
    lines = [
        "# Mainnet RPC Read-Only Poller Soak Report",
        "",
        f"- **Status:** {status}",
        f"- **RPC URL:** `{report.rpc_url}`",
        f"- **Window:** `{report.started_at}` to `{report.ended_at}` ({report.duration_seconds}s)",
        f"- **Poll interval:** {report.poll_interval_seconds}s",
        "",
        "## Summary Metrics",
        "",
        "| Metric | Value |",
        "| --- | ---: |",
        f"| Start ledger | {report.start_ledger} |",
        f"| End ledger | {report.end_ledger} |",
        f"| Latest network ledger | {report.latest_network_ledger} |",
        f"| Cursor advances | {report.cursor_advances} |",
        f"| Cursor continuous | {report.cursor_continuous} |",
        f"| Polls (ok / total) | {report.successful_polls} / {report.total_polls} |",
        f"| Request rate | {report.request_rate_rps} req/s ({report.request_rate_rpm} req/min) |",
        f"| Provider limit | {report.provider_max_rps} req/s (within limit: {report.within_rate_limit}) |",
        f"| Lag p50 / p95 / p99 (ledgers) | {report.lag_ledgers_p50} / {report.lag_ledgers_p95} / {report.lag_ledgers_p99} |",
        f"| Lag p50 / p95 / p99 (approx s) | {report.lag_seconds_p50} / {report.lag_seconds_p95} / {report.lag_seconds_p99} |",
        f"| RPC latency p50 / p95 / p99 (ms) | {report.rpc_latency_ms_p50} / {report.rpc_latency_ms_p95} / {report.rpc_latency_ms_p99} |",
        f"| Events fetched / unique / dup suppressed | {report.events_fetched} / {report.unique_events_processed} / {report.duplicate_events_suppressed} |",
        f"| Duplicate applications | {report.duplicate_applications} |",
        f"| Gap alerts | {report.gap_alert_count} |",
        f"| Total errors | {report.error_counts.get('total', 0)} |",
        f"| Rate-limit (429) errors | {report.error_counts.get('rate_limit', 0)} |",
        f"| Retention-window errors | {report.error_counts.get('retention', 0)} |",
        f"| Timeout errors | {report.error_counts.get('timeout', 0)} |",
    ]
    if report.failure_reasons:
        lines.extend(["", "## Failure Reasons", ""])
        for reason in report.failure_reasons:
            lines.append(f"- {reason}")
    lines.append("")
    return "\n".join(lines)


def build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Run a read-only Soroban mainnet RPC poller soak test and emit a soak report."
    )
    parser.add_argument(
        "--rpc-url",
        default=os.environ.get("STELLAR_RPC_URL", DEFAULT_RPC_URL),
        help=f"Soroban RPC URL (default: {DEFAULT_RPC_URL})",
    )
    parser.add_argument(
        "--duration",
        default="24h",
        help="Soak duration such as 24h, 30m, 60s (default: 24h)",
    )
    parser.add_argument(
        "--poll-interval",
        type=float,
        default=DEFAULT_POLL_INTERVAL_S,
        help=f"Seconds between getEvents polls (default: {DEFAULT_POLL_INTERVAL_S})",
    )
    parser.add_argument(
        "--timeout",
        type=float,
        default=DEFAULT_REQUEST_TIMEOUT_S,
        help=f"Per-request HTTP timeout in seconds (default: {DEFAULT_REQUEST_TIMEOUT_S})",
    )
    parser.add_argument(
        "--cold-start-offset",
        type=int,
        default=DEFAULT_COLD_START_OFFSET,
        help=f"Ledgers below tip to begin polling (default: {DEFAULT_COLD_START_OFFSET})",
    )
    parser.add_argument(
        "--gap-alert-ledgers",
        type=int,
        default=DEFAULT_GAP_ALERT_LEDGERS,
        help=f"Ledger jump threshold that fires a gap alert (default: {DEFAULT_GAP_ALERT_LEDGERS})",
    )
    parser.add_argument(
        "--provider-max-rps",
        type=float,
        default=DEFAULT_PROVIDER_MAX_RPS,
        help=f"Provider request-rate budget in req/s (default: {DEFAULT_PROVIDER_MAX_RPS})",
    )
    parser.add_argument(
        "--contract-ids",
        default="",
        help="Optional comma-separated Soroban contract IDs to filter getEvents",
    )
    parser.add_argument(
        "--output-json",
        default="",
        help="Optional path to write the JSON soak report",
    )
    parser.add_argument(
        "--output-md",
        default="",
        help="Optional path to write the Markdown soak report",
    )
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_arg_parser().parse_args(argv)
    contract_ids = [c.strip() for c in args.contract_ids.split(",") if c.strip()]
    cfg = SoakConfig(
        rpc_url=args.rpc_url,
        duration_seconds=parse_duration(args.duration),
        poll_interval_seconds=args.poll_interval,
        request_timeout_seconds=args.timeout,
        cold_start_offset=args.cold_start_offset,
        gap_alert_ledgers=args.gap_alert_ledgers,
        provider_max_rps=args.provider_max_rps,
        contract_ids=contract_ids,
    )

    report = SoakRunner(cfg).run()
    md = render_markdown_report(report)
    print(md)

    if args.output_json:
        with open(args.output_json, "w", encoding="utf-8") as fh:
            json.dump(asdict(report), fh, indent=2)
    if args.output_md:
        with open(args.output_md, "w", encoding="utf-8") as fh:
            fh.write(md)

    return 0 if report.passed else 1


if __name__ == "__main__":
    sys.exit(main())
