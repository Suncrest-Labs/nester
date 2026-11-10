import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { SavingsActivityPage } from "@/lib/stellar/activity";

vi.mock("@/components/app-shell", () => ({
  AppShell: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));

const wallet = { isConnected: true, address: "GAWALLET" };
vi.mock("@/components/wallet-provider", () => ({ useWallet: () => wallet }));

const state = vi.hoisted(() => ({ coreId: "CCORE" as string | null }));
vi.mock("@/lib/contracts", () => ({ getContractId: () => state.coreId }));

const fetchSavingsActivity = vi.fn<(w: string, c: string, cursor?: string) => Promise<SavingsActivityPage>>();
vi.mock("@/lib/stellar/activity", () => ({
  fetchSavingsActivity: (w: string, c: string, cursor?: string) => fetchSavingsActivity(w, c, cursor),
}));

import ActivityPage from "@/app/activity/page";

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ActivityPage />
    </QueryClientProvider>
  );
}

const deposit = { id: "1", kind: "deposit" as const, amount: "100", hash: "abc", createdAt: "2026-11-01T10:53:40Z" };
const withdraw = { id: "2", kind: "withdraw" as const, amount: "40", hash: "def", createdAt: "2026-11-01T10:54:10Z" };

beforeEach(() => {
  fetchSavingsActivity.mockReset();
  wallet.isConnected = true;
  state.coreId = "CCORE";
});

describe("Activity page", () => {
  it("asks a disconnected visitor to connect", () => {
    wallet.isConnected = false;
    renderPage();

    expect(screen.getByRole("heading", { level: 1, name: "Activity" })).toBeTruthy();
    expect(screen.getByRole("link", { name: "Connect wallet" }).getAttribute("href")).toBe("/");
    expect(fetchSavingsActivity).not.toHaveBeenCalled();
  });

  it("says when the savings rate isn't deployed", () => {
    state.coreId = null;
    renderPage();

    expect(screen.getByText("The Savings Rate isn't live on this network yet.")).toBeTruthy();
  });

  it("lists deposits and withdrawals with totals and explorer links", async () => {
    fetchSavingsActivity.mockResolvedValue({ items: [withdraw, deposit], scanned: 7, nextCursor: null });
    renderPage();

    expect(await screen.findByText("Deposit")).toBeTruthy();
    expect(screen.getByText("Withdraw")).toBeTruthy();
    expect(screen.getByText("+$100.00")).toBeTruthy();
    expect(screen.getByText("−$40.00")).toBeTruthy();
    expect(screen.getByText("Deposited")).toBeTruthy();
    expect(screen.getByText("Withdrawn")).toBeTruthy();
    const links = screen.getAllByRole("link", { name: /View on explorer/ });
    expect(links[0].getAttribute("href")).toMatch(/\/tx\/def$/);
    expect(fetchSavingsActivity).toHaveBeenCalledWith("GAWALLET", "CCORE", undefined);
  });

  it("labels partial totals and loads more with the cursor", async () => {
    fetchSavingsActivity
      .mockResolvedValueOnce({ items: [withdraw], scanned: 50, nextCursor: "cur-1" })
      .mockResolvedValueOnce({ items: [deposit], scanned: 3, nextCursor: null });
    renderPage();

    expect(await screen.findByText("Deposited in the last 50 transactions")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Load more" }));

    await waitFor(() => expect(screen.getByText("Deposit")).toBeTruthy());
    expect(fetchSavingsActivity).toHaveBeenLastCalledWith("GAWALLET", "CCORE", "cur-1");
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
    expect(screen.getByText("Deposited")).toBeTruthy();
  });

  it("shows an empty state that points to the dashboard", async () => {
    fetchSavingsActivity.mockResolvedValue({ items: [], scanned: 4, nextCursor: null });
    renderPage();

    expect(await screen.findByText("No savings activity yet")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Start saving" }).getAttribute("href")).toBe("/dashboard");
  });

  it("offers a retry when Horizon fails", async () => {
    fetchSavingsActivity.mockRejectedValueOnce(new Error("down"));
    renderPage();

    expect(await screen.findByText("Your activity couldn't be loaded.")).toBeTruthy();
    fetchSavingsActivity.mockResolvedValue({ items: [deposit], scanned: 1, nextCursor: null });
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));

    expect(await screen.findByText("Deposit")).toBeTruthy();
  });
});
