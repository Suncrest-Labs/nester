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
import { ApiError } from "@/lib/api/client";
import * as savingsApi from "@/lib/api/savings";

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

describe("useSavings position status", () => {
  beforeEach(() => {
    address = "GA";
    fetchPosition.mockReset();
  });

  it("reports position loading and errors separately from the summary", async () => {
    let reject: (e: Error) => void = () => {};
    fetchPosition.mockImplementation(() => new Promise((_, r) => (reject = r)));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useSavings(), { wrapper });
    await waitFor(() => expect(result.current.positionLoading).toBe(true));
    expect(result.current.positionError).toBeNull();

    reject(new Error("position down"));
    await waitFor(() => expect(result.current.positionError?.message).toBe("position down"));
    expect(result.current.positionLoading).toBe(false);
  });
});

describe("useSavings retries", () => {
  beforeEach(() => {
    address = "GA";
    fetchPosition.mockReset();
    fetchPosition.mockResolvedValue({ shares: "1", value: "1", deposited: null });
    vi.mocked(savingsApi.fetchSavingsSummary).mockClear();
    vi.mocked(savingsApi.fetchSavingsHistory).mockClear();
  });

  it("does not retry summary or history on 503 (savings unconfigured)", async () => {
    const err = new ApiError(503, "SERVICE_UNAVAILABLE", "not configured");
    vi.mocked(savingsApi.fetchSavingsSummary).mockRejectedValue(err);
    vi.mocked(savingsApi.fetchSavingsHistory).mockRejectedValue(err);
    const qc = new QueryClient();
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useSavings(), { wrapper });
    await waitFor(() => expect(result.current.error).toBe(err));
    expect(savingsApi.fetchSavingsSummary).toHaveBeenCalledTimes(1);
    expect(savingsApi.fetchSavingsHistory).toHaveBeenCalledTimes(1);
  });
});
