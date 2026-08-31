# [DAPP-24] Add WebSocket heartbeat and auto-reconnect

**Status:** Open  
**Priority:** Medium  
**Phase:** 1 (Core Savings Vaults)  
**Type:** Enhancement

## Issue

Real-time balance updates via WebSocket have no heartbeat or automatic reconnection logic. When the network connection drops or becomes unstable, users see stale balances until they manually refresh the page.

**Related PRD claims:**
- [DAPP-24] WebSocket heartbeat + auto-reconnect
- [P-08] WebSocket connections have no heartbeat / reconnection handling in DApp
- [E-06] DApp: WebSocket heartbeat + auto-reconnect

## Acceptance Criteria

- [ ] Implement ping/pong heartbeat on WebSocket client (send ping every 30s)
- [ ] Detect connection loss if pong not received within 60s
- [ ] Implement exponential backoff reconnect: 1s, 2s, 4s, 8s, max 60s
- [ ] Show "reconnecting…" badge in UI during reconnect
- [ ] Resume data subscription after successful reconnect
- [ ] Log all connection state changes for debugging
- [ ] Test: simulate network drop → verify reconnect and badge
- [ ] Test: verify max retry delay does not exceed 60s

## Implementation

**File:** `apps/dapp/frontend/hooks/useWebSocket.ts`

```typescript
const useWebSocket = (url: string) => {
  const [isConnected, setIsConnected] = useState(true);
  const wsRef = useRef<WebSocket | null>(null);
  const heartbeatIntervalRef = useRef<NodeJS.Timeout | null>(null);
  const reconnectAttemptsRef = useRef(0);

  useEffect(() => {
    connect();
    return () => disconnect();
  }, []);

  const connect = () => {
    wsRef.current = new WebSocket(url);
    wsRef.current.onopen = () => {
      setIsConnected(true);
      reconnectAttemptsRef.current = 0;
      startHeartbeat();
    };
    wsRef.current.onclose = () => {
      setIsConnected(false);
      scheduleReconnect();
    };
  };

  const startHeartbeat = () => {
    heartbeatIntervalRef.current = setInterval(() => {
      wsRef.current?.send(JSON.stringify({ type: 'ping' }));
    }, 30000); // Every 30s
  };

  const scheduleReconnect = () => {
    const delay = Math.min(
      Math.pow(2, reconnectAttemptsRef.current) * 1000,
      60000 // Max 60s
    );
    reconnectAttemptsRef.current++;
    setTimeout(connect, delay);
  };

  return { isConnected };
};
```

**File:** `apps/dapp/frontend/components/ConnectionStatus.tsx`

```tsx
export const ConnectionStatus = ({ isConnected }: { isConnected: boolean }) => {
  if (isConnected) return null;
  return (
    <div className="fixed top-4 right-4 bg-yellow-500 text-white px-4 py-2 rounded">
      🔄 Reconnecting...
    </div>
  );
};
```

## Testing

- WebSocket connected → badge not shown
- Simulate connection drop → badge shows "Reconnecting…"
- Connection restored → badge disappears, data resumes

## Evidence References

Once resolved:
- `file: apps/dapp/frontend/hooks/useWebSocket.ts` (heartbeat logic)
- `file: apps/dapp/frontend/components/ConnectionStatus.tsx` (UI indicator)
- `pr: #<number>` (merged PR)

## Related Issues

- PRD [P-08], [E-06], [DAPP-24]
- GitHub issue #1115
