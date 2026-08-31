#!/usr/bin/env python3
"""Regression tests for scripts/typosquat_check.py.

Run with: python3 scripts/test_typosquat_check.py
"""

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

SCRIPT = Path(__file__).resolve().parent / "typosquat_check.py"


def run_check(manifest_text: str, base_text: str = "{}", base_ref: str = "HEAD"):
    """Run typosquat_check.py against temp manifests and capture output."""
    with tempfile.TemporaryDirectory() as tmp:
        repo = Path(tmp)
        (repo / "apps").mkdir(parents=True)
        (repo / "apps" / "dapp").mkdir()
        (repo / "apps" / "dapp" / "frontend").mkdir()
        manifest = repo / "apps" / "dapp" / "frontend" / "package.json"
        manifest.write_text(manifest_text)

        # Fake a git repo so `git show HEAD:...` works.
        subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
        subprocess.run(["git", "config", "user.email", "test@example.com"], cwd=repo, check=True)
        subprocess.run(["git", "config", "user.name", "test"], cwd=repo, check=True)
        (repo / "apps" / "dapp" / "frontend" / "package.json").write_text(base_text)
        subprocess.run(["git", "add", "-A"], cwd=repo, check=True)
        subprocess.run(["git", "commit", "-qm", "base"], cwd=repo, check=True)
        # Head manifest differs from base.
        (repo / "apps" / "dapp" / "frontend" / "package.json").write_text(manifest_text)

        env = dict(os.environ)
        env["BASE_REF"] = base_ref
        proc = subprocess.run(
            [sys.executable, str(SCRIPT), "apps/dapp/frontend/package.json", base_ref],
            cwd=repo,
            capture_output=True,
            text=True,
            env=env,
        )
        return proc.returncode, proc.stdout


def test_no_new_deps_is_clean():
    base = json.dumps({"dependencies": {"react": "^19.0.0"}})
    head = json.dumps({"dependencies": {"react": "^19.0.0"}})
    rc, out = run_check(head, base)
    assert rc == 0, out
    assert "No potential typosquats detected" in out


def test_scoped_close_match_is_detected():
    # Full scoped names are compared, so a close misspelling of a scoped
    # package is caught (e.g. @radix-ui/react-dailog vs react-dialog).
    base = json.dumps({"dependencies": {}})
    head = json.dumps({"dependencies": {"@radix-ui/react-dailog": "^1.0.0"}})
    rc, out = run_check(head, base)
    assert rc == 0, out  # advisory only
    assert "Potential typosquatted dependencies detected" in out
    assert "@radix-ui/react-dailog" in out


def test_unrelated_scoped_name_is_not_flaggged():
    # A different scope is not an edit-distance typosquat of the unscoped
    # package: full-name comparison must not reduce @attacker/react to react.
    base = json.dumps({"dependencies": {}})
    head = json.dumps({"dependencies": {"@attacker/react": "^1.0.0"}})
    rc, out = run_check(head, base)
    assert rc == 0, out
    assert "No potential typosquats detected" in out


def test_close_match_is_detected():
    base = json.dumps({"dependencies": {}})
    head = json.dumps({"dependencies": {"reacct": "^1.0.0"}})
    rc, out = run_check(head, base)
    assert rc == 0, out
    assert "reacct" in out


def test_optional_dependencies_are_checked():
    # optionalDependencies must participate in the new-dep diff.
    base = json.dumps({"dependencies": {}})
    head = json.dumps({"optionalDependencies": {"reacct": "^1.0.0"}})
    rc, out = run_check(head, base)
    assert rc == 0, out
    assert "reacct" in out


def test_existing_dependency_is_not_flagged():
    # Only newly added deps are checked, not the whole manifest.
    base = json.dumps({"dependencies": {"reacct": "^1.0.0"}})
    head = json.dumps({"dependencies": {"reacct": "^1.0.0", "zod": "^3.0.0"}})
    rc, out = run_check(head, base)
    assert rc == 0, out
    assert "No potential typosquats detected" in out


def main():
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for t in tests:
        t()
        print(f"ok  {t.__name__}")
    print(f"\n{len(tests)} tests passed")


if __name__ == "__main__":
    main()
