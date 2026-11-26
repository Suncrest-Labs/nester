#!/usr/bin/env python3
"""Verify that deploy/preprod matches deploy/production's exact infra shape
while remaining strictly isolated from production and staging.

Uses only the Python standard library so it can run in any environment.
"""

from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parent.parent
PROD_K8S = ROOT / "deploy" / "production" / "k8s-production.yaml"
PREPROD_K8S = ROOT / "deploy" / "preprod" / "k8s-preprod.yaml"
PROD_COMPOSE = ROOT / "deploy" / "production" / "docker-compose.production.yml"
PREPROD_COMPOSE = ROOT / "deploy" / "preprod" / "docker-compose.preprod.yml"


def split_yaml_docs(path: Path) -> list[str]:
    text = path.read_text(encoding="utf-8")
    docs = [d.strip() for d in re.split(r"(?m)^---\s*$", text) if d.strip()]
    return docs


def extract_field(doc: str, key: str) -> str | None:
    match = re.search(rf"(?m)^\s*{re.escape(key)}:\s*(.+?)\s*$", doc)
    return match.group(1).strip().strip('"').strip("'") if match else None


def check_k8s_parity(errors: list[str]) -> None:
    prod_docs = split_yaml_docs(PROD_K8S)
    preprod_docs = split_yaml_docs(PREPROD_K8S)

    prod_kinds = [extract_field(d, "kind") for d in prod_docs]
    preprod_kinds = [extract_field(d, "kind") for d in preprod_docs]
    if prod_kinds != preprod_kinds:
        errors.append(
            f"k8s resource kinds mismatch: production={prod_kinds}, preprod={preprod_kinds}"
        )
        return

    for prod, preprod in zip(prod_docs, preprod_docs):
        kind = extract_field(prod, "kind")
        prod_ns = extract_field(prod, "namespace")
        preprod_ns = extract_field(preprod, "namespace")
        if preprod_ns == prod_ns or preprod_ns != "nester-preprod":
            errors.append(
                f"{kind} must use dedicated namespace 'nester-preprod', got {preprod_ns!r}"
            )

        if kind == "Deployment":
            if extract_field(prod, "replicas") != extract_field(preprod, "replicas"):
                errors.append(
                    f"Deployment {extract_field(preprod, 'name')} replicas mismatch: "
                    f"production={extract_field(prod, 'replicas')}, "
                    f"preprod={extract_field(preprod, 'replicas')}"
                )
            if extract_field(prod, "image") != extract_field(preprod, "image"):
                errors.append(
                    f"Deployment {extract_field(preprod, 'name')} image placeholder mismatch"
                )
            for probe in ("readinessProbe", "livenessProbe"):
                if (probe in prod) != (probe in preprod):
                    errors.append(
                        f"Deployment {extract_field(preprod, 'name')} {probe} presence mismatch"
                    )
            if "readinessProbe:" in preprod and "path: /readyz" not in preprod:
                errors.append("API Deployment readinessProbe in preprod must target /readyz")
            if "STELLAR_NETWORK" in preprod and 'value: "testnet"' not in preprod:
                errors.append(
                    f"Deployment {extract_field(preprod, 'name')} must target Stellar testnet"
                )
            if "RUN_MIGRATIONS" in preprod and 'value: "false"' not in preprod:
                errors.append("API Deployment in preprod must set RUN_MIGRATIONS=false")

        elif kind == "Job":
            if extract_field(prod, "backoffLimit") != extract_field(preprod, "backoffLimit"):
                errors.append("Migration Job backoffLimit differs from production")
            if extract_field(prod, "image") != extract_field(preprod, "image"):
                errors.append("Migration Job image placeholder differs from production")
            if 'value: "true"' not in preprod or "RUN_MIGRATIONS" not in preprod:
                errors.append("Migration Job in preprod must set RUN_MIGRATIONS=true")


def extract_compose_services(text: str) -> list[str]:
    in_services = False
    services: list[str] = []
    for line in text.splitlines():
        if re.match(r"^services:\s*$", line):
            in_services = True
            continue
        if in_services and re.match(r"^\S", line):
            break
        if in_services:
            match = re.match(r"^  ([a-zA-Z0-9_-]+):\s*$", line)
            if match:
                services.append(match.group(1))
    return services


def check_compose_parity(errors: list[str]) -> None:
    prod_text = PROD_COMPOSE.read_text(encoding="utf-8")
    preprod_text = PREPROD_COMPOSE.read_text(encoding="utf-8")

    prod_roles = sorted(s.removesuffix("_production") for s in extract_compose_services(prod_text))
    preprod_roles = sorted(s.removesuffix("_preprod") for s in extract_compose_services(preprod_text))
    if prod_roles != preprod_roles:
        errors.append(
            f"docker-compose service roles mismatch: production={prod_roles}, preprod={preprod_roles}"
        )

    if ("ports:" in prod_text) != ("ports:" in preprod_text):
        errors.append("docker-compose port exposure differs from production")

    if "postgres_production_data" in preprod_text:
        errors.append("docker-compose preprod must not share volumes with production")

    if "STELLAR_NETWORK=testnet" not in preprod_text:
        errors.append("docker-compose preprod must target STELLAR_NETWORK=testnet")


def main() -> int:
    errors: list[str] = []
    check_k8s_parity(errors)
    check_compose_parity(errors)

    if errors:
        print("Pre-production parity check FAILED:", file=sys.stderr)
        for err in errors:
            print(f"  - {err}", file=sys.stderr)
        return 1

    print("Pre-production parity check PASSED: deploy/preprod matches deploy/production infra shape.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
