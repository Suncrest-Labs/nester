#!/usr/bin/env python3
"""Detect potential npm typosquatting in newly added package dependencies.

Compares the manifest at the base git ref (merge base for PRs, previous
commit for pushes) against the working-tree manifest and checks only
dependencies *added* by the change set, using full scoped package names so
a spoofed ``@attacker/react`` cannot hide behind a name collision with the
unscoped ``react``.

Usage:
    python3 scripts/typosquat_check.py [MANIFEST] [BASE_REF]

Arguments (optional, defaults match the dapp manifest):
    MANIFEST  path to package.json relative to the repo root
              (default: apps/dapp/frontend/package.json)
    BASE_REF  git ref whose copy of MANIFEST is the baseline; if omitted or
              unavailable, the environment variable BASE_REF is used, and if
              that is empty every current dependency is checked.

This is an advisory heuristic: it always exits 0 so a fuzzy name-similarity
match can never block an unrelated PR. Suspects are reported as GitHub
Actions workflow warnings (``::warning::``).
"""

import json
import os
import subprocess
import sys
from pathlib import Path

DEFAULT_MANIFEST = "apps/dapp/frontend/package.json"

# Curated set of well-known packages this codebase legitimately uses. A new
# dependency whose normalized full name sits within edit distance 2 of one of
# these is flagged for manual review.
KNOWN_PACKAGES = {
    "react", "react-dom", "next", "next/router", "next/navigation",
    "@tanstack/react-query", "framer-motion", "recharts", "zod",
    "react-hook-form", "zustand", "uuid", "date-fns", "ethers",
    "viem", "wagmi", "axios", "lodash",
    "@radix-ui/react-dialog", "@radix-ui/react-dropdown-menu",
    "@radix-ui/react-tabs", "@radix-ui/react-tooltip",
    "@radix-ui/react-popover", "@radix-ui/react-select",
    "tailwindcss", "postcss", "autoprefixer", "typescript",
    "vitest", "eslint", "@typescript-eslint/eslint-plugin",
    "@typescript-eslint/parser", "prettier", "clsx", "tailwind-merge",
    "lucide-react", "class-variance-authority", "@hookform/resolvers",
    "sonner", "react-hot-toast", "next-themes", "@starknet-react/core",
    "@starknet-react/chains", "starknet", "@stellar/stellar-sdk",
    "@blend-capital/blend-sdk",
}

MAX_EDIT_DISTANCE = 2


def levenshtein(a: str, b: str) -> int:
    """Return the Levenshtein edit distance between ``a`` and ``b``."""
    m, n = len(a), len(b)
    dp = [[0] * (n + 1) for _ in range(m + 1)]
    for i in range(m + 1):
        dp[i][0] = i
    for j in range(n + 1):
        dp[0][j] = j
    for i in range(1, m + 1):
        for j in range(1, n + 1):
            dp[i][j] = min(
                dp[i - 1][j] + 1,
                dp[i][j - 1] + 1,
                dp[i - 1][j - 1] + (a[i - 1] != b[j - 1]),
            )
    return dp[m][n]


def dep_names(pkg: dict) -> set:
    """Return dependency names across all dependency categories."""
    names = set()
    for section in ("dependencies", "devDependencies", "optionalDependencies"):
        names |= set(pkg.get(section, {}))
    return names


def load_manifest_at_ref(manifest: str, ref: str) -> dict:
    """Return the manifest at ``ref`` via ``git show`` (empty on any failure)."""
    if not ref:
        return {}
    try:
        out = subprocess.run(
            ["git", "show", f"{ref}:{manifest}"],
            capture_output=True,
            text=True,
            timeout=30,
        )
        if out.returncode != 0:
            return {}
        return json.loads(out.stdout)
    except Exception:
        return {}


def find_suspicious(new_deps: set) -> list:
    """Return [(dep, known, distance)] for deps within edit distance of knowns."""
    suspicious = []
    for dep in sorted(new_deps):
        for known in KNOWN_PACKAGES:
            if dep == known:
                continue
            if len(dep) > 3 and len(known) > 3:
                distance = levenshtein(dep.lower(), known.lower())
                if 0 < distance <= MAX_EDIT_DISTANCE:
                    suspicious.append((dep, known, distance))
    return suspicious


def main(argv: list = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    manifest = argv[0] if argv else DEFAULT_MANIFEST
    base_ref = argv[1] if len(argv) > 1 else os.environ.get("BASE_REF", "")

    head_pkg = json.loads(Path(manifest).read_text())
    base_pkg = load_manifest_at_ref(manifest, base_ref)
    new_deps = dep_names(head_pkg) - dep_names(base_pkg)

    suspicious = find_suspicious(new_deps)
    if suspicious:
        print("::warning::Potential typosquatted dependencies detected:")
        for dep, known, distance in suspicious:
            print(f"  {dep} (edit distance {distance} from {known})")
    else:
        print("No potential typosquats detected")
    return 0


if __name__ == "__main__":
    sys.exit(main())
