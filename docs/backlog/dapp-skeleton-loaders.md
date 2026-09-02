# [DAPP-21] Add skeleton loaders to all data-fetching components

**Status:** Open  
**Priority:** Low  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Enhancement

## Issue

Many data-fetching components show blank space while loading, causing layout shift and poor perceived performance. Skeleton loaders (placeholder content that matches final layout) improve UX significantly.

**Related PRD claims:**
- [DAPP-21] Skeleton loaders on all data-fetching components
- [E-04] DApp: skeleton loaders on all data-fetching routes

## Acceptance Criteria

- [ ] Create `components/Skeleton.tsx` with Tailwind-based skeleton component
- [ ] Add skeleton loaders to: VaultList, PortfolioChart, TransactionHistory
- [ ] Skeleton must match final layout dimensions and spacing
- [ ] Use Tailwind `animate-pulse` for loading animation
- [ ] Test: verify no layout shift between skeleton and loaded content

## Implementation

**File:** `apps/dapp/frontend/components/Skeleton.tsx`

```tsx
export const SkeletonRow = ({ cols = 3 }: { cols?: number }) => (
  <div className="flex gap-4 py-4">
    {Array.from({ length: cols }).map((_, i) => (
      <div
        key={i}
        className="h-6 bg-gray-200 rounded animate-pulse flex-1"
      />
    ))}
  </div>
);

export const SkeletonCard = () => (
  <div className="p-4 bg-white rounded-lg border animate-pulse">
    <div className="h-4 bg-gray-200 rounded mb-2 w-3/4" />
    <div className="h-6 bg-gray-200 rounded w-1/2" />
  </div>
);
```

## Testing

- Load page → skeleton appears for ~500ms
- Data loads → skeleton replaced with real content
- No layout shift between skeleton and content

## Evidence References

Once resolved:
- `file: apps/dapp/frontend/components/Skeleton.tsx` (skeleton components)
- `file: apps/dapp/frontend/app/dashboard/page.tsx#<lines>` (skeleton usage)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [E-04], [DAPP-21]
- GitHub issue #1115
