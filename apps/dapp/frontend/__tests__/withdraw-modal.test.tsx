import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { WithdrawModal } from "@/components/vault/withdrawModal";
import type { PortfolioPosition } from "@/components/portfolio-provider";
import { SECONDS_PER_DAY } from "@/lib/fees";

const WALLET_ADDRESS = "GABC1234567890123456789012345678901234567890123456789012345678";
const CONTRACT_ADDRESS = "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({ address: WALLET_ADDRESS }),
}));

vi.mock("@/components/portfolio-provider", () => ({
  usePortfolio: () => ({
    getWithdrawalQuote: (_id: string, amount: number) =>
      amount > 0
        ? {
            grossAmount: amount,
            penaltyAmount: 0,
            netAmount: amount,
            sharesBurned: amount,
          }
        : null,
    recordWithdrawal: vi.fn(),
    refreshBalances: vi.fn(),
  }),
}));

vi.mock("@/lib/vault-contracts", () => ({
  getVaultContractById: () => ({
    id: "usdc-vault",
    contractAddress: CONTRACT_ADDRESS,
  }),
}));

const previewWithdrawalFeeMock = vi.fn();
const previewFeeScheduleMock = vi.fn();

vi.mock("@/lib/stellar/transaction", () => ({
  executeVaultWithdraw: vi.fn(),
  previewWithdrawalFee: (...args: unknown[]) => previewWithdrawalFeeMock(...args),
  previewFeeSchedule: (...args: unknown[]) => previewFeeScheduleMock(...args),
  UserRejectedError: class UserRejectedError extends Error {},
  TransactionFailedError: class TransactionFailedError extends Error {},
  TransactionTimeoutError: class TransactionTimeoutError extends Error {},
  truncateTxHash: (h: string) => h.slice(0, 8),
}));

const basePosition: PortfolioPosition = {
  id: "pos-1",
  vaultId: "usdc-vault",
  vaultName: "USDC Core Vault",
  asset: "USDC",
  principal: 1000,
  currentValue: 1050,
  yieldEarned: 50,
  shares: 1000,
  apy: 8.5,
  depositedAt: "2026-01-01T00:00:00Z",
  maturityAt: "2026-07-31T00:00:00Z",
  isMatured: false,
  daysRemaining: 60,
  earlyWithdrawalPenaltyPct: 2.0,
};

