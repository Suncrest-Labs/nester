"""Unit tests for scripts/mainnet-rpc-soak.py."""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
import importlib.util
from pathlib import Path
import sys
import unittest

_SCRIPT_PATH = Path(__file__).resolve().parent / "mainnet-rpc-soak.py"
_SPEC = importlib.util.spec_from_file_location("mainnet_rpc_soak", _SCRIPT_PATH)
assert _SPEC and _SPEC.loader
soak = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = soak
_SPEC.loader.exec_module(soak)


class FakeClock:
    def __init__(self) -> None:
        self.t = 0.0
        self.base = datetime(2026, 11, 20, 0, 0, 0, tzinfo=timezone.utc)

    def monotonic(self) -> float:
        return self.t

    def sleep(self, seconds: float) -> None:
        self.t += max(0.0, seconds)

    def now(self) -> datetime:
        return self.base + timedelta(seconds=self.t)


class FakeRPCClient:
    def __init__(self, responses: list[soak.RPCResponse]) -> None:
        self._responses = list(responses)
        self.calls: list[tuple[str, dict | None]] = []

    def call(self, method: str, params: dict | None = None) -> soak.RPCResponse:
        self.calls.append((method, params))
        if not self._responses:
            return soak.RPCResponse(error_kind="rpc_error", error_message="no more fake responses")
        return self._responses.pop(0)


class TestHelpers(unittest.TestCase):
    def test_parse_duration(self) -> None:
        self.assertEqual(soak.parse_duration("24h"), 86400.0)
        self.assertEqual(soak.parse_duration("1h30m"), 5400.0)
        self.assertEqual(soak.parse_duration("45s"), 45.0)
        self.assertEqual(soak.parse_duration("120"), 120.0)
        with self.assertRaises(ValueError):
            soak.parse_duration("")
        with self.assertRaises(ValueError):
            soak.parse_duration("abc")
        with self.assertRaises(ValueError):
            soak.parse_duration("0s")

    def test_percentile(self) -> None:
        self.assertEqual(soak.percentile([], 50), 0.0)
        self.assertEqual(soak.percentile([42.0], 99), 42.0)
        self.assertAlmostEqual(soak.percentile([1.0, 2.0, 3.0, 4.0, 5.0], 50), 3.0)
        self.assertAlmostEqual(soak.percentile([1.0, 2.0, 3.0, 4.0, 5.0], 99), 4.96)

    def test_classify_rpc_error(self) -> None:
        self.assertEqual(soak.classify_rpc_error("HTTP 429: Too Many Requests", 429), "rate_limit")
        self.assertEqual(
            soak.classify_rpc_error("startLedger is older than oldest ledger in retention"),
            "retention",
        )
        self.assertEqual(soak.classify_rpc_error("request timed out"), "timeout")
        self.assertEqual(soak.classify_rpc_error("HTTP 502: Bad Gateway", 502), "http_error")
        self.assertEqual(soak.classify_rpc_error("internal error"), "rpc_error")


class TestSoakRunner(unittest.TestCase):
    def test_happy_path_continuous_cursor_and_dedup(self) -> None:
        clock = FakeClock()
        rpc = FakeRPCClient(
            [
                # getLatestLedger -> tip = 1000 -> start_ledger = 988
                soak.RPCResponse(result={"sequence": 1000}, status_code=200, latency_ms=45.0),
                # Poll 1
                soak.RPCResponse(
                    result={
                        "latestLedger": 1002,
                        "events": [
                            {"id": "ev-1", "ledger": 995},
                            {"id": "ev-2", "ledger": 1001},
                        ],
                    },
                    status_code=200,
                    latency_ms=60.0,
                ),
                # Poll 2 (includes duplicate ev-2)
                soak.RPCResponse(
                    result={
                        "latestLedger": 1004,
                        "events": [
                            {"id": "ev-2", "ledger": 1001},
                            {"id": "ev-3", "ledger": 1003},
                        ],
                    },
                    status_code=200,
                    latency_ms=55.0,
                ),
                # Poll 3
                soak.RPCResponse(
                    result={"latestLedger": 1006, "events": []},
                    status_code=200,
                    latency_ms=50.0,
                ),
            ]
        )

        cfg = soak.SoakConfig(
            rpc_url="https://mainnet.sorobanrpc.com",
            duration_seconds=15.0,
            poll_interval_seconds=6.0,
            cold_start_offset=12,
            gap_alert_ledgers=50,
            provider_max_rps=10.0,
        )
        report = soak.SoakRunner(
            cfg,
            rpc_client=rpc,
            monotonic_fn=clock.monotonic,
            sleep_fn=clock.sleep,
            now_fn=clock.now,
        ).run()

        self.assertTrue(report.passed, msg=report.failure_reasons)
        self.assertEqual(report.start_ledger, 988)
        self.assertEqual(report.end_ledger, 1006)
        self.assertTrue(report.cursor_continuous)
        self.assertEqual(report.events_fetched, 4)
        self.assertEqual(report.unique_events_processed, 3)
        self.assertEqual(report.duplicate_events_suppressed, 1)
        self.assertEqual(report.duplicate_applications, 0)
        self.assertEqual(report.gap_alert_count, 0)
        self.assertEqual(report.error_counts["total"], 0)
        self.assertTrue(report.within_rate_limit)

        md = soak.render_markdown_report(report)
        self.assertIn("Status:** PASS", md)
        self.assertIn("Lag p50 / p95 / p99", md)
        self.assertIn("Request rate", md)

    def test_gap_alert_and_rate_limit_fail_report(self) -> None:
        clock = FakeClock()
        rpc = FakeRPCClient(
            [
                soak.RPCResponse(result={"sequence": 1000}, status_code=200, latency_ms=40.0),
                # Poll 1: jumps by 100 ledgers (> gap_alert_ledgers=50)
                soak.RPCResponse(
                    result={"latestLedger": 1100, "events": []},
                    status_code=200,
                    latency_ms=70.0,
                ),
                # Poll 2: rate-limited (429)
                soak.RPCResponse(
                    status_code=429,
                    latency_ms=30.0,
                    error_kind="rate_limit",
                    error_message="HTTP 429: Too Many Requests",
                ),
            ]
        )

        cfg = soak.SoakConfig(
            duration_seconds=10.0,
            poll_interval_seconds=6.0,
            gap_alert_ledgers=50,
        )
        report = soak.SoakRunner(
            cfg,
            rpc_client=rpc,
            monotonic_fn=clock.monotonic,
            sleep_fn=clock.sleep,
            now_fn=clock.now,
        ).run()

        self.assertFalse(report.passed)
        self.assertEqual(report.gap_alert_count, 1)
        self.assertEqual(report.error_counts["rate_limit"], 1)
        self.assertEqual(report.error_counts["total"], 1)
        self.assertTrue(any("gap alert" in r for r in report.failure_reasons))
        self.assertTrue(any("rate-limit" in r for r in report.failure_reasons))


if __name__ == "__main__":
    unittest.main()
