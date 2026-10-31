import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

let address: string | null = "GA";
vi.mock("@/components/wallet-provider", () => ({ useWallet: () => ({ address }) }));
vi.mock("@/lib/auth/token-store", () => ({ getAccessToken: () => "tok" }));
const fetchPosition = vi.fn();
vi.mock("@/lib/api/savings", () => ({
  fetchSavingsSummary: vi.fn(async () => ({})),
  fetchSavingsHistory: vi.fn(async () => []),
  fetchSavingsPosition: () => fetchPosition(),
}));

import { useSavings } from "@/hooks/useSavings";

describe("useSavings position", () => {
  beforeEach(() => {
    address = "GA";
    fetchPosition.mockReset();
  });

  it("does not show the previous wallet's position after switching or disconnecting", async () => {
    fetchPosition.mockResolvedValueOnce({ shares: "1", value: "100", deposited: null });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
    const { result, rerender } = renderHook(() => useSavings(), { wrapper });
    await waitFor(() => expect(result.current.position?.value).toBe("100"));

    fetchPosition.mockImplementation(() => new Promise(() => {}));
    address = "GB";
    rerender();
    expect(result.current.position).toBeUndefined();

    address = null;
    rerender();
    expect(result.current.position).toBeUndefined();
  });
});
