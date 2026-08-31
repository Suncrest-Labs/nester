# [CI-17] Add SAST for TypeScript/Next.js to CI pipeline

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Security/CI

## Issue

The CI pipeline currently lacks static analysis for TypeScript and Next.js code. Go (gosec), Rust (cargo audit), and Python (bandit) have security scanners, but JavaScript/TypeScript has none. This leaves XSS, injection, and Next.js-specific vulnerabilities undetected.

**Related PRD claims:**
- [CI-17] SAST integration for TypeScript/Next.js
- [E-07] Add SAST for TypeScript to CI pipeline

## Acceptance Criteria

- [ ] Add one or more of: eslint-plugin-security, Semgrep (p/typescript, p/react, p/nextjs), or CodeQL (JavaScript/TypeScript)
- [ ] Configure to detect: XSS, injection attacks, unsafe regex, eval, unsafe localStorage, unsafe API use
- [ ] Add GitHub Actions workflow step in `.github/workflows/security.yml`
- [ ] Run on every PR; fail build on High/Critical findings, warn on Medium
- [ ] Document findings in CI logs
- [ ] Add configuration file (`.semgrep.yml` or `.eslintrc.security.js`) with tuned rules
- [ ] Test: verify a known XSS pattern is detected and fails the build

## Recommended Tools

1. **Semgrep** (recommended)
   - Supports TypeScript, React, Next.js specific rules
   - Docker-based; no build dependencies
   - SAST + secrets scanning in one tool

2. **eslint-plugin-security**
   - Lightweight; integrates with existing ESLint setup
   - Good coverage of common JS/TS security issues

3. **CodeQL** (optional, if using GitHub Enterprise)
   - Deep code analysis
   - Works with existing CodeQL setup for Go/Python

## Implementation

**File:** `.github/workflows/security.yml`

```yaml
- name: Run Semgrep (TypeScript/React/Next.js)
  uses: returntocorp/semgrep-action@v1
  with:
    config: >-
      p/typescript
      p/react
      p/nextjs
      p/secrets
    generateSarif: true
    # Fail on high+critical issues
    onFailLevel: high
```

**File:** `.semgrep.yml` (optional tuning)

```yaml
rules:
  - id: no-eval
    pattern: eval(...)
    message: Do not use eval()
    severity: HIGH
  - id: no-dangerous-html
    pattern-either:
      - pattern: |
          dangerouslySetInnerHTML={{__html: ...}}
      - pattern: |
          innerHTML = ...
    message: Use React safely without dangerouslySetInnerHTML
    severity: HIGH
```

## Testing

- Add known vulnerability to test file
- Run SAST → verify it detects the issue
- Fix vulnerability → SAST passes

## Evidence References

Once resolved:
- `file: .github/workflows/security.yml#<lines>` (SAST job added)
- `file: .semgrep.yml` (configuration)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [E-07], [CI-17]
- GitHub issue #1115
