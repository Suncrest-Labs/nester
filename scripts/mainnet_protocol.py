#!/usr/bin/env python3
"""Settings and reporting helpers for the mainnet-protocol rehearsal (#1569).

The workflow fetches mainnet's Soroban config settings with
`stellar network settings`, prepares a copy quickstart can apply, checks the
local network's resulting settings, and writes a durable run report.

Subcommands:
  prepare-settings   copy mainnet's ConfigUpgradeSet without entries that
                     describe mainnet-only ledger state
  compare-settings   fail unless the local network's settings match the prepared
                     mainnet set (ignoring the excluded entries)
  report             write the markdown run report recorded as a workflow
                     artifact and the job summary

Keeping this logic in one tested script means the workflow's record and its
pass/fail decision cannot drift from each other.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import sys
from pathlib import Path

# These entries are not network-limit settings: they freeze specific mainnet
# ledger keys and bypass transactions. A local network does not contain those
# keys/hashes, so stellar-core rejects a ConfigUpgradeSet that carries them.
# They are recorded in the report but deliberately not applied.
ENVIRONMENT_SPECIFIC_ENTRIES = ("frozen_ledger_keys", "freeze_bypass_txs")

# Mainnet limits this workflow exists to exercise. Missing entries are reported
# as n/a so a protocol upgrade that renames a setting is visible in the report
# instead of silently disappearing.
KEY_ROWS = (
    ("Contract max size (bytes)", "contract_max_size_bytes"),
    ("Transaction max size (bytes)", "contract_bandwidth_v0.tx_max_size_bytes"),
    ("Ledger max transactions size (bytes)", "contract_bandwidth_v0.ledger_max_txs_size_bytes"),
    ("Transaction max instructions", "contract_compute_v0.tx_max_instructions"),
    ("Ledger max instructions", "contract_compute_v0.ledger_max_instructions"),
    ("Transaction memory limit (bytes)", "contract_compute_v0.tx_memory_limit"),
    ("Transaction max disk read (bytes)", "contract_ledger_cost_v0.tx_max_disk_read_bytes"),
    ("Transaction max disk read (entries)", "contract_ledger_cost_v0.tx_max_disk_read_entries"),
    ("Transaction max write (bytes)", "contract_ledger_cost_v0.tx_max_write_bytes"),
    ("Transaction max write (entries)", "contract_ledger_cost_v0.tx_max_write_ledger_entries"),
    ("Ledger max disk read (bytes)", "contract_ledger_cost_v0.ledger_max_disk_read_bytes"),
    ("Ledger max disk read (entries)", "contract_ledger_cost_v0.ledger_max_disk_read_entries"),
    ("Ledger max write (bytes)", "contract_ledger_cost_v0.ledger_max_write_bytes"),
    ("Ledger max write (entries)", "contract_ledger_cost_v0.ledger_max_write_ledger_entries"),
    ("Transaction max footprint entries", "contract_ledger_cost_ext_v0.tx_max_footprint_entries"),
    ("Transaction max contract events size (bytes)", "contract_events_v0.tx_max_contract_events_size_bytes"),
    ("Ledger max transaction count", "contract_execution_lanes.ledger_max_tx_count"),
    ("Temporary entry min TTL (ledgers)", "state_archival.min_temporary_ttl"),
    ("Persistent entry min TTL (ledgers)", "state_archival.min_persistent_ttl"),
    ("Entry max TTL (ledgers)", "state_archival.max_entry_ttl"),
    ("Max entries to archive", "state_archival.max_entries_to_archive"),
    ("Fee per instructions increment (stroops)", "contract_compute_v0.fee_rate_per_instructions_increment"),
    ("Fee per transaction size KB (stroops)", "contract_bandwidth_v0.fee_tx_size1_kb"),
    ("Fee per disk read KB (stroops)", "contract_ledger_cost_v0.fee_disk_read1_kb"),
    ("Fee per write ledger entry (stroops)", "contract_ledger_cost_v0.fee_write_ledger_entry"),
)

OUTCOME_LABELS = {
    "success": "PASS",
    "failure": "FAIL",
    "cancelled": "CANCELLED",
    "skipped": "SKIPPED",
}


def fail(message: str) -> "NoReturn":
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


def load_settings(path: str | Path) -> dict:
    path = Path(path)
    try:
        data = json.loads(path.read_text())
    except FileNotFoundError:
        fail(f"settings file not found: {path}")
    except json.JSONDecodeError as exc:
        fail(f"settings file is not valid JSON ({path}): {exc}")
    entries = data.get("updated_entry")
    if not isinstance(entries, list):
        fail(f"settings file has no updated_entry list: {path}")
    return data


def flatten(settings: dict) -> dict[str, object]:
    flat: dict[str, object] = {}
    for entry in settings["updated_entry"]:
        if not isinstance(entry, dict) or len(entry) != 1:
            fail(f"unexpected ConfigUpgradeSet entry: {entry!r}")
        key, value = next(iter(entry.items()))
        if key in flat:
            fail(f"duplicate ConfigUpgradeSet entry: {key}")
        flat[key] = value
    return flat


def nested(flat: dict[str, object], dotted: str) -> object:
    value: object = flat
    for part in dotted.split("."):
        if not isinstance(value, dict) or part not in value:
            return None
        value = value[part]
    return value


def display(value: object) -> str:
    if value is None:
        return "n/a"
    if isinstance(value, (dict, list)):
        return json.dumps(value, sort_keys=True)
    return str(value)


def cmd_prepare(args: argparse.Namespace) -> int:
    settings = load_settings(args.input)
    excluded = set(args.exclude)
    kept: list[dict] = []
    removed: list[str] = []
    for entry in settings["updated_entry"]:
        key = next(iter(entry))
        if key in excluded:
            removed.append(key)
        else:
            kept.append(entry)

    if not kept:
        fail("every entry was excluded; refusing to write an empty ConfigUpgradeSet")

    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps({"updated_entry": kept}, indent=2) + "\n")

    print(f"prepared {output} with {len(kept)} of {len(settings['updated_entry'])} entries")
    for key in removed:
        print(f"excluded (mainnet-only ledger state): {key}")
    return 0


def cmd_compare(args: argparse.Namespace) -> int:
    expected = flatten(load_settings(args.expected))
    actual = flatten(load_settings(args.actual))
    ignored = set(args.ignore)

    failures: list[str] = []
    for key, expected_value in expected.items():
        if key in ignored:
            continue
        if key not in actual:
            failures.append(f"missing on local network: {key}")
        elif actual[key] != expected_value:
            failures.append(
                f"mismatch for {key}: expected {display(expected_value)}, got {display(actual[key])}"
            )

    for key, actual_value in actual.items():
        if key in expected or key in ignored:
            continue
        if actual_value in ({}, [], None):
            continue
        failures.append(f"unexpected non-empty local setting: {key} = {display(actual_value)}")

    if failures:
        if not args.quiet:
            for line in failures:
                print(f"FAIL: {line}", file=sys.stderr)
        return 1

    if not args.quiet:
        applied = [key for key in expected if key not in ignored]
        print(f"OK: local settings match mainnet for {len(applied)} applied entries")
        for key in sorted(ignored):
            if key in expected:
                print(f"ignored by design: {key}")
    return 0


def read_optional(path: Path | None) -> str:
    if path is None:
        return ""
    try:
        return path.read_text().rstrip()
    except OSError:
        return ""


def tail(text: str, lines: int = 40) -> str:
    if not text:
        return "_not captured_"
    return "\n".join(text.splitlines()[-lines:])


def env_rows(text: str) -> list[tuple[str, str]]:
    rows: list[tuple[str, str]] = []
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        if "CONTRACT_ID" in key:
            rows.append((key, value))
    return rows


def cmd_report(args: argparse.Namespace) -> int:
    settings_error = ""
    try:
        mainnet_settings = load_settings(Path(args.mainnet_settings))
        prepared_settings = load_settings(Path(args.prepared_settings))
    except SystemExit:
        settings_error = "mainnet settings were not captured"
        mainnet_settings = {"updated_entry": []}
        prepared_settings = {"updated_entry": []}

    mainnet_flat = flatten(mainnet_settings)
    prepared_flat = flatten(prepared_settings)

    local_flat: dict[str, object] = {}
    local_error = ""
    try:
        local_flat = flatten(load_settings(Path(args.local_settings)))
    except SystemExit:
        local_error = "local settings were not captured"

    excluded = [key for key in mainnet_flat if key not in prepared_flat]
    deployed_env = read_optional(Path(args.deployed_env) if args.deployed_env else None)
    verification = read_optional(Path(args.verification) if args.verification else None)
    deploy_log = read_optional(Path(args.deploy_log) if args.deploy_log else None)
    integration_log = read_optional(Path(args.integration_log) if args.integration_log else None)
    e2e_log = read_optional(Path(args.e2e_log) if args.e2e_log else None)

    outcomes = {
        "Deploy launch manifest": args.deploy_outcome,
        "Contract integration suite": args.integration_outcome,
        "Dapp mainnet e2e smoke suite": args.e2e_outcome,
    }
    overall = "PASS"
    for outcome in outcomes.values():
        if outcome not in ("success", "skipped"):
            overall = "FAIL"
            break
    if args.job_status == "failure":
        overall = "FAIL"

    settings_ok = not settings_error and not local_error and bool(prepared_flat)
    if settings_ok:
        for key, expected_value in prepared_flat.items():
            if key in ENVIRONMENT_SPECIFIC_ENTRIES:
                continue
            if local_flat.get(key) != expected_value:
                settings_ok = False
                break
    if not settings_ok:
        overall = "FAIL"

    now = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    lines: list[str] = [
        "# Mainnet protocol integration run report",
        "",
        f"Generated: `{now}`  ",
        f"Run: {args.run_url or '_(local run)_'}  ",
        f"Commit: `{args.commit or 'unknown'}`",
        "",
        f"**Overall result: {overall}**",
        "",
        "## Protocol and network",
        "",
        "| Setting | Value |",
        "| --- | --- |",
        f"| Mainnet RPC source | `{args.mainnet_rpc}` |",
        f"| Mainnet passphrase | `{args.mainnet_passphrase}` |",
        f"| Mainnet protocol version | `{args.protocol_version}` |",
        f"| Quickstart image | `{args.quickstart_image}` |",
        f"| Local protocol version | `{args.protocol_version}` (verified against core `/info`) |",
        f"| Local network passphrase | `{args.local_passphrase}` |",
        f"| Local Soroban RPC | `{args.local_rpc}` |",
        f"| Settings profile applied | `mainnet` ({len(prepared_flat)} Soroban config entries) |",
        f"| Excluded mainnet-only entries | {', '.join(f'`{key}`' for key in excluded) or '_none_'} |",
        "",
        "The excluded entries freeze specific mainnet ledger keys and bypass",
        "transactions. They describe mainnet ledger state, not network limits, and",
        "stellar-core rejects them on a fresh network. Every other mainnet config",
        "setting is applied verbatim. Quickstart also sets the protocol version and",
        "the 5 XLM base reserve; both are recorded above.",
        "",
        "## Mainnet limits exercised",
        "",
        "| Limit | Mainnet value | Applied locally |",
        "| --- | --- | --- |",
    ]

    for label, dotted in KEY_ROWS:
        expected = nested(mainnet_flat, dotted)
        actual = nested(local_flat, dotted) if local_flat else None
        lines.append(f"| {label} | `{display(expected)}` | `{display(actual)}` |")

    lines.extend(["", "## Settings verification", ""])
    if settings_error:
        lines.append(f"FAIL: {settings_error}.")
    elif local_error:
        lines.append(f"FAIL: {local_error}.")
    else:
        mismatches = [
            key
            for key, expected_value in prepared_flat.items()
            if key not in ENVIRONMENT_SPECIFIC_ENTRIES and local_flat.get(key) != expected_value
        ]
        if mismatches:
            lines.append(
                "FAIL: local settings differ from the applied mainnet settings for: "
                + ", ".join(f"`{item}`" for item in mismatches)
            )
        else:
            lines.append(
                "PASS: the local network reports the same values as mainnet for all "
                f"{len(prepared_flat)} applied Soroban config entries."
            )

    lines.extend(["", "## Suite results", "", "| Stage | Result |", "| --- | --- |"])
    for stage, outcome in outcomes.items():
        lines.append(f"| {stage} | {OUTCOME_LABELS.get(outcome, outcome)} |")

    lines.extend(["", "## Launch manifest deployed", ""])
    rows = env_rows(deployed_env)
    if rows:
        lines.extend(["| Env key | Contract ID |", "| --- | --- |"])
        for key, value in rows:
            lines.append(f"| `{key}` | `{value}` |")
    else:
        lines.append("_No deployment output was recorded._")

    lines.extend(["", "## Contract verification", "", "```", verification or "_not captured_", "```"])

    lines.extend(
        [
            "",
            "## Logs",
            "",
            "Full logs are attached in the workflow artifact. Tail of each log:",
            "",
            "<details><summary>Deploy</summary>",
            "",
            "```",
            tail(deploy_log),
            "```",
            "",
            "</details>",
            "",
            "<details><summary>Integration suite</summary>",
            "",
            "```",
            tail(integration_log),
            "```",
            "",
            "</details>",
            "",
            "<details><summary>Dapp mainnet e2e smoke suite</summary>",
            "",
            "```",
            tail(e2e_log),
            "```",
            "",
            "</details>",
            "",
            "## Full mainnet ConfigUpgradeSet",
            "",
            "Fetched from mainnet with `stellar network settings --output json-formatted`.",
            "",
            "<details><summary>mainnet-settings.json</summary>",
            "",
            "```json",
            json.dumps(mainnet_settings, indent=2, sort_keys=True),
            "```",
            "",
            "</details>",
            "",
            "## Full deployed env",
            "",
            "<details><summary>deployed-local.env</summary>",
            "",
            "```",
            deployed_env or "_not captured_",
            "```",
            "",
            "</details>",
            "",
        ]
    )

    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text("\n".join(lines) + "\n")
    print(f"wrote {output}")
    return 0 if overall == "PASS" else 1


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)

    prepare = sub.add_parser("prepare-settings", help="strip mainnet-only ledger entries")
    prepare.add_argument("--input", required=True)
    prepare.add_argument("--output", required=True)
    prepare.add_argument("--exclude", action="append", default=list(ENVIRONMENT_SPECIFIC_ENTRIES))
    prepare.set_defaults(func=cmd_prepare)

    compare = sub.add_parser("compare-settings", help="compare local settings with mainnet")
    compare.add_argument("--expected", required=True)
    compare.add_argument("--actual", required=True)
    compare.add_argument("--ignore", action="append", default=list(ENVIRONMENT_SPECIFIC_ENTRIES))
    compare.add_argument("--quiet", action="store_true", help="only report pass/fail through the exit status")
    compare.set_defaults(func=cmd_compare)

    report = sub.add_parser("report", help="write the markdown run report")
    report.add_argument("--output", required=True)
    report.add_argument("--mainnet-settings", required=True)
    report.add_argument("--prepared-settings", required=True)
    report.add_argument("--local-settings", required=True)
    report.add_argument("--deployed-env", default="")
    report.add_argument("--verification", default="")
    report.add_argument("--deploy-log", default="")
    report.add_argument("--integration-log", default="")
    report.add_argument("--e2e-log", default="")
    report.add_argument("--protocol-version", required=True)
    report.add_argument("--quickstart-image", required=True)
    report.add_argument("--mainnet-rpc", required=True)
    report.add_argument("--mainnet-passphrase", required=True)
    report.add_argument("--local-passphrase", required=True)
    report.add_argument("--local-rpc", required=True)
    report.add_argument("--deploy-outcome", default="skipped")
    report.add_argument("--integration-outcome", default="skipped")
    report.add_argument("--e2e-outcome", default="skipped")
    report.add_argument("--job-status", default="")
    report.add_argument("--run-url", default="")
    report.add_argument("--commit", default="")
    report.set_defaults(func=cmd_report)

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
