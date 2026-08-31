"""Tests for the synthetic probes (nester#1056).

The safety guards get the most coverage here, deliberately. A bug in probe
rendering produces a wrong graph; a bug in the guards moves real user money.
"""

from __future__ import annotations

import json
import urllib.error

import pytest

import probe as probe_module
from probe import (
    REASON_ASSERTION,
    REASON_CONNECTION,
    REASON_HTTP_4XX,
    REASON_HTTP_5XX,
    REASON_TIMEOUT,
    REASON_UNKNOWN,
    Config,
    ProbeAbort,
    ProbeResult,
    _classify,
    _require_safe_target,
    render,
    select_probes,
)


def make_config(**overrides: object) -> Config:
    base = {
        "api_base_url": "https://staging-api.example.test",
        "intelligence_base_url": "https://staging-intel.example.test",
        "environment": "staging",
        "auth_token": "token",
        "vault_id": "vault-1",
        "allow_mutations": False,
        "probe_amount": "0.01",
        "timeout": 5.0,
    }
    base.update(overrides)
    return Config(**base)  # type: ignore[arg-type]


# ---------------------------------------------------------------------------
# Safety guards
# ---------------------------------------------------------------------------


def test_missing_base_url_aborts() -> None:
    """No default target. Guessing one is how a probe reaches production."""
    with pytest.raises(ProbeAbort, match="PROBE_API_BASE_URL"):
        _require_safe_target(make_config(api_base_url=""))


def test_missing_environment_aborts() -> None:
    with pytest.raises(ProbeAbort, match="PROBE_ENVIRONMENT"):
        _require_safe_target(make_config(environment=""))


@pytest.mark.parametrize("environment", ["production", "PROD", "Live", "mainnet"])
def test_production_environment_names_are_refused(environment: str) -> None:
    with pytest.raises(ProbeAbort, match="production environment"):
        _require_safe_target(make_config(environment=environment))


@pytest.mark.parametrize(
    "url",
    [
        "https://api.nester.io",
        "https://prod-api.example.test",
        "https://api-mainnet.example.test",
        "https://www.nester.io",
    ],
)
def test_production_looking_urls_are_refused(url: str) -> None:
    """The environment label and the URL are set independently.

    The dangerous case is one of them being wrong, so a production-looking URL
    is refused even when the label says staging.
    """
    with pytest.raises(ProbeAbort, match="looks like production"):
        _require_safe_target(make_config(api_base_url=url))


def test_production_looking_intelligence_url_is_refused() -> None:
    """Both URLs are checked, not just the API one."""
    with pytest.raises(ProbeAbort, match="looks like production"):
        _require_safe_target(
            make_config(intelligence_base_url="https://prod-intel.example.test")
        )


def test_safe_staging_target_is_accepted() -> None:
    _require_safe_target(make_config())


def test_mutating_probes_are_skipped_without_opt_in() -> None:
    """Read-only by default. Moving funds is opt-in per environment."""
    selected = select_probes(make_config(allow_mutations=False))

    assert "deposit" not in selected
    assert "withdrawal" not in selected
    assert set(selected) == {"balance", "intelligence"}


def test_mutating_probes_run_when_explicitly_allowed() -> None:
    selected = select_probes(make_config(allow_mutations=True))

    assert set(selected) == {"balance", "intelligence", "deposit", "withdrawal"}


def test_explicitly_requested_mutation_still_requires_opt_in() -> None:
    """Naming the probe on the command line is not consent to move money."""
    selected = select_probes(
        make_config(allow_mutations=False, selected=["deposit", "balance"])
    )

    assert selected == ["balance"]


def test_unknown_probe_name_aborts() -> None:
    with pytest.raises(ProbeAbort, match="Unknown probes"):
        select_probes(make_config(selected=["nonexistent"]))


def test_misconfiguration_exits_two_not_one(monkeypatch: pytest.MonkeyPatch) -> None:
    """Exit 2 for config, 1 for a failed probe.

    A misconfigured probe reporting the same code as a failed one would send
    someone chasing an outage that is not happening.
    """
    monkeypatch.delenv("PROBE_API_BASE_URL", raising=False)
    monkeypatch.setenv("PROBE_ENVIRONMENT", "staging")

    assert probe_module.main([]) == 2


