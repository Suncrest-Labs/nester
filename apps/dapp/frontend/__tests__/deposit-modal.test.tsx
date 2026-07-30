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
    refreshBalances: vi.fn(),
  }),
}));

vi.mock("@/lib/stellar/transaction", () => ({
  executeVaultDeposit: vi.fn(),
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
  strategies: []
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
});
