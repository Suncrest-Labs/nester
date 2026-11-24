#!/usr/bin/env python3
"""Deploy deploy/mainnet/launch-manifest.toml to a local Stellar network.

Used by .github/workflows/mainnet-protocol-integration.yml (nester#1569) after
quickstart has been pinned to mainnet's protocol version and mainnet's Soroban
config settings have been applied. The manifest is deliberately local-only: the
workflow supplies the standalone passphrase and the quickstart RPC URL.

The script deploys every contract in manifest order, initialises them in the
order the manifest lists, then runs the manifest's read-only `verify` calls.
Contract IDs, WASM hashes and the verification transcript are written out so the
workflow's run report can record exactly what the suite ran against.

Usage:
  scripts/deploy_launch_manifest.py \
    --manifest deploy/mainnet/launch-manifest.toml \
    --wasm-dir packages/contracts/target/wasm32-unknown-unknown/release \
    --source-account deployer \
    --rpc-url http://localhost:8000/soroban/rpc \
    --network-passphrase "Standalone Network ; February 2017" \
    --admin G... --usdc-sac C... --xlm-sac C... \
    --out .mainnet-protocol/deployed-local.env \
    --verification .mainnet-protocol/deployed-local.verification.txt

Add --dry-run to print the exact stellar-cli commands without sending anything.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import re
import subprocess
import sys
import tomllib
from pathlib import Path

CONTRACT_ID_RE = re.compile(r"\bC[A-Z2-7]{55}\b")
PLACEHOLDER_RE = re.compile(r"\{\{([A-Za-z0-9_]+)\}\}")

# Contract names in the manifest map onto the same NEXT_PUBLIC_* names the dapp
# and deploy-mainnet.sh use, so the generated env file is directly reusable.
ENV_KEYS = {
    "VaultTokenUSDC": "NEXT_PUBLIC_VAULT_TOKEN_CONTRACT_ID",
    "VaultTokenXLM": "NEXT_PUBLIC_VAULT_TOKEN_XLM_CONTRACT_ID",
    "VaultUSDC": "NEXT_PUBLIC_VAULT_CONTRACT_ID",
    "VaultXLM": "NEXT_PUBLIC_VAULT_XLM_CONTRACT_ID",
    "Treasury": "NEXT_PUBLIC_TREASURY_CONTRACT_ID",
    "YieldRegistry": "NEXT_PUBLIC_YIELD_REGISTRY_CONTRACT_ID",
    "AllocationStrategy": "NEXT_PUBLIC_ALLOCATION_STRATEGY_CONTRACT_ID",
    "Nester": "NEXT_PUBLIC_NESTER_CONTRACT_ID",
}


def fail(message: str) -> "NoReturn":
    print(f"ERROR: {message}", file=sys.stderr)
    raise SystemExit(1)


def run(command: list[str], *, dry_run: bool = False) -> str:
    quoted = " ".join(command)
    print(f"+ {quoted}", flush=True)
    if dry_run:
        return ""
    proc = subprocess.run(command, text=True, capture_output=True)
    if proc.stdout:
        print(proc.stdout, end="", flush=True)
    if proc.stderr:
        print(proc.stderr, end="", file=sys.stderr, flush=True)
    if proc.returncode != 0:
        fail(f"command exited {proc.returncode}: {quoted}")
    return proc.stdout + proc.stderr


def load_manifest(path: Path) -> dict:
    with open(path, "rb") as handle:
        return tomllib.load(handle)


def resolve(value: str, values: dict[str, str]) -> str:
    def replace(match: re.Match[str]) -> str:
        key = match.group(1)
        if key not in values:
            fail(f"unknown placeholder {{{{{key}}}}} in {value!r}")
        return values[key]

    return PLACEHOLDER_RE.sub(replace, value)


def validate_manifest(manifest: dict) -> tuple[list[dict], list[dict], list[dict]]:
    contracts = manifest.get("contract", [])
    inits = manifest.get("init", [])
    verifies = manifest.get("verify", [])
    if not isinstance(contracts, list) or not contracts:
        fail("manifest has no [[contract]] entries")

    names: set[str] = set()
    for entry in contracts:
        for field in ("name", "package", "wasm"):
            if not entry.get(field):
                fail(f"contract entry missing {field!r}: {entry}")
        if entry["name"] in names:
            fail(f"duplicate contract name in manifest: {entry['name']}")
        names.add(entry["name"])

    for kind, entries in (("init", inits), ("verify", verifies)):
        for entry in entries:
            contract = entry.get("contract")
            if contract not in names:
                fail(f"[[{kind}]] references unknown contract: {contract!r}")
            if not entry.get("function"):
                fail(f"[[{kind}]] for {contract} has no function")
            if kind == "init" and entry.get("args") is not None and not isinstance(entry["args"], list):
                fail(f"[[init]] args for {contract} must be a list")

    return contracts, inits, verifies


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def optimize_wasm(source: Path, destination: Path, *, dry_run: bool) -> Path:
    if not source.is_file():
        fail(f"WASM not found: {source} (run the contract build first)")
    if destination.is_file() and destination.stat().st_mtime >= source.stat().st_mtime:
        print(f"reusing optimized WASM {destination}", flush=True)
        return destination
    run(
        ["stellar", "contract", "optimize", "--wasm", str(source), "--wasm-out", str(destination)],
        dry_run=dry_run,
    )
    return destination


def parse_contract_id(output: str, name: str) -> str:
    matches = CONTRACT_ID_RE.findall(output)
    if not matches:
        fail(f"could not parse a contract ID for {name} from stellar-cli output")
    # stellar-cli prints progress lines before the contract ID; the last valid
    # C-address is the deployed contract.
    return matches[-1]


def stellar_network_args(args: argparse.Namespace) -> list[str]:
    return [
        "--rpc-url",
        args.rpc_url,
        "--network-passphrase",
        args.network_passphrase,
    ]


def main() -> int:
    parser = argparse.ArgumentParser(
        description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument("--manifest", required=True, type=Path)
    parser.add_argument("--wasm-dir", required=True, type=Path)
    parser.add_argument("--source-account", required=True)
    parser.add_argument("--rpc-url", required=True)
    parser.add_argument("--network-passphrase", required=True)
    parser.add_argument("--admin", required=True)
    parser.add_argument("--usdc-sac", required=True)
    parser.add_argument("--xlm-sac", required=True)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--verification", required=True, type=Path)
    parser.add_argument("--no-optimize", action="store_true")
    parser.add_argument(
        "--max-contract-bytes",
        type=int,
        default=0,
        help="fail before deploying a WASM larger than this (the mainnet contract size limit)",
    )
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    manifest = load_manifest(args.manifest)
    contracts, inits, verifies = validate_manifest(manifest)

    values = {
        "ADMIN": args.admin,
        "USDC_SAC": args.usdc_sac,
        "XLM_SAC": args.xlm_sac,
    }

    optimized: dict[Path, Path] = {}
    contract_ids: dict[str, str] = {}

    print("== Deploying launch manifest ==", flush=True)
    for entry in contracts:
        name = entry["name"]
        source = args.wasm_dir / entry["wasm"]
        if args.no_optimize:
            wasm = source
        else:
            if source not in optimized:
                destination = source.with_name(f"{source.stem}_opt{source.suffix}")
                optimized[source] = optimize_wasm(source, destination, dry_run=args.dry_run)
            wasm = optimized[source]

        size = wasm.stat().st_size
        print(f"== {name} WASM: {size} bytes", flush=True)
        if args.max_contract_bytes and size > args.max_contract_bytes:
            fail(
                f"{name} WASM is {size} bytes, over the {args.max_contract_bytes}-byte "
                "network contract limit. Refusing to deploy a contract the target "
                "network will reject at install time."
            )

        deploy_command = [
            "stellar",
            "contract",
            "deploy",
            "--wasm",
            str(wasm),
            "--source-account",
            args.source_account,
            *stellar_network_args(args),
        ]
        if args.dry_run:
            print(f"+ {' '.join(deploy_command)}", flush=True)
            contract_id = f"<{name}>"
        else:
            contract_id = parse_contract_id(run(deploy_command), name)

        contract_ids[name] = contract_id
        values[name] = contract_id
        print(f"== {name}: {contract_id}", flush=True)

    print("== Initializing launch manifest ==", flush=True)
    for entry in inits:
        name = entry["contract"]
        resolved_args = [resolve(str(value), values) for value in entry.get("args", [])]
        run(
            [
                "stellar",
                "contract",
                "invoke",
                "--id",
                contract_ids[name],
                "--source-account",
                args.source_account,
                *stellar_network_args(args),
                "--",
                entry["function"],
                *resolved_args,
            ],
            dry_run=args.dry_run,
        )
        print(f"== {name}.{entry['function']} initialized", flush=True)

    print("== Verifying launch manifest ==", flush=True)
    verification_lines = [
        f"# generated {dt.datetime.now(dt.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ')}",
    ]
    for entry in verifies:
        name = entry["contract"]
        resolved_args = [resolve(str(value), values) for value in entry.get("args", [])]
        expected = resolve(str(entry["expect"]), values)
        output = run(
            [
                "stellar",
                "contract",
                "invoke",
                "--id",
                contract_ids[name],
                "--source-account",
                args.source_account,
                *stellar_network_args(args),
                "--",
                entry["function"],
                *resolved_args,
            ],
            dry_run=args.dry_run,
        )
        if not args.dry_run and expected not in output:
            fail(
                f"verification failed for {name}.{entry['function']}: "
                f"expected {expected!r} in output, got {output.strip()!r}"
            )
        line = f"PASS {name}.{entry['function']} expected {expected!r}"
        verification_lines.append(line)
        print(line, flush=True)

    args.verification.parent.mkdir(parents=True, exist_ok=True)
    args.verification.write_text("\n".join(verification_lines) + "\n")

    args.out.parent.mkdir(parents=True, exist_ok=True)
    timestamp = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    lines = [
        "# Local launch-manifest deployment (nester#1569).",
        "# Not a mainnet deployment record: these IDs exist only on the",
        "# ephemeral quickstart network used by the protocol rehearsal.",
        f"# generated: {timestamp}",
        f"# manifest: {args.manifest}",
        f"# admin: {args.admin}",
        f"# usdc_sac: {args.usdc_sac}",
        f"# xlm_sac: {args.xlm_sac}",
        "",
        f"LOCAL_RPC_URL={args.rpc_url}",
        f"LOCAL_NETWORK_PASSPHRASE={args.network_passphrase}",
        f"NEXT_PUBLIC_ADMIN_ADDRESS={args.admin}",
        f"NEXT_PUBLIC_USDC_CONTRACT_ID={args.usdc_sac}",
        f"NEXT_PUBLIC_XLM_CONTRACT_ID={args.xlm_sac}",
    ]
    for entry in contracts:
        name = entry["name"]
        env_key = ENV_KEYS.get(name, f"LOCAL_{re.sub(r'(?<!^)(?=[A-Z])', '_', name).upper()}_CONTRACT_ID")
        lines.append(f"{env_key}={contract_ids[name]}")
    lines.extend(
        [
            "",
            "# wasm sha256 (unoptimized release build; optimized files are what deploy used)",
        ]
    )
    hashed: set[Path] = set()
    for entry in contracts:
        source = args.wasm_dir / entry["wasm"]
        if source in hashed or not source.is_file():
            continue
        hashed.add(source)
        lines.append(f"# {entry['wasm']}: {sha256(source)}")
    args.out.write_text("\n".join(lines) + "\n")

    print(f"== Wrote {args.out}", flush=True)
    print(f"== Wrote {args.verification}", flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
