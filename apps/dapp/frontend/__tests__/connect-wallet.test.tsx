import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ConnectWallet } from "@/components/connect-wallet";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));

const mockConnect = vi.fn();
const mockDisconnect = vi.fn();

vi.mock("@/components/wallet-provider", () => ({
  useWallet: vi.fn(() => ({
    connect: mockConnect,
    disconnect: mockDisconnect,
    isConnecting: false,
    wallets: [{ id: "freighter", name: "Freighter", icon: null }],
    walletsLoaded: true,
    isConnected: false,
    address: null,
    selectedWalletId: null,
  })),
}));

vi.mock("@/components/portfolio-provider", () => ({
  usePortfolio: () => ({
    balances: { USDC: 0, XLM: 0, USDT: 0 },
    applyBalanceUpdate: vi.fn(),
  }),
}));


vi.mock("@/components/notifications-provider", () => ({
  useNotifications: () => ({ addNotification: vi.fn() }),
}));

vi.mock("@/hooks/useNetwork", () => ({
  useNetwork: () => ({ currentNetwork: { id: "testnet" } }),
}));

describe("ConnectWallet", () => {
  // The disconnected screen deliberately shows one Connect button and no
  // wallet list: a first-time visitor should not be handed a grid of wallets
  // they may not have installed. The list is one click away.
  it("shows a single connect action when disconnected", () => {
    render(<ConnectWallet />);
    expect(screen.getByRole("button", { name: /^Connect$/ })).toBeInTheDocument();
    expect(screen.queryByText("Freighter")).not.toBeInTheDocument();
  });

  it("reveals the wallet list after the connect action is used", () => {
    render(<ConnectWallet />);
    fireEvent.click(screen.getByRole("button", { name: /^Connect$/ }));
    expect(screen.getByText("Freighter")).toBeInTheDocument();
  });

  it("calls connect when wallet is selected", async () => {
    mockConnect.mockResolvedValue(undefined);
    render(<ConnectWallet />);
    fireEvent.click(screen.getByRole("button", { name: /^Connect$/ }));
    fireEvent.click(screen.getByText("Freighter"));
    expect(mockConnect).toHaveBeenCalledWith("freighter");
  });

  it("shows connected state when wallet is linked", async () => {
    const { useWallet } = await import("@/components/wallet-provider");
    vi.mocked(useWallet).mockReturnValue({
      connect: mockConnect,
      disconnect: mockDisconnect,
      isConnecting: false,
      wallets: [],
      walletsLoaded: true,
      isConnected: true,
      address: "GABC1234567890",
      user: { address: "GABC1234567890" },
      selectedWalletId: "freighter",
    } as ReturnType<typeof useWallet>);

    render(<ConnectWallet />);
    expect(screen.getByText(/wallet connected/i)).toBeInTheDocument();
  });
});
