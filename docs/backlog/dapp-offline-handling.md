# [DAPP-20] Implement offline mode and network-loss handling

**Status:** Open  
**Priority:** Low  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Enhancement

## Issue

The DApp has no graceful degradation when network is lost. Users see blank pages or stuck loaders indefinitely. There is no offline mode, error messaging, or retry logic.

**Related PRD claims:**
- [DAPP-20] Offline / network-loss handling
- [E-06] DApp: WebSocket heartbeat + auto-reconnect

## Acceptance Criteria

- [ ] Detect network loss (offline event or failed requests)
- [ ] Show "offline" badge in header or toast notification
- [ ] Disable write operations (deposit, withdraw) when offline
- [ ] Cache critical data (vault list, portfolio) for offline viewing
- [ ] Implement request retry queue: queue failed mutations, retry when online
- [ ] Add "Retry" button in error states
- [ ] Test: disable network → verify offline mode activates
- [ ] Test: re-enable network → verify retry queue processes

## Implementation

Use `@react-native-async-storage/async-storage` for persistence and Zustand or React Context for offline state.

## Testing

- Network disconnected → offline badge appears
- Cached data displayed (if available)
- Network reconnected → retry queue processes

## Evidence References

Once resolved:
- `file: apps/dapp/frontend/hooks/useOnline.ts` (offline detection)
- `file: apps/dapp/frontend/components/OfflineBadge.tsx` (UI indicator)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [E-06], [DAPP-20]
- GitHub issue #1115
