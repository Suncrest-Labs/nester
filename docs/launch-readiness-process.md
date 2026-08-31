# Launch Readiness Process

> **How to maintain the docs/launch-readiness-register.md and keep PRD claims verified**

---

## Overview

The `docs/launch-readiness-register.md` is a living document that tracks the implementation status of all Nester product requirements. Unlike the stale `PRD.md`, the register is kept current through a simple discipline: every code change, merged PR, or deployment must be reflected in the register with evidence references.

This document describes:
1. **Who updates the register** (everyone on core team)
2. **When to update** (on every PR merge, hotfix, or design change)
3. **How to add evidence** (format, where to find it)
4. **Automation** (verification script and tests)
5. **Release gates** (checks before major releases)

---

## Rule: Evidence Over Assertion

> **An entry may be marked `Resolved` only when it has an attached evidence reference.**

This is the core discipline. No claim is considered done until it's backed by:
- A test that passes and is checked into source control
- Production code with a file path and line range
- A migration file with a specific ID
- A merged PR number or commit hash
- A CI job name that verifies the claim

**Example (Good):**
```
- **Status:** Resolved
- **Evidence:** test: apps/api/internal/service/auth_service_test.go::TestAuthService_GenerateChallenge
- **Notes:** Challenge generation tested and passing
```

**Example (Bad):**
```
- **Status:** Resolved
- **Evidence:** —
- **Notes:** We implemented this somewhere
```

---

## Who Updates the Register

**Everyone on the core team.**

- **PR authors**: After merging a PR that resolves a register entry, update the register entry with evidence
- **Release engineer**: Before releasing, verify all entries match current codebase state
- **Maintainers**: Review register updates in PR alongside code changes

---

## When to Update the Register

### On Every PR Merge

If your PR resolves or partially implements a PRD claim (i.e., an entry in the register):

1. **Find the entry** in `docs/launch-readiness-register.md` (e.g., `[API-05] Auth — challenge/verify`)
2. **Add evidence reference**:
   - If tests: `test: path/to/file.test.ts::TestName`
   - If code: `file: path/to/file.ts#L10-L50`
   - If migration: `migration: path/to/001_migration.sql`
   - If multiple: separate by commas
3. **Update status**: `Resolved` (if complete), `Needs more info` (if partial), or leave as `Open`
4. **Update notes**: Briefly explain what you did or what's still needed
5. **Commit together**: Include register update in the same PR or a quick follow-up

**Example commit message:**
```
chore: update register for [API-05] auth challenge implementation

Evidence: test: apps/api/internal/service/auth_service_test.go::TestAuthService_GenerateChallenge
Status: Resolved

See docs/launch-readiness-register.md [API-05]
```

### On Design Changes

If a feature is redesigned or behavior changes:

- Update the register entry **Notes** section with explanation
- If evidence becomes invalid, update or remove it
- Change status if appropriate (e.g., from `Resolved` to `Needs more info` if rework is needed)

### On Bug Fixes

If you fix a bug mentioned in the register (e.g., `[B-03] BOLA vulnerability`):

- Mark status `Resolved` with evidence pointing to the fix
- Include PR number in Evidence
- Note the original issue number in Notes

### On Creating New Issues

If you discover a gap or enhancement opportunity:

1. Create a backlog issue file under `docs/backlog/` (see template below)
2. Add a register entry for it
3. Mark status `Open` with link to backlog issue

---

## Evidence Format & Examples

### Evidence Types

#### 1. Test Reference
```
test: apps/api/internal/service/auth_service_test.go::TestAuthService_VerifyAndIssue_Success
```
- **Path**: relative to repo root
- **Test name**: exact function name matching the Go test function (or Jest describe.it)
- **Used for**: proving behavior is correct and won't regress

#### 2. File Reference
```
file: apps/api/internal/handler/vault_handler.go#L42-L120
```
- **Path**: relative to repo root
- **Line range**: optional; use if referencing a specific function
- **Used for**: pointing to implementation

#### 3. Migration Reference
```
migration: apps/api/migrations/009_create_user_roles_table.sql
```
- **Path**: full path to migration file
- **Used for**: schema changes

#### 4. PR Reference
```
pr: #342 (merged) commit: abc1234def567
```
- **PR number**: GitHub PR ID
- **Merged status**: (optional) mention if merged
- **Commit hash**: (optional) short commit hash
- **Used for**: high-level PR linkage; less reliable than test/file/migration