describe("WithdrawModal fee tier preview", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    vi.setSystemTime(new Date("2026-06-01T00:00:00Z"));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("displays Tier 1 (< 90 days) fee, performance fee, next lower tier date, and net received from contract preview", async () => {
    previewWithdrawalFeeMock.mockResolvedValue({
      grossAssetValue: 500,
      managementFeeDeducted: 0,
      performanceFeeDeducted: 2.5,
      earlyWithdrawalFeeDeducted: 10,
      netAmountReceived: 487.5,
      available: true,
    });
    previewFeeScheduleMock.mockResolvedValue({
      tenureSecs: 30 * SECONDS_PER_DAY,
      currentExitFeeBps: 200,
      currentPerformanceFeeBps: 1000,
      nextBoundarySecs: 90 * SECONDS_PER_DAY,
      nextBoundaryExitFeeBps: 100,
      nextBoundaryPerfFeeBps: 1000,
      available: true,
    });

    render(<WithdrawModal open position={basePosition} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "500" } });

    await waitFor(() => {
      expect(previewWithdrawalFeeMock).toHaveBeenCalledWith({
        walletAddress: WALLET_ADDRESS,
        contractId: CONTRACT_ADDRESS,
        shares: 500,
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId("withdrawal-current-tier")).toHaveTextContent(
        "Tier 1 (< 90 days) (2.00%)"
      );
    });

    expect(screen.getByTestId("withdrawal-next-tier-date")).toHaveTextContent(
      "1.00% on Jul 31, 2026"
    );
    expect(screen.getByTestId("withdrawal-fee-amount")).toHaveTextContent("10.00 USDC");
    expect(screen.getByTestId("withdrawal-performance-fee")).toHaveTextContent("2.50 USDC");
    expect(screen.getByTestId("withdrawal-net-received")).toHaveTextContent("487.50 USDC");
  });

  it("displays Tier 2 (90–364 days) and next lower tier date", async () => {
    previewWithdrawalFeeMock.mockResolvedValue({
      grossAssetValue: 500,
      managementFeeDeducted: 0,
      performanceFeeDeducted: 1.0,
      earlyWithdrawalFeeDeducted: 5.0,
      netAmountReceived: 494.0,
      available: true,
    });
    previewFeeScheduleMock.mockResolvedValue({
      tenureSecs: 100 * SECONDS_PER_DAY,
      currentExitFeeBps: 100,
      currentPerformanceFeeBps: 1000,
      nextBoundarySecs: 365 * SECONDS_PER_DAY,
      nextBoundaryExitFeeBps: 25,
      nextBoundaryPerfFeeBps: 1000,
      available: true,
    });

    render(<WithdrawModal open position={basePosition} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "500" } });

    await waitFor(() => {
      expect(screen.getByTestId("withdrawal-current-tier")).toHaveTextContent(
        "Tier 2 (90–364 days) (1.00%)"
      );
    });

    expect(screen.getByTestId("withdrawal-next-tier-date")).toHaveTextContent(
      "0.25% on Feb 21, 2027"
    );
    expect(screen.getByTestId("withdrawal-fee-amount")).toHaveTextContent("5.00 USDC");
    expect(screen.getByTestId("withdrawal-net-received")).toHaveTextContent("494.00 USDC");
  });

  it("displays Tier 3 (365+ days · Lowest tier) with Lowest tier reached", async () => {
    previewWithdrawalFeeMock.mockResolvedValue({
      grossAssetValue: 500,
      managementFeeDeducted: 0,
      performanceFeeDeducted: 0,
      earlyWithdrawalFeeDeducted: 1.25,
      netAmountReceived: 498.75,
      available: true,
    });
    previewFeeScheduleMock.mockResolvedValue({
      tenureSecs: 400 * SECONDS_PER_DAY,
      currentExitFeeBps: 25,
      currentPerformanceFeeBps: 1000,
      nextBoundarySecs: null,
      nextBoundaryExitFeeBps: 25,
      nextBoundaryPerfFeeBps: 1000,
      available: true,
    });

    render(<WithdrawModal open position={basePosition} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "500" } });

    await waitFor(() => {
      expect(screen.getByTestId("withdrawal-current-tier")).toHaveTextContent(
        "Tier 3 (365+ days · Lowest tier) (0.25%)"
      );
    });

    expect(screen.getByTestId("withdrawal-next-tier-date")).toHaveTextContent(
      "Lowest tier reached"
    );
    expect(screen.getByTestId("withdrawal-fee-amount")).toHaveTextContent("1.25 USDC");
    expect(screen.getByTestId("withdrawal-net-received")).toHaveTextContent("498.75 USDC");
  });

  it("displays No exit fee (Matured) when exit fee is 0 bps and no next boundary exists", async () => {
    previewWithdrawalFeeMock.mockResolvedValue({
      grossAssetValue: 500,
      managementFeeDeducted: 0,
      performanceFeeDeducted: 0,
      earlyWithdrawalFeeDeducted: 0,
      netAmountReceived: 500,
      available: true,
    });
    previewFeeScheduleMock.mockResolvedValue({
      tenureSecs: 400 * SECONDS_PER_DAY,
      currentExitFeeBps: 0,
      currentPerformanceFeeBps: 0,
      nextBoundarySecs: null,
      nextBoundaryExitFeeBps: 0,
      nextBoundaryPerfFeeBps: 0,
      available: true,
    });

    render(
      <WithdrawModal
        open
        position={{ ...basePosition, isMatured: true, daysRemaining: 0, earlyWithdrawalPenaltyPct: 0 }}
        onClose={() => {}}
      />
    );
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "500" } });

    await waitFor(() => {
      expect(screen.getByTestId("withdrawal-current-tier")).toHaveTextContent(
        "No exit fee (Matured) (0.00%)"
      );
    });

    expect(screen.getByTestId("withdrawal-next-tier-date")).toHaveTextContent(
      "Lowest tier reached"
    );
    expect(screen.getByTestId("withdrawal-fee-amount")).toHaveTextContent("0.00 USDC");
    expect(screen.getByTestId("withdrawal-net-received")).toHaveTextContent("500.00 USDC");
  });
});
