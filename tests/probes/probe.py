#!/usr/bin/env python3
"""Synthetic probes against staging (nester#1056).

Real-traffic SLIs cannot detect a flow that is broken while nobody is using
it. That is the failure the issue calls out — "broken but generating no
traffic ... nobody notices because nobody tried" — and it is the specific gap
these probes fill. They exercise deposit, withdrawal, balance read, and an
intelligence query on a schedule, so a broken path is discovered by us rather
than by the first user who tries it on Monday morning.

Safety
------
These probes move money on whatever environment they point at. Three
independent guards keep that from ever being production:

1. ``PROBE_API_BASE_URL`` has no default. An unset value aborts; there is no
   fallback to localhost and none to a deployed host. The load-soak workflow
   establishes this convention for exactly the same reason.

2. ``PROBE_ENVIRONMENT`` must be a non-production value and is checked against
   a deny list. A URL that looks production-like is refused even when the
   environment name says otherwise, because the two are set independently and
   the dangerous case is one of them being wrong.

3. Deposit and withdrawal probes require ``PROBE_ALLOW_MUTATIONS=true``. The
   default run is read-only: balance and intelligence only. Moving funds is
   opt-in per environment rather than implied by running the script.

The probes use ordinary API credentials for a dedicated staging account. They
are not privileged and cannot reach another user's vault.

Output
------
Each probe emits Prometheus exposition to ``--output``, for a Pushgateway or a
node_exporter textfile collector. Every probe reports success, latency, a
bounded failure reason, and a completion timestamp, per the issue.

The failure reason is a closed enum, never an exception string: an exception
message can contain a vault ID, an amount, or an upstream URL, and a metric
label is the wrong place for any of those.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Callable

# Failure reasons. Closed set: these become a Prometheus label, so an
# unbounded value here is a cardinality incident. Anything unrecognised maps
# to "unknown" and the detail goes to stderr, where it is safe.
REASON_OK = "ok"
REASON_TIMEOUT = "timeout"
REASON_CONNECTION = "connection"
REASON_HTTP_5XX = "http_5xx"
REASON_HTTP_4XX = "http_4xx"
REASON_BAD_PAYLOAD = "bad_payload"
REASON_ASSERTION = "assertion"
REASON_UNKNOWN = "unknown"

# Environment names that must never run a probe that moves money.
FORBIDDEN_ENVIRONMENTS = {"production", "prod", "live", "mainnet"}

# Substrings that suggest a production host even if PROBE_ENVIRONMENT lies.
# Deliberately broad: a false refusal costs a config change, a false
# acceptance moves real user funds.
FORBIDDEN_URL_MARKERS = ("prod", "mainnet", "api.nester.", "www.nester.")

DEFAULT_TIMEOUT = 30.0


@dataclass
class ProbeResult:
    name: str
    success: bool
    duration_seconds: float
    reason: str
    timestamp: float
    # Free-form detail for the log only. Never becomes a metric label.
    detail: str = ""


@dataclass
class Config:
    api_base_url: str
    intelligence_base_url: str
    environment: str
    auth_token: str
    vault_id: str
    allow_mutations: bool
    probe_amount: str
    timeout: float
    intelligence_token: str = ""
    selected: list[str] = field(default_factory=list)


class ProbeAbort(Exception):
    """Configuration is unsafe or incomplete. Never caught as a probe failure.

    A misconfigured probe must not report a *failed* probe: that would look
    like a broken service and send someone chasing an outage that is not
    happening. It aborts loudly instead.
    """


def _require_safe_target(config: Config) -> None:
    if not config.api_base_url:
        raise ProbeAbort(
            "PROBE_API_BASE_URL is not set. Refusing to guess a target: "
            "defaulting to localhost or to a deployed host is how a probe "
            "ends up moving real money."
        )

    environment = config.environment.strip().lower()
    if not environment:
        raise ProbeAbort("PROBE_ENVIRONMENT is not set. Refusing to run against an unlabelled target.")

    if environment in FORBIDDEN_ENVIRONMENTS:
        raise ProbeAbort(
            f"PROBE_ENVIRONMENT={environment!r} is a production environment. "
            "These probes create real transactions and must never run there."
        )

    haystack = f"{config.api_base_url} {config.intelligence_base_url}".lower()
    for marker in FORBIDDEN_URL_MARKERS:
        if marker in haystack:
            raise ProbeAbort(
                f"Target URL contains {marker!r}, which looks like production. "
                "PROBE_ENVIRONMENT and the URL are set independently, so this "
                "check exists for the case where one of them is wrong."
            )


def _classify(exc: BaseException) -> tuple[str, str]:
    """Map an exception to a bounded reason plus unbounded detail."""
    if isinstance(exc, urllib.error.HTTPError):
        if exc.code >= 500:
            return REASON_HTTP_5XX, f"HTTP {exc.code}"
        return REASON_HTTP_4XX, f"HTTP {exc.code}"

    if isinstance(exc, TimeoutError):
        return REASON_TIMEOUT, "timed out"

    if isinstance(exc, urllib.error.URLError):
        # URLError wraps socket timeouts too, which is why this is checked
        # after HTTPError (a subclass) and alongside the reason string.
        if isinstance(exc.reason, TimeoutError):
            return REASON_TIMEOUT, "timed out"
        return REASON_CONNECTION, str(exc.reason)[:200]

    if isinstance(exc, (json.JSONDecodeError, ValueError)):
        return REASON_BAD_PAYLOAD, str(exc)[:200]

    if isinstance(exc, AssertionError):
        return REASON_ASSERTION, str(exc)[:200]

    return REASON_UNKNOWN, f"{type(exc).__name__}: {exc}"[:200]


def _request(
    config: Config,
    method: str,
    url: str,
    *,
    body: dict[str, Any] | None = None,
    token: str | None = None,
) -> Any:
    data = None
    headers = {"Accept": "application/json"}

    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"

    bearer = config.auth_token if token is None else token
    if bearer:
        headers["Authorization"] = f"Bearer {bearer}"

    request = urllib.request.Request(url, data=data, headers=headers, method=method)

    # nosec B310: the scheme is validated below, and the URL comes from
    # environment configuration rather than from user input.
    if not url.startswith(("http://", "https://")):
        raise ProbeAbort(f"Refusing to request a non-HTTP URL: {url!r}")

    with urllib.request.urlopen(request, timeout=config.timeout) as response:  # nosec B310
        payload = response.read()

    if not payload:
        return None

    return json.loads(payload)


def _run(name: str, fn: Callable[[], None]) -> ProbeResult:
    started = time.perf_counter()
    try:
        fn()
    except ProbeAbort:
        raise
    except BaseException as exc:  # noqa: BLE001 - every failure is a probe result
        reason, detail = _classify(exc)
        return ProbeResult(
            name=name,
            success=False,
            duration_seconds=time.perf_counter() - started,
            reason=reason,
            timestamp=time.time(),
            detail=detail,
        )

    return ProbeResult(
        name=name,
        success=True,
        duration_seconds=time.perf_counter() - started,
        reason=REASON_OK,
        timestamp=time.time(),
    )


# ---------------------------------------------------------------------------
# The probes
# ---------------------------------------------------------------------------


def probe_balance(config: Config) -> ProbeResult:
    """Read the probe account's vault balance.

    Read-only, so it runs in every environment the probes are pointed at. This
    is the probe that catches "the API is up but the read path is broken",
    which a health endpoint returning 200 will happily hide.
    """

    def check() -> None:
        payload = _request(config, "GET", f"{config.api_base_url}/api/v1/vaults/{config.vault_id}")

        if not isinstance(payload, dict):
            raise AssertionError("vault response was not an object")

        # Assert on shape, not on value: the balance legitimately changes, but
        # a missing field means the contract broke.
        for key in ("id", "current_balance"):
            if key not in payload:
                raise AssertionError(f"vault response missing {key!r}")

    return _run("balance", lambda: check())


def probe_intelligence(config: Config) -> ProbeResult:
    """Ask the intelligence service a grounded question.

    Read-only. A refusal counts as success: the service answering "I don't
    have that in your account data" is the guardrail working, and treating it
    as a probe failure would page on correct behaviour. What is being checked
    is that a response arrives and is well-formed.
    """

    def check() -> None:
        payload = _request(
            config,
            "POST",
            f"{config.intelligence_base_url}/intelligence/chat",
            body={"message": "What is my current savings balance?"},
            token=config.intelligence_token or config.auth_token,
        )

        if payload is None:
            raise AssertionError("intelligence returned an empty body")

    return _run("intelligence", lambda: check())


def probe_deposit(config: Config) -> ProbeResult:
    """Deposit a minimal amount into the probe account's staging vault.

    Mutating: gated on PROBE_ALLOW_MUTATIONS and on the environment checks in
    _require_safe_target. Uses testnet funds in a vault owned by the probe
    account, so the worst case of a stuck probe is a small testnet balance
    drifting — never a real user's money.
    """

    def check() -> None:
        payload = _request(
            config,
            "POST",
            f"{config.api_base_url}/api/v1/vaults/{config.vault_id}/deposits",
            body={"amount": config.probe_amount},
        )

        if not isinstance(payload, dict):
            raise AssertionError("deposit response was not an object")

    return _run("deposit", lambda: check())


def probe_withdrawal(config: Config) -> ProbeResult:
    """Withdraw the same minimal amount back out.

    Runs after the deposit probe so the two are balance-neutral over a cycle.
    Ordering matters: withdrawing first would fail on an empty vault and
    report an outage that is really just probe sequencing.
    """

    def check() -> None:
        payload = _request(
            config,
            "POST",
            f"{config.api_base_url}/api/v1/vaults/{config.vault_id}/withdrawals",
            body={"amount": config.probe_amount},
        )

        if not isinstance(payload, dict):
            raise AssertionError("withdrawal response was not an object")

    return _run("withdrawal", lambda: check())


READ_ONLY_PROBES: dict[str, Callable[[Config], ProbeResult]] = {
    "balance": probe_balance,
    "intelligence": probe_intelligence,
}

MUTATING_PROBES: dict[str, Callable[[Config], ProbeResult]] = {
    "deposit": probe_deposit,
    "withdrawal": probe_withdrawal,
}

ALL_PROBES = {**READ_ONLY_PROBES, **MUTATING_PROBES}


def render(results: list[ProbeResult], environment: str) -> str:
    """Render results as Prometheus exposition.

    Labels are `probe`, `reason`, and `environment` — all closed sets. The
    probe's detail string is deliberately absent: it can contain an upstream
    URL or an identifier, and a label is the wrong place for either.
    """
    lines = [
        "# HELP nester_probe_success Whether the last synthetic probe run succeeded.",
        "# TYPE nester_probe_success gauge",
    ]
    for result in results:
        lines.append(
            f'nester_probe_success{{probe="{result.name}",environment="{environment}"}} '
            f"{1 if result.success else 0}"
        )

    lines += [
        "# HELP nester_probe_duration_seconds Duration of the last synthetic probe run.",
        "# TYPE nester_probe_duration_seconds gauge",
    ]
    for result in results:
        lines.append(
            f'nester_probe_duration_seconds{{probe="{result.name}",environment="{environment}"}} '
            f"{result.duration_seconds:.6f}"
        )

    lines += [
        "# HELP nester_probe_last_run_timestamp_seconds Unix time of the last probe run.",
        "# TYPE nester_probe_last_run_timestamp_seconds gauge",
    ]
    for result in results:
        lines.append(
            f'nester_probe_last_run_timestamp_seconds{{probe="{result.name}",environment="{environment}"}} '
            f"{result.timestamp:.3f}"
        )

    # Reason is emitted as a gauge per (probe, reason) rather than as a label
    # on the success gauge, so that a reason changing between runs does not
    # leave a stale series claiming the old failure is still current.
    lines += [
        "# HELP nester_probe_last_reason Last outcome reason, 1 for the current reason.",
        "# TYPE nester_probe_last_reason gauge",
    ]
    for result in results:
        lines.append(
            f'nester_probe_last_reason{{probe="{result.name}",'
            f'reason="{result.reason}",environment="{environment}"}} 1'
        )

    return "\n".join(lines) + "\n"


def build_config(args: argparse.Namespace) -> Config:
    return Config(
        api_base_url=os.environ.get("PROBE_API_BASE_URL", "").rstrip("/"),
        intelligence_base_url=os.environ.get("PROBE_INTELLIGENCE_BASE_URL", "").rstrip("/"),
        environment=os.environ.get("PROBE_ENVIRONMENT", ""),
        auth_token=os.environ.get("PROBE_AUTH_TOKEN", ""),
        intelligence_token=os.environ.get("PROBE_INTELLIGENCE_TOKEN", ""),
        vault_id=os.environ.get("PROBE_VAULT_ID", ""),
        allow_mutations=os.environ.get("PROBE_ALLOW_MUTATIONS", "").lower() == "true",
        probe_amount=os.environ.get("PROBE_AMOUNT", "0.01"),
        timeout=float(os.environ.get("PROBE_TIMEOUT_SECONDS", DEFAULT_TIMEOUT)),
        selected=args.probes,
    )


def select_probes(config: Config) -> list[str]:
    if config.selected:
        unknown = sorted(set(config.selected) - set(ALL_PROBES))
        if unknown:
            raise ProbeAbort(f"Unknown probes requested: {', '.join(unknown)}")
        requested = config.selected
    else:
        requested = list(ALL_PROBES)

    selected = []
    for name in requested:
        if name in MUTATING_PROBES and not config.allow_mutations:
            print(
                f"skipping {name}: PROBE_ALLOW_MUTATIONS is not true. "
                "Deposit and withdrawal probes create real transactions and "
                "are opt-in per environment.",
                file=sys.stderr,
            )
            continue
        selected.append(name)

    return selected


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--probes",
        nargs="*",
        default=[],
        help="Probes to run. Defaults to all that the environment permits.",
    )
    parser.add_argument(
        "--output",
        default="",
        help="Write Prometheus exposition here. Defaults to stdout.",
    )
    args = parser.parse_args(argv)

    config = build_config(args)

    try:
        _require_safe_target(config)
        names = select_probes(config)
    except ProbeAbort as exc:
        # Write failure metrics before exiting so alerting can report the
        # misconfiguration.
        #
        # Reported under its own name rather than borrowing "balance": a
        # synthetic balance failure is indistinguishable from a real balance
        # outage on the dashboard, so a misconfigured probe run would page
        # whoever is on call for a problem that does not exist. A distinct
        # series lets the alert rule say "the probes are broken" instead.
        fallback_result = ProbeResult(
            name="probe_configuration",
            success=False,
            duration_seconds=0.0,
            reason="configuration",
            timestamp=time.time(),
            detail=str(exc)[:200],
        )
        exposition = render([fallback_result], config.environment or "unknown")
        if args.output:
            try:
                with open(args.output, "w", encoding="utf-8") as handle:
                    handle.write(exposition)
            except OSError as write_err:
                print(f"Failed to write metrics: {write_err}", file=sys.stderr)
        
        # Exit 2, distinct from a probe failure (1), so a misconfiguration is
        # never mistaken for an outage.
        print(f"probe aborted: {exc}", file=sys.stderr)
        return 2

    if not names:
        print("no probes selected", file=sys.stderr)
        return 0

    results = [ALL_PROBES[name](config) for name in names]

    for result in results:
        status = "ok" if result.success else f"FAIL ({result.reason})"
        suffix = f" - {result.detail}" if result.detail else ""
        print(
            f"{result.name}: {status} in {result.duration_seconds:.3f}s{suffix}",
            file=sys.stderr,
        )

    exposition = render(results, config.environment)
    if args.output:
        with open(args.output, "w", encoding="utf-8") as handle:
            handle.write(exposition)
    else:
        print(exposition, end="")

    # Non-zero when any probe failed, so a scheduler without metrics scraping
    # still notices.
    return 1 if any(not r.success for r in results) else 0


if __name__ == "__main__":
    raise SystemExit(main())
