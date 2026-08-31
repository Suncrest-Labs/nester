#!/usr/bin/env python3
"""Validate the vulnerability waivers in ``.vulnignore``.

``.vulnignore`` is the enforcement source of truth for the CI vulnerability
gate: every bare ``GO-YYYY-NNNN`` line is exempted from blocking CI, provided
its waiver block carries a justification, a future review date, an owner, and
a status. This script fails (exit 1) when any waiver is expired, malformed, or
ownerless, or when a vulnerability ID is appended without its own metadata
block (metadata cannot be inherited from a neighbouring entry).

Usage:
    python3 scripts/validate_vulnignore.py [PATH]

Prints the accepted vulnerability IDs (space separated) to stdout on success;
all diagnostics go to stderr as GitHub Actions ``::error::`` annotations.

Exit codes:
    0  all waivers valid
    1  a waiver is missing, malformed, expired, or ownerless
"""

import re
import sys
from datetime import date
from pathlib import Path

VULN_ID_RE = re.compile(r"GO-\d{4}-\d+")
REVIEW_DATE_RE = re.compile(r"Review date:[ \t]*(\d{4}-\d{2}-\d{2})")
OWNER_RE = re.compile(r"Owner:[ \t]+\S+")
STATUS_RE = re.compile(r"Status:[ \t]+\S+")
JUSTIFICATION_RE = re.compile(r"Justification:")


def parse_waiver_blocks(text: str) -> list:
    """Split ``.vulnignore`` into per-vulnerability waiver blocks.

    Each bare ``GO-YYYY-NNNN`` line owns the comment block immediately above
    it. Iterating in order with a running anchor guarantees metadata can never
    be inherited from another entry — an ID appended without its own block
    gets an empty block and fails validation.
    """
    lines = text.splitlines()
    blocks = []
    current = []
    for line in lines:
        stripped = line.strip()
        if VULN_ID_RE.fullmatch(stripped):
            blocks.append((stripped, "\n".join(current)))
            current = []
        else:
            current.append(line)
    return blocks


def validate_blocks(blocks: list, today: date) -> list:
    """Return a list of human-readable errors, one per failing waiver."""
    errors = []
    for vid, block in blocks:
        if not block.strip():
            errors.append(
                f"{vid}: waiver block is empty - add Justification:, "
                "Review date: YYYY-MM-DD, Owner:, and Status: above the ID"
            )
            continue

        if not JUSTIFICATION_RE.search(block):
            errors.append(f"{vid}: missing required 'Justification:'")
        if not OWNER_RE.search(block):
            errors.append(f"{vid}: missing required 'Owner:'")
        if not STATUS_RE.search(block):
            errors.append(f"{vid}: missing required 'Status:'")

        review = REVIEW_DATE_RE.search(block)
        if not review:
            errors.append(f"{vid}: missing required 'Review date: YYYY-MM-DD'")
        else:
            try:
                if date.fromisoformat(review.group(1)) < today:
                    errors.append(
                        f"{vid}: waiver review date {review.group(1)} has "
                        f"expired (today {today.isoformat()})"
                    )
            except ValueError:
                errors.append(f"{vid}: invalid review date {review.group(1)}")
    return errors


def main(argv: list = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    path = Path(argv[0]) if argv else Path(".vulnignore")
    if not path.exists():
        print(f"::error::Missing waiver file: {path}", file=sys.stderr)
        return 1

    blocks = parse_waiver_blocks(path.read_text())
    if not blocks:
        print("::error::No vulnerability waivers found in .vulnignore", file=sys.stderr)
        return 1

    errors = validate_blocks(blocks, today=date.today())
    if errors:
        for err in errors:
            print(f"::error::{err}", file=sys.stderr)
        return 1

    print(" ".join(vid for vid, _ in blocks))
    return 0


if __name__ == "__main__":
    sys.exit(main())
