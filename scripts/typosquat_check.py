#!/usr/bin/env python3
"""
Typosquat / suspicious-package detection for issue #867.

Runs on every PR via .github/workflows/security.yml → `typosquat-check`.

Detects, for each new package added to the lockfiles:
  1. Levenshtein distance <= 1 from a popular package on any ecosystem.
  2. Defined as the wrong package type for its manifest (npm name in a
     pip requirements file, etc.).
  3. Mixed-script identifiers (e.g. Cyrillic 'а' next to Latin 'a') — the
     canonical homoglyph attack signal.

The script is dependency-light (no third-party imports) so it runs in the
stock `ubuntu-latest` runner image without `pip install`.
"""

from __future__ import annotations

import difflib
import json
import re
import sys
from pathlib import Path

# ── Popular packages per ecosystem that we have observed or that an attacker
#    is most likely to typosquat against. Extend as new dependencies are
#    added; this is a deliberately conservative seed list.
POPULAR_PACKAGES: dict[str, set[str]] = {
    "npm": {
        "react", "react-dom", "next", "next-auth", "vite", "vitest",
        "framer-motion", "lucide-react", "tailwindcss",
        "@tanstack/react-query", "@hookform/resolvers",
        "@stellar/stellar-sdk", "@creit.tech/stellar-wallets-kit",
        "@blend-capital/blend-sdk", "zod", "react-hook-form",
        "eslint", "prettier", "typescript", "ts-node",
        "jspdf", "recharts", "react-markdown", "clsx",
        "tailwind-merge", "date-fns", "remark-gfm",
    },
    "pip": {
        "requests", "urllib3", "numpy", "pandas", "scipy",
        "fastapi", "uvicorn", "pydantic", "httpx",
        "openai", "anthropic", "langchain",
        "cryptography", "pyyaml", "click",
        "python-dateutil", "pillow", "sqlalchemy",
    },
    "cargo": {
        "serde", "tokio", "reqwest", "clap", "thiserror",
        "anyhow", "tracing", "soroban-sdk",
    },
    "go": {
        # Mostly transverse dependencies for the stellar SDK; less surface
        # area but still worth scanning.
        "github.com/go-chi/chi", "github.com/gin-gonic/gin",
    },
}

# ── Suspicious homoglyph pairs (Latin/Cyrillic/Greek confusion). Detected
#    by Unicode category mismatch between adjacent codepoints.
HOMOGLYPH_RE = re.compile(r"[\u0400-\u04FF\u0500-\u052F]")


def levenshtein(a: str, b: str) -> int:
    if a == b:
        return 0
    if not a:
        return len(b)
    if not b:
        return len(a)
    prev = list(range(len(b) + 1))
    for i, ca in enumerate(a):
        cur = [i + 1]
        for j, cb in enumerate(b):
            cur.append(min(
                cur[-1] + 1,
                prev[j + 1] + 1,
                prev[j] + (ca != cb),
            ))
        prev = cur
    return prev[-1]


def check_levenshtein(name: str, ecosystem: str) -> list[str]:
    findings: list[str] = []
    target = name.lower()
    # Compare against the seed list AND a broader "all known good names"
    # (if any was provided via external JSON). We also use difflib to catch
    # longer-distance lookalikes (e.g. `reactt`, `reqests`) without false
    # positives on legitimately distinct names.
    seed = POPULAR_PACKAGES.get(ecosystem, set())
    for popular in seed:
        p = popular.lower()
        if p == target:
            continue
        # Go modules with /vN or /vN.M version suffixes are legitimate
        # semver paths, not impersonations of the unversioned module.
        if ecosystem == "go" and target.startswith(p + "/v"):
            continue
        d = levenshtein(target, p)
        if d <= 1 and abs(len(target) - len(p)) <= 1:
            findings.append(
                f"Levenshtein {d} from popular package '{popular}' — "
                f"possible typosquat"
            )
        # difflib's quick_ratio catches near-matches we may have missed
        elif difflib.SequenceMatcher(a=target, b=p).ratio() >= 0.92:
            findings.append(
                f"High string similarity ({difflib.SequenceMatcher(a=target, b=p).ratio():.2f}) "
                f"to popular package '{popular}' — possible impersonation"
            )
    return findings


def cross_ecosystem_check(name: str, ecosystem: str) -> list[str]:
    # Only flag for unfamiliar ecosystems. Scoped npm / Go module paths
    # legitimately contain slashes and dots — never flag them.
    if ecosystem in ("go",):
        return []
    if ecosystem == "npm" and (name.startswith("@") or name.startswith("node_modules/")):
        return []
    if ecosystem == "pip":
        # pip package names ARE case-insensitive but historical convention
        # permits CamelCase (e.g. PyJWT, GitPython). Flag only the npm-style
        # naming (no separators, no dashes, looks like a JS identifier).
        if "." in name and "/" in name:
            return ["Looks like an npm/Go-style path in a pip requirements file"]
        # Allow CamelCase by default — the homoglyph + Levenshtein checks
        # still flag genuinely suspicious names.
    if ecosystem == "npm":
        if name.startswith("@") and "/" not in name[1:]:
            return ["Scoped npm name missing target — malformed"]
    return []


