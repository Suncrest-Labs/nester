# [DAPP-23] Accessibility audit (WCAG 2.1 AA)

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Audit

## Issue

No formal accessibility audit has been conducted. The DApp may have WCAG 2.1 AA compliance issues that exclude users with disabilities.

**Related PRD claims:**
- [DAPP-23] Accessibility audit (WCAG 2.1 AA)

## Acceptance Criteria

- [ ] Run automated tools: WAVE, Axe DevTools, Lighthouse
- [ ] Conduct manual testing with screen readers: NVDA, JAWS, VoiceOver
- [ ] Test keyboard-only navigation (no mouse)
- [ ] Test color contrast: WCAG AA requires 4.5:1 for text
- [ ] Test focus indicators: all interactive elements must have visible focus
- [ ] Test semantic HTML: proper heading hierarchy, form labels, ARIA attributes
- [ ] Document findings in `docs/ACCESSIBILITY.md`
- [ ] Create GitHub issues for each finding

## Scope

- Dashboard, vault detail, deposit modal, portfolio page
- Navigation, form inputs, modals, notifications
- Dark mode contrast

## Tools

- [Axe DevTools](https://www.deque.com/axe/devtools/) browser extension
- [WAVE](https://wave.webaim.org/) online tool or browser extension
- [Lighthouse](https://developers.google.com/web/tools/lighthouse) (built into Chrome)
- Screen reader simulators or real screen readers

## Manual Testing Checklist

- [ ] Navigate entire DApp using only Tab, Enter, Escape, Arrow keys
- [ ] Verify all form fields have labels
- [ ] Verify all buttons have descriptive text or aria-labels
- [ ] Test with browser zoom at 200%
- [ ] Test with Windows High Contrast mode
- [ ] Test with screen reader (VoiceOver on macOS, NVDA on Windows)

## Evidence References

Once resolved:
- `file: docs/ACCESSIBILITY.md` (audit report)
- `file: .github/issues/` (accessibility issues created)
- `pr: #<number>` (merged PR with fixes)

## Related Issues

- PRD [DAPP-23]
- GitHub issue #1115

## Notes

Full WCAG 2.1 AA validation typically requires professional accessibility review. This audit captures the most critical issues.
