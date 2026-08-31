# [P4-18] Pin and upgrade Anthropic SDK version

**Status:** Open  
**Priority:** Medium  
**Phase:** 4 (AI Intelligence Layer)  
**Type:** Maintenance

## Issue

The Anthropic SDK version is not explicitly pinned in `requirements.txt`. The intelligence service may silently upgrade to a newer SDK version (or Claude model) on dependency updates, potentially introducing breaking changes or behavioral differences.

**Related PRD claims:**
- [P4-18] Upgrade Anthropic SDK / model version (currently 0.42.0 — check for newer Claude models)
- [P-07] Intelligence service Claude model version not pinned

## Acceptance Criteria

- [ ] Pin SDK to specific version in `requirements.txt` (e.g., `anthropic==1.0.0`)
- [ ] Pin Claude model in `config.py` (e.g., `CLAUDE_MODEL=claude-3-5-sonnet-20241022`)
- [ ] Update `.env.example` with both pinned versions
- [ ] Document in `docs/INTELLIGENCE.md` why versions are pinned
- [ ] Test with pinned version: verify all endpoints work
- [ ] On upgrade: test all endpoints, benchmark latency, verify outputs make sense
- [ ] Create GitHub issue for quarterly SDK/model version review

## Implementation

**File:** `apps/intelligence/requirements.txt`

```
anthropic==1.0.0  # Pinned version; update quarterly
```

**File:** `apps/intelligence/app/config.py`

```python
# Pinned Claude model; update quarterly or as new models are released
CLAUDE_MODEL = "claude-3-5-sonnet-20241022"
```

**File:** `.env.example`

```
ANTHROPIC_API_KEY=your-key-here
CLAUDE_MODEL=claude-3-5-sonnet-20241022
ANTHROPIC_SDK_VERSION=1.0.0  # Pinned; see docs/INTELLIGENCE.md
```

## Testing

- Run intelligence service with pinned version
- Test endpoints: `/intelligence/chat`, `/intelligence/ws`, `/intelligence/analyze`
- Verify latency and output quality

## Upgrade Procedure (Quarterly Review)

1. Read Anthropic SDK changelog
2. Test new version/model in staging environment
3. Benchmark latency and token usage
4. Update pins and merge via PR
5. Deploy to production

## Evidence References

Once resolved:
- `file: apps/intelligence/requirements.txt#<lines>` (pinned SDK)
- `file: apps/intelligence/app/config.py#<lines>` (pinned model)
- `file: .env.example#<lines>` (versions documented)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [P-07], [P4-18]
- GitHub issue #1115
