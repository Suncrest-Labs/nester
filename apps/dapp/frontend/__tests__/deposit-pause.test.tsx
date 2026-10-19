import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { DepositModal } from "@/components/vault/depositModal";
import type { Vault as VaultType } from "@/lib/types/vault";

const status = vi.hoisted(() => ({
  deposit: { paused: false, reason: "" },
  withdrawal: { paused: false, reason: "" },
}));

vi.mock("@/hooks/useMoneyPathStatus", () => ({
  useMoneyPathStatus: () => status,
}));

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({
    address: "GABC1234567890123456789012345678901234567890123456789012345678",
  }),
}));

vi.mock("@/components/portfolio-provider", () => ({
  usePortfolio: () => ({
    getAvailableBalance: () => 1000,
    recordPendingDeposit: vi.fn(),
    confirmPendingDeposit: vi.fn(),
    failPendingDeposit: vi.fn(),
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

const vault: VaultType = {
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

describe("DepositModal when deposits are globally paused", () => {
  beforeEach(() => {
    status.deposit = { paused: false, reason: "" };
  });

  it("allows a valid deposit when nothing is paused", () => {
    render(<DepositModal open vault={vault} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "100" } });

    expect(screen.getByRole("button", { name: "Confirm Deposit" })).toBeEnabled();
  });

  it("explains the pause honestly and blocks the deposit", () => {
    status.deposit = { paused: true, reason: "Investigating an RPC issue" };
    render(<DepositModal open vault={vault} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "100" } });

    expect(
      screen.getByText("Deposits are temporarily paused: Investigating an RPC issue")
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Confirm Deposit" })).toBeDisabled();
  });
});