#### 5. CI Job Reference
```
ci: security.yml::gitleaks
```
- **Workflow file**: without .yml extension
- **Job name**: exact name from workflow
- **Used for**: automated checks and scanning

### Multiple Evidence References

If an entry has multiple evidence sources, separate by comma:
```
test: apps/api/internal/service/auth_service_test.go::TestAuthService_GenerateChallenge, 
file: apps/api/internal/service/auth_service.go#L50-L100
```

### Finding Evidence

**For tests:**
- Go: `grep -r "func Test" apps/api/internal/service/` find test names
- TypeScript/Jest: `grep -r "it('" apps/dapp/frontend/` find test names
- Rust: `#[test] fn` in `src/test.rs` files

**For code:**
- Use VS Code "Go to Definition" to find file and line range
- Or: `grep -n "function_name" path/to/file.rs`

**For migrations:**
- List: `ls apps/api/migrations/`
- Verify date created: `ls -la`

**For workflows:**
- List: `ls .github/workflows/`
- Check job names: `grep -A2 "^jobs:" .github/workflows/security.yml`

---

## Updating via Verification Script

Run the verification script to validate all evidence references:

```bash
node scripts/verify-prd-refs.js
```

Output:
```
📊 Summary:
  ✓ Resolved:       92
  ⊘ Open:           44
  ? Needs more info: 1

✅ All entries validated successfully!

📄 Evidence report written to: docs/launch-readiness-evidence.json
```

The JSON report can be consumed by dashboards or CI gates:

```json
{
  "timestamp": "2026-08-27T10:15:30.000Z",
  "totalEntries": 137,
  "summary": {
    "resolved": 92,
    "open": 44,
    "needsMoreInfo": 1
  },
  "issues": [],
  "resolvedWithEvidence": [
    { "id": "API-05", "title": "Auth — challenge/verify", "evidence": "test: ..." }
  ]
}
```

---

## Creating Backlog Issues

When a PRD claim cannot be resolved (no evidence found), create a backlog issue file.

**Location:** `docs/backlog/<slug>.md`

**Naming:** Use slug format (kebab-case): `api-30-db-ping.md`

**Template:**

```markdown
# [ID] Title

**Status:** Open  
**Priority:** Medium | High | Low  
**Phase:** 1–5  
**Type:** Bug | Enhancement | Testing | Security | Documentation

## Issue

Description of the problem or feature gap.

## Acceptance Criteria

- [ ] Criterion 1
- [ ] Criterion 2

## Implementation Notes

(Optional) Technical guidance or code snippets.

## Testing

How to verify this is done.

## Evidence References

Once resolved, add to register:
- `file: ...` (implementation)
- `test: ...` (passing test)
- `pr: #...` (merged PR)

## Related Issues

