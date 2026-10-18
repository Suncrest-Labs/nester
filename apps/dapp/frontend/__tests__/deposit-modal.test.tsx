import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { DepositModal } from "@/components/vault/depositModal";
import type { Vault as VaultType } from "@/lib/types/vault";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({
    address: "GABC1234567890123456789012345678901234567890123456789012345678",
  }),
}));

vi.mock("@/components/portfolio-provider", () => ({
  usePortfolio: () => ({
    getAvailableBalance: () => 1000,
    recordDeposit: vi.fn(),
    recordPendingDeposit: vi.fn(),
    confirmPendingDeposit: vi.fn(),
    failPendingDeposit: vi.fn(),
    refreshBalances: vi.fn(),
  }),
}));

vi.mock("@/lib/stellar/transaction", () => ({
  executeVaultDeposit: vi.fn(),
  getCurrentNetwork: () => ({
    id: "testnet",
    rpcUrl: "https://soroban-testnet.stellar.org",
    networkPassphrase: "Test SDF Network ; September 2015",
  }),
  UserRejectedError: class UserRejectedError extends Error {},
  TransactionFailedError: class TransactionFailedError extends Error {},
  TransactionTimeoutError: class TransactionTimeoutError extends Error {},
  truncateTxHash: (h: string) => h.slice(0, 8),
}));

const mockVault: VaultType = {
  id: "usdc",
  name: "USDC Market",
  description: "Test market",
  marketType: "single",
  tokens: ["USDC"],
  currentApy: 10,
  apyRange: "8-12%",
  tvl: 1000000,
  utilization: 80,
  allocations: [],
  supportedAssets: ["USDC"],
  maturityTerms: "Flexible",
  earlyWithdrawalPenalty: "None",
  apyHistory: [],
  strategies: [],
};

describe("DepositModal", () => {
  it("validates amount input", () => {
    render(<DepositModal open vault={mockVault} onClose={() => {}} />);
    const input = screen.getByPlaceholderText("0.00");
    fireEvent.change(input, { target: { value: "0" } });
    expect(input).toHaveValue("0");
  });

  it("shows amount preview when valid value entered", () => {
    render(<DepositModal open vault={mockVault} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "100" } });
    expect(screen.getByText(/estimated annual yield/i)).toBeInTheDocument();
  });

  it("displays error for insufficient balance", () => {
    render(<DepositModal open vault={mockVault} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "5000" } });
    expect(screen.getByText(/insufficient balance/i)).toBeInTheDocument();
  });

  it("shows capacity for an open capped vault and allows full wallet Max when room exceeds balance", () => {
    const openCappedVault: VaultType = {
      ...mockVault,
      totalAssets: 2500,
      depositCap: 10000,
    };
    render(<DepositModal open vault={openCappedVault} onClose={() => {}} />);
    expect(screen.getByText("2,500.00 of 10,000.00 USDC filled")).toBeInTheDocument();
    expect(screen.getByText(/Remaining room: 7,500\.00 USDC/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /^max$/i }));
    expect(screen.getByPlaceholderText("0.00")).toHaveValue("1000.00");
  });

  it("clamps the Max button to remaining room and warns when the vault is nearly full", () => {
    const nearlyFullVault: VaultType = {
      ...mockVault,
      totalAssets: 9750,
      depositCap: 10000,
    };
    render(<DepositModal open vault={nearlyFullVault} onClose={() => {}} />);
    expect(screen.getByText("9,750.00 of 10,000.00 USDC filled")).toBeInTheDocument();
    expect(
      screen.getByText(/Nearly full — only 250\.00 USDC remaining under the current deposit cap/i)
    ).toBeInTheDocument();

    // User wallet balance is 1,000 USDC, but only 250 USDC room remains under the cap.
    fireEvent.click(screen.getByRole("button", { name: /^max$/i }));
    expect(screen.getByPlaceholderText("0.00")).toHaveValue("250.00");

    // Entering more than the remaining 250 USDC shows a remaining-capacity error.
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "400" } });
    expect(
      screen.getByText(/Amount exceeds remaining vault capacity \(250\.00 USDC available\)/i)
    ).toBeInTheDocument();
  });

  it("shows 'Vault is at capacity' with an explanation and disables deposit when the vault is full", () => {
    const fullVault: VaultType = {
      ...mockVault,
      totalAssets: 10000,
      depositCap: 10000,
    };
    render(<DepositModal open vault={fullVault} onClose={() => {}} />);
    expect(screen.getByText("10,000.00 of 10,000.00 USDC filled")).toBeInTheDocument();
    expect(screen.getByText("Vault is at capacity")).toBeInTheDocument();
    expect(
      screen.getByText(/Capacity increases are scheduled through the governance timelock/i)
    ).toBeInTheDocument();

    expect(screen.getByPlaceholderText("0.00")).toBeDisabled();
    expect(screen.getByRole("button", { name: /^max$/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /confirm deposit/i })).toBeDisabled();
  });
});