# ---------------------------------------------------------------------------
# Failure classification
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    ("exc", "expected"),
    [
        (urllib.error.HTTPError("u", 503, "x", {}, None), REASON_HTTP_5XX),  # type: ignore[arg-type]
        (urllib.error.HTTPError("u", 404, "x", {}, None), REASON_HTTP_4XX),  # type: ignore[arg-type]
        (urllib.error.URLError(TimeoutError()), REASON_TIMEOUT),
        (urllib.error.URLError("refused"), REASON_CONNECTION),
        (TimeoutError(), REASON_TIMEOUT),
        (AssertionError("missing field"), REASON_ASSERTION),
        (RuntimeError("boom"), REASON_UNKNOWN),
    ],
)
def test_classify_maps_to_bounded_reasons(exc: BaseException, expected: str) -> None:
    reason, _ = _classify(exc)

    assert reason == expected


def test_classify_reason_is_always_from_the_closed_set() -> None:
    """The reason becomes a Prometheus label, so it must never be free text."""
    allowed = {
        REASON_TIMEOUT,
        REASON_CONNECTION,
        REASON_HTTP_5XX,
        REASON_HTTP_4XX,
        probe_module.REASON_BAD_PAYLOAD,
        REASON_ASSERTION,
        REASON_UNKNOWN,
    }

    for exc in [
        RuntimeError("a" * 5000),
        ValueError("vault 7f3a-secret amount 1234.56"),
        json.JSONDecodeError("bad", "{}", 0),
    ]:
        reason, _ = _classify(exc)
        assert reason in allowed


def test_detail_is_truncated_and_never_a_label() -> None:
    """Detail can carry sensitive text, so it is bounded and log-only."""
    _, detail = _classify(RuntimeError("x" * 5000))

    assert len(detail) <= 200

    rendered = render(
        [
            ProbeResult(
                name="balance",
                success=False,
                duration_seconds=1.0,
                reason=REASON_UNKNOWN,
                timestamp=1.0,
                detail="vault 7f3a amount 1234.56",
            )
        ],
        "staging",
    )

    assert "7f3a" not in rendered
    assert "1234.56" not in rendered


# ---------------------------------------------------------------------------
# Exposition
# ---------------------------------------------------------------------------


def test_render_emits_every_required_signal() -> None:
    """The issue requires success, latency, reason, and timestamp per probe."""
    rendered = render(
        [
            ProbeResult("balance", True, 0.25, "ok", 1700000000.0),
            ProbeResult("deposit", False, 12.5, REASON_HTTP_5XX, 1700000001.0),
        ],
        "staging",
    )

    assert 'nester_probe_success{probe="balance",environment="staging"} 1' in rendered
    assert 'nester_probe_success{probe="deposit",environment="staging"} 0' in rendered
    assert 'nester_probe_duration_seconds{probe="balance",environment="staging"} 0.250000' in rendered
    assert 'nester_probe_last_reason{probe="deposit",reason="http_5xx",environment="staging"} 1' in rendered
    assert "nester_probe_last_run_timestamp_seconds" in rendered


def test_render_labels_are_bounded() -> None:
    """No amount, vault ID, or account identifier reaches a label."""
    rendered = render([ProbeResult("balance", True, 0.1, "ok", 1.0)], "staging")

    for line in rendered.splitlines():
        if line.startswith("#") or not line.strip():
            continue
        label_section = line[line.index("{") + 1 : line.index("}")]
        keys = {pair.split("=")[0] for pair in label_section.split(",")}
        assert keys <= {"probe", "reason", "environment"}


def test_render_is_valid_exposition_shape() -> None:
    rendered = render([ProbeResult("balance", True, 0.1, "ok", 1.0)], "staging")

    assert rendered.endswith("\n")
    assert rendered.count("# TYPE") == 4
    assert rendered.count("# HELP") == 4
