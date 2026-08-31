#!/usr/bin/env python3
"""Regression tests for scripts/validate_vulnignore.py.

Run with: python3 scripts/test_validate_vulnignore.py
"""

import os
import subprocess
import sys
import tempfile
from datetime import date, timedelta
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "validate_vulnignore.py"

HEADER = """// This file tracks known vulnerabilities that are accepted or mitigated.
// Review date format: YYYY-MM-DD
"""


def run_validator(text: str):
    with tempfile.TemporaryDirectory() as tmp:
        path = Path(tmp) / ".vulnignore"
        path.write_text(text)
        proc = subprocess.run(
            [sys.executable, str(SCRIPT), str(path)],
            capture_output=True,
            text=True,
            env=dict(os.environ),
        )
        return proc.returncode, proc.stdout, proc.stderr


def valid_block(vid: str, days_from_today: int = 365, owner: str = "security-team",
                status: str = "Accepted - waiting for upstream fix",
                justification: str = "// Justification:\n// - Exploit not reachable\n"):
    review = (date.today() + timedelta(days=days_from_today)).isoformat()
    return (
        f"// {vid}: some advisory\n"
        f"{justification}"
        f"// Review date: {review}\n"
        f"// Owner: {owner}\n"
        f"// Status: {status}\n"
        f"{vid}\n"
    )


def test_valid_waivers_pass():
    text = HEADER + valid_block("GO-2026-4316") + "\n" + valid_block("GO-2026-5037")
    rc, out, err = run_validator(text)
    assert rc == 0, err
    assert "GO-2026-4316" in out and "GO-2026-5037" in out


def test_ownerless_appended_id_fails():
    # Regression: an ID appended at EOF without its own metadata block must
    # NOT inherit metadata from the previous entry.
    text = HEADER + valid_block("GO-2026-4316") + "\nGO-2026-5037\n"
    rc, out, err = run_validator(text)
    assert rc == 1, "ownerless appended ID must fail"
    assert "GO-2026-5037" in err
    assert "empty" in err


def test_missing_justification_fails():
    text = HEADER + valid_block("GO-2026-4316", justification="// No justification\n")
    rc, out, err = run_validator(text)
    assert rc == 1, "missing Justification must fail"
    assert "Justification" in err


def test_missing_owner_fails():
    text = HEADER + valid_block("GO-2026-4316", owner="")
    rc, out, err = run_validator(text)
    assert rc == 1
    assert "Owner" in err


def test_expired_review_date_fails():
    text = HEADER + valid_block("GO-2026-4316", days_from_today=-1)
    rc, out, err = run_validator(text)
    assert rc == 1
    assert "expired" in err


def test_invalid_review_date_fails():
    # ISO-shaped but impossible date: matches the regex, fails fromisoformat.
    block = valid_block("GO-2026-4316")
    block = block.replace("// Review date: ", "// Review date: 2026-13-45\n//")
    text = HEADER + block
    rc, out, err = run_validator(text)
    assert rc == 1
    assert "invalid review date" in err


def test_malformed_date_is_rejected():
    # Not even ISO-shaped: reported as missing review date.
    block = valid_block("GO-2026-4316")
    block = block.replace("// Review date: ", "// Review date: not-a-date\n//")
    text = HEADER + block
    rc, out, err = run_validator(text)
    assert rc == 1
    assert "missing required 'Review date" in err


def main():
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for t in tests:
        t()
        print(f"ok  {t.__name__}")
    print(f"\n{len(tests)} tests passed")


if __name__ == "__main__":
    main()
