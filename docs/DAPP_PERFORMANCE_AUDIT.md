# Nester DApp Performance Audit & Core Web Vitals Budget

## 1. Executive Summary & Field Profile

Nester serves global financial users predominantly navigating via mid-range mobile hardware (4-core ARM, 4GB RAM) on variable 3G/4G cellular connections.

To guarantee instant usability and zero loss of trust during financial operations (depositing savings, checking yields, withdrawing capital), strict Core Web Vitals (CWV) budgets and code-splitting boundaries are enforced.

---

## 2. Core Web Vitals Budget Matrix

| Metric | Target (Good) | Warning Threshold | CI Failure Threshold | Measurement Profile |
| :--- | :--- | :--- | :--- | :--- |
| **LCP** (Largest Contentful Paint) | $\le 2.0\text{ s}$ | $2.5\text{ s}$ | $> 3.0\text{ s}$ | Moto G Power, Fast 3G (1.6 Mbps / 150ms RTT) |
| **INP** (Interaction to Next Paint) | $\le 100\text{ ms}$ | $150\text{ ms}$ | $> 200\text{ ms}$ | 4x CPU Throttling, Active Input Flow |
| **CLS** (Cumulative Layout Shift) | $\le 0.05$ | $0.08$ | $> 0.10$ | Layout shift throughout async balance fetches |
| **Initial JS Chunk Size** | $\le 250\text{ KB}$ | $300\text{ KB}$ | $> 350\text{ KB}$ | Uncompressed parsed JS |
| **Total Route Bundle** | $\le 1.0\text{ MB}$ | $1.2\text{ MB}$ | $> 1.5\text{ MB}$ | Aggregate client bundles |

---

## 3. Bundle Analysis & Optimization Strategy

### 1. Dynamic Code-Splitting
- Heavy analytics charts (`Recharts`, `Lucide` icon sets, complex projection canvas) are dynamically imported using `next/dynamic` with lightweight skeleton placeholders.
- Wallet SDK modals (`Freighter`, `XBULL`, `Albedo`) are loaded on-demand only when the user triggers the connect action.

### 2. Live Balance & Animation Render Optimization
- Number animations for balance transitions throttle repaint ticks using `requestAnimationFrame` and completely bypass motion when `prefers-reduced-motion` is active.
- Real-time SSE / WebSocket updates batch state changes to eliminate layout thrashing.

---

## 4. Verification & CI Automation

Performance budgets are guarded in CI via `scripts/check-performance-budget.js` and Vitest automated suites in `apps/dapp/frontend/__tests__/performance-budget.test.ts`. Any regression exceeding initial chunk thresholds fails the build automatically.