- PRD claim [ID]
- GitHub issue #1115
```

**Example:** See `docs/backlog/bootstrap-admin-db-ping.md`

---

## Release Gates

### Before Major Release (e.g., mainnet deployment)

1. **Run verification script**: `node scripts/verify-prd-refs.js`
   - All Resolved entries must have valid evidence
   - No "— (" marks in Evidence column
   - JSON report shows 0 issues

2. **Run test suite**: `npm test -- tests/prd-register.test.js`
   - All tests must pass
   - Verify 90+ Resolved entries

3. **Audit evidence freshness**:
   - Spot-check 10 random Resolved entries
   - Verify evidence references still exist (files, tests, migrations)
   - Verify test names haven't changed

4. **Update register summary**:
   - Document final counts (Resolved / Open / Needs more info)
   - Add release date to header

5. **Sign-off**:
   - Core maintainer reviews and approves register
   - PR description includes register summary
   - Merge to main/staging branch

### Quarterly Review

Every quarter (or before major release), re-verify all entries:

1. **Run script**: Catch any broken references
2. **Spot-check**: Manually verify 10–20 entries
3. **Update**: Add new entries for recent features/bugs
4. **Upgrade dependencies**: Pin and test new SDK versions (e.g., Anthropic)

---

## Common Workflows

### Resolving a PRD Claim

**Scenario:** You're implementing [API-05] auth challenge/verify.

1. Create a feature branch: `git checkout -b feat/api-05-auth-challenge`
2. Implement the feature with tests
3. Write test: `TestAuthService_GenerateChallenge` in `apps/api/internal/service/auth_service_test.go`
4. Verify test passes: `go test ./apps/api/internal/service/...`
5. Create PR with title: `feat(api): implement auth challenge/verify`
6. Once PR merges, update register entry [API-05]:
   ```markdown
   - **Status:** Resolved
   - **Evidence:** test: apps/api/internal/service/auth_service_test.go::TestAuthService_GenerateChallenge
   - **Notes:** Challenge generation and signature verification implemented
   ```
7. Commit register update: `git commit -am "chore: mark [API-05] resolved"`
8. Push: `git push origin chore/update-register`

### Creating a Backlog Issue

**Scenario:** You discover [API-30] bootstrap-admin needs db.Ping().

1. Create backlog file: `docs/backlog/bootstrap-admin-db-ping.md`
2. Fill in Issue, Acceptance Criteria, Implementation guidance
3. In register [API-30], update to:
   ```markdown
   - **Status:** Open
   - **Evidence:** —
   - **Notes:** Created issue: `docs/backlog/bootstrap-admin-db-ping.md`
   ```
4. Commit: `git commit -am "docs: add backlog issue for [API-30]"`

### Fixing a Bug in Register

**Scenario:** You fix [B-03] BOLA vulnerability in initiateSettlement.

1. Create branch: `git checkout -b fix/bola-settlement`
2. Fix code: extract user_id from JWT, not request body
3. Write test: `TestInitiateSettlement_BOLA_RejectsOtherUserID`
4. PR: `fix(api): prevent BOLA in initiateSettlement`
5. Once merged, update register [API-26]:
   ```markdown
   - **Status:** Resolved
   - **Evidence:** test: apps/api/internal/handler/settlement_handler_test.go::TestInitiateSettlement_BOLA_RejectsOtherUserID, pr: #342
   - **Notes:** BOLA fixed: user_id now extracted from JWT auth context, not request body
   ```
6. Commit register: `git commit -am "chore: mark [API-26] BOLA fixed (Closes #342)"`

---

## Troubleshooting

### Verification Script Fails: "Evidence references a file that doesn't exist"

**Cause:** File path is wrong or file was deleted/moved.

**Fix:**
1. Run: `find . -name "filename"`
2. Update Evidence with correct path
3. Re-run script

### Test Name Not Found in File

**Cause:** Test name is wrong or test was renamed.

**Fix:**
1. Search file: `grep -n "func Test" path/to/file_test.go`
2. Update Evidence with exact test name
3. Re-run script

### Too Many Open Items

**Cause:** Many features not yet implemented.

**Action:** Prioritize backlog; focus on Critical/High priority items for next release.

### Evidence String Is Too Long

**Cause:** Multiple evidence references for one entry.

**Solution:** Split across multiple lines:
```markdown
- **Evidence:** 
  - test: apps/api/internal/service/auth_service_test.go::TestAuthService_GenerateChallenge
  - file: apps/api/internal/service/auth_service.go#L50-L100
```

---

## Quick Reference

| Task | Command |
|------|---------|
| Verify all entries | `node scripts/verify-prd-refs.js` |
| Run tests | `npm test -- tests/prd-register.test.js` |
| Find test names | `grep -r "func Test" apps/api/` (Go) or `grep -r "it('" apps/dapp/` (JS) |
| Find code line | Use VS Code "Go to Definition" or `grep -n` |
| List migrations | `ls apps/api/migrations/` |
| List workflows | `ls .github/workflows/` |
| View register | `cat docs/launch-readiness-register.md` |
| Create issue | `vim docs/backlog/<slug>.md` |

---

## Questions?

- **What if I can't find evidence for a resolved entry?** Move it back to Open and create a backlog issue.
- **What if an entry becomes partially resolved?** Mark it `Needs more info` and document what's incomplete.
- **What if evidence format is ambiguous?** Use the clearest format; if a file, include line range; if a test, use full path.
- **Who approves evidence?** Core maintainer during PR review; no formal sign-off needed if format is correct.

---

## See Also

- `docs/launch-readiness-register.md` — The register itself
- `scripts/verify-prd-refs.js` — Automated verification
- `tests/prd-register.test.js` — Test suite
- `PRD.md` — Original product requirements (now superseded by register)

