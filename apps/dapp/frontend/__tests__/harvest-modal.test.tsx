import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { HarvestModal } from "@/components/vault/HarvestModal";
import { vaultsApi } from "@/lib/api/vaults";
import type { HarvestPreview, HarvestResult } from "@/lib/api/vaults";
import { fetchYieldHarvests } from "@/lib/api/yields";
import type { YieldHarvest } from "@/lib/api/yields";

vi.mock("@/lib/api/vaults", () => ({
  vaultsApi: {
    previewHarvest: vi.fn(),
    harvest: vi.fn(),
  },
}));

vi.mock("@/lib/api/yields", () => ({
  fetchYieldHarvests: vi.fn(),
}));

vi.mock("@/utils/explorer", () => ({
  getExplorerTxUrl: (txHash: string) =>
    `https://stellar.example/transactions/${txHash}`,
}));

const preview: HarvestPreview = {
  vault_id: "vault-1",
  gross_yield_usdc: "12.500000",
  performance_fee_usdc: "1.250000",
  net_yield_usdc: "11.250000",
  gas_cost_usdc: "0.050000",
  net_yield_after_gas_usdc: "11.200000",
  compounded: true,
  estimated_new_shares: "11.250000",
  performance_fee_bps: 1000,
};

const result: HarvestResult = {
  gross_yield_usdc: "12.500000",
  performance_fee_usdc: "1.250000",
  net_yield_usdc: "11.250000",
  compounded: true,
  new_shares_minted: "11.250000",
  tx_hash: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
};

const zeroYieldPreview: HarvestPreview = {
  vault_id: "vault-1",
  gross_yield_usdc: "0",
  performance_fee_usdc: "0",
  net_yield_usdc: "0",
  compounded: true,
  performance_fee_bps: 1000,
  impaired: true,
};

const vaultHarvest: YieldHarvest = {
  id: "harvest-1",
  user_id: "user-1",
  vault_id: "vault-1",
  amount: "3.400000",
  currency: "USDC",
  protocol: "blend",
  harvested_at: "2026-05-01T10:00:00Z",
  tx_hash: "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
};

function renderHarvestModal() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  function wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  }

  return render(
    <HarvestModal
      open
      onClose={() => {}}
      vaultId="vault-1"
      vaultName="USDC Market"
    />,
    { wrapper },
  );
}

describe("HarvestModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchYieldHarvests).mockResolvedValue({
      items: [],
      next_cursor: "",
    });
  });

  it("shows preview loading state on open", async () => {
    let resolvePreview: (value: HarvestPreview) => void = () => {};
    vi.mocked(vaultsApi.previewHarvest).mockReturnValue(
      new Promise<HarvestPreview>((resolve) => {
        resolvePreview = resolve;
      }),
    );

    renderHarvestModal();

    expect(screen.getByText(/loading harvest preview/i)).toBeInTheDocument();

    resolvePreview(preview);
    await screen.findByText("Gross Yield");
  });

  it("displays preview data", async () => {
    vi.mocked(vaultsApi.previewHarvest).mockResolvedValue(preview);

    renderHarvestModal();

    expect(await screen.findByText("Gross Yield")).toBeInTheDocument();
    expect(screen.getByText("12.50 USDC")).toBeInTheDocument();
    expect(screen.getByText("Performance Fee (10%)")).toBeInTheDocument();
    expect(screen.getByText("1.25 USDC")).toBeInTheDocument();
    // Gas from the API, and net after gas as reported by the API (11.25 net
    // minus 0.05 estimated gas), not a gas-free figure.
    expect(screen.getByText("0.05 USDC")).toBeInTheDocument();
    expect(screen.getByText("11.20 USDC")).toBeInTheDocument();
    expect(screen.getByText("11.250000")).toBeInTheDocument();
    expect(vaultsApi.previewHarvest).toHaveBeenCalledWith("vault-1", true);
  });

  it("lists this vault's past harvests with net after gas", async () => {
    vi.mocked(vaultsApi.previewHarvest).mockResolvedValue(preview);
    vi.mocked(fetchYieldHarvests).mockResolvedValue({
      items: [vaultHarvest],
      next_cursor: "",
    });

    renderHarvestModal();

    expect(await screen.findByText("May 1, 2026")).toBeInTheDocument();
    expect(screen.getByText("blend")).toBeInTheDocument();
    expect(screen.getByText("3.40 USDC")).toBeInTheDocument();
    // 3.40 net - 0.05 estimated gas.
    expect(screen.getByText("3.35 USDC")).toBeInTheDocument();
    expect(fetchYieldHarvests).toHaveBeenCalledWith(undefined, 20, "vault-1");
  });

  it("shows an empty state when the vault has no harvests", async () => {
    vi.mocked(vaultsApi.previewHarvest).mockResolvedValue(preview);

    renderHarvestModal();

    expect(
      await screen.findByText(/no harvests have been recorded for this vault/i),
    ).toBeInTheDocument();
  });

  it("submits harvest from the confirm step", async () => {
    vi.mocked(vaultsApi.previewHarvest).mockResolvedValue(preview);
    vi.mocked(vaultsApi.harvest).mockResolvedValue(result);

    renderHarvestModal();

    await screen.findByText("Gross Yield");
    fireEvent.click(
      screen.getByRole("button", { name: "Continue to Confirmation" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Confirm Harvest" }));

    await waitFor(() => {
      expect(vaultsApi.harvest).toHaveBeenCalledWith("vault-1", true);
    });
    expect(await screen.findByText(/harvest confirmed/i)).toBeInTheDocument();
    expect(screen.getByText(/you received 11.25 USDC/i)).toBeInTheDocument();
  });

  it("switches preview between compound and withdraw", async () => {
    vi.mocked(vaultsApi.previewHarvest).mockResolvedValue(preview);

    renderHarvestModal();

    await screen.findByText("Gross Yield");
    fireEvent.click(screen.getByRole("button", { name: /withdraw/i }));

    await waitFor(() => {
      expect(vaultsApi.previewHarvest).toHaveBeenCalledWith("vault-1", false);
    });
    expect(
      screen.getByText(/net yield will be sent back to your connected wallet/i),
    ).toBeInTheDocument();
  });

  it("warns and disables harvest when preview has zero yield", async () => {
    vi.mocked(vaultsApi.previewHarvest).mockResolvedValue(zeroYieldPreview);

    renderHarvestModal();

    expect(
      await screen.findByText(/impaired vault warning/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Continue to Confirmation" }),
    ).toBeDisabled();
  });
});