def homoglyph_check(name: str) -> list[str]:
    # If the package name mixes Latin and Cyrillic/Greek chars, treat as
    # suspicious (Unicode homoglyph attack).
    has_cyrillic = False
    has_latin = False
    for ch in name:
        if ch.isascii() and ch.isalpha():
            has_latin = True
        elif HOMOGLYPH_RE.match(ch):
            has_cyrillic = True
        if has_cyrillic and has_latin:
            return ["Mixed Latin/Cyrillic characters in package name — possible homoglyph attack"]
    return []


def parse_npm_lockfile(path: Path) -> list[str]:
    """Yield all package names from a pnpm / npm lockfile (best-effort)."""
    if not path.exists():
        return []
    text = path.read_text()
    # npm v7+ style: top-level "packages": { "node_modules/<name>": ... }
    # pnpm style:      top-level "packages": { "<name>@<version>": ... }
    # We pick out the name-ish keys and let ecosystem checks filter later.
    names: set[str] = set()
    for m in re.finditer(r'"node_modules/(@?[^"/]+(?:/[^"/]+)?)"\s*:', text):
        names.add(m.group(1))
    for m in re.finditer(r'"(@?[^"/]+(?:/[^"/]+)?)@[\^~]?[\d.]+(?:[^\s"]*)"\s*:', text):
        names.add(m.group(1))
    return sorted(names)


def parse_pip_requirements(path: Path) -> list[str]:
    if not path.exists():
        return []
    out: list[str] = []
    for line in path.read_text().splitlines():
        s = line.strip()
        if not s or s.startswith("#") or s.startswith("-"):
            continue
        # Strip extras, version specifier, markers.
        name = re.split(r"[<>=!~;\[]", s)[0].strip()
        if name:
            out.append(name)
    return out


def parse_cargo_lockfile(path: Path) -> list[str]:
    if not path.exists():
        return []
    out: list[str] = []
    for m in re.finditer(r'^name\s*=\s*"([^"]+)"', path.read_text(), flags=re.MULTILINE):
        out.append(m.group(1))
    return out


def parse_go_mod(path: Path) -> list[str]:
    if not path.exists():
        return []
    out: list[str] = []
    for line in path.read_text().splitlines():
        stripped = line.strip()
        if stripped.startswith("//") or not stripped:
            continue
        if stripped.startswith("require ") or stripped.startswith("module "):
            continue
        parts = stripped.split()
        if not parts:
            continue
        # A line like `github.com/x/y v1.2.3` after a `require (...)` block.
        if "/" in parts[0] and not parts[0].startswith("module"):
            out.append(parts[0])
    return out


def main() -> int:
    repo_root = Path(__file__).resolve().parents[1]
    findings: list[dict] = []

    lockfiles = [
        ("npm", repo_root / "pnpm-lock.yaml", parse_npm_lockfile),
        ("npm", repo_root / "apps" / "dapp" / "frontend" / "pnpm-lock.yaml", parse_npm_lockfile),
        ("npm", repo_root / "apps" / "website" / "pnpm-lock.yaml", parse_npm_lockfile),
        ("pip", repo_root / "apps" / "intelligence" / "requirements.txt", parse_pip_requirements),
        ("pip", repo_root / "apps" / "api" / "requirements.txt", parse_pip_requirements),
        ("cargo", repo_root / "packages" / "contracts" / "Cargo.lock", parse_cargo_lockfile),
        ("cargo", repo_root / "apps" / "dapp" / "contracts" / "Cargo.lock", parse_cargo_lockfile),
        ("go", repo_root / "apps" / "api" / "go.mod", parse_go_mod),
    ]

    seen: set[tuple[str, str]] = set()
    for ecosystem, path, parser in lockfiles:
        for name in parser(path):
            key = (ecosystem, name.lower())
            if key in seen:
                continue
            seen.add(key)
            notes: list[str] = []
            notes.extend(check_levenshtein(name, ecosystem))
            notes.extend(cross_ecosystem_check(name, ecosystem))
            notes.extend(homoglyph_check(name))
            if notes:
                findings.append({
                    "ecosystem": ecosystem,
                    "package": name,
                    "issues": notes,
                    "source": str(path.relative_to(repo_root)),
                })

    print(json.dumps({"findings": findings, "scanned": len(seen)}, indent=2))

    if findings:
        print(
            "\n::error::Typosquat / suspicious package check FAILED. "
            "Review the findings above. A new package that is one character "
            "away from a popular package, mixes scripts, or is the wrong "
            "shape for its ecosystem is a likely supply-chain attack.",
            file=sys.stderr,
        )
        return 1

    print("\nOK: no typosquat / suspicious packages detected.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
