# [DAPP-19] Add error boundaries to all major DApp routes

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Enhancement

## Issue

The Next.js DApp lacks global error boundaries. If a single component crashes (e.g., due to unexpected API response), the entire page goes blank with a confusing error instead of showing a graceful fallback UI.

**Related PRD claims:**
- [DAPP-19] Error boundaries on all major routes
- [E-05] DApp: global error boundary with graceful fallback UI

## Acceptance Criteria

- [ ] Create `components/ErrorBoundary.tsx` React error boundary component
- [ ] Add global error boundary wrapping all routes in `app/layout.tsx`
- [ ] Per-route error boundaries for major sections: /dashboard, /vaults, /offramp, /portfolio, /savings
- [ ] Error fallback UI shows: error message, "Go Home" button, "Report Issue" link
- [ ] Log all errors to monitoring service (e.g., Sentry)
- [ ] Test: intentionally throw error in component → verify fallback UI appears, page remains interactive
- [ ] Verify error boundaries do not suppress console errors or hide security issues

## Implementation

**File:** `apps/dapp/frontend/components/ErrorBoundary.tsx`

```tsx
'use client';

import React from 'react';
import Link from 'next/link';

export class ErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { hasError: boolean; error?: Error }
> {
  constructor(props: { children: React.ReactNode }) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('Error caught by boundary:', error, errorInfo);
    // Send to monitoring service (Sentry, etc.)
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="flex flex-col items-center justify-center min-h-screen gap-4">
          <h1 className="text-2xl font-bold">Something went wrong</h1>
          <p className="text-gray-600">{this.state.error?.message}</p>
          <div className="flex gap-2">
            <Link href="/" className="px-4 py-2 bg-blue-500 text-white rounded">
              Go Home
            </Link>
            <a 
              href="https://github.com/suncrestlabs/nester/issues/new"
              target="_blank"
              className="px-4 py-2 bg-gray-300 rounded"
            >
              Report Issue
            </a>
          </div>
        </div>
      );
    }

    return this.props.children;
  }
}
```

## Testing

- Deploy error boundary
- Intentionally crash a component (throw error)
- Verify fallback UI appears
- Verify page is still interactive

## Evidence References

Once resolved:
- `file: apps/dapp/frontend/components/ErrorBoundary.tsx` (error boundary component)
- `file: apps/dapp/frontend/app/layout.tsx#<lines>` (global wrapper)
- `file: apps/dapp/frontend/app/dashboard/error.tsx` (per-route error)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [E-05], [DAPP-19]
- GitHub issue #1115
