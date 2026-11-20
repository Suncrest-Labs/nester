#!/usr/bin/env python3
"""Fail if contract persistent writes are not followed by TTL renewal."""
from pathlib import Path
import re
import sys

root = Path(__file__).resolve().parents[1]
contract_root = root / "contracts"
violations = []

for path in contract_root.rglob("*/src/lib.rs"):
    if "test_fixtures" in path.parts:
        continue
    lines = path.read_text().splitlines()
    for i, line in enumerate(lines):
        if ".persistent().set(" not in line:
            continue
        window = "\n".join(lines[i:min(len(lines), i + 14)])
        if not re.search(r"\.persistent\(\)\s*\.extend_ttl\s*\(", window):
            violations.append(f"{path.relative_to(root)}:{i + 1}")

if violations:
    print("Persistent storage writes without matching extend_ttl:")
    print("\n".join(violations))
    sys.exit(1)
print("storage TTL write check passed")
