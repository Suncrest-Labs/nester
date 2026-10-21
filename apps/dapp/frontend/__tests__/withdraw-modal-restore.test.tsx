import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { WithdrawModal } from "@/components/vault/withdrawModal";
import type { PortfolioPosition } from "@/components/portfolio-provider";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({
    address: "GCVVVNSKJAOJ6LCWBK4TG7IR3YEJJPGSBPVBYYBFI7M53V3QL5ABZ66D",
  }),
}));

vi.mock("@/lib/vault-contracts", () => ({
  getVaultContractById: () => ({
    contractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
  }),
}));

const mockRecordWithdrawal = vi.fn().mockReturnValue({
  penaltyAmount: 0,
  netAmount: 50,
});

vi.mock("@/components/portfolio-provider", () => ({
  usePortfolio: () => ({
    getWithdrawalQuote: () => ({
      grossAmount: 50,
      netAmount: 50,
      sharesBurned: 50,
      penaltyPct: 0,
    }),
    recordWithdrawal: mockRecordWithdrawal,
    refreshBalances: vi.fn(),
  }),
}));

let resolveTxPromise: (val: unknown) => void;

vi.mock("@/lib/stellar/transaction", () => ({
  executeVaultWithdraw: vi.fn().mockImplementation((params) => {
    params.onStatusChange?.("restoring");
    return new Promise((resolve) => {
      resolveTxPromise = resolve;
    });
  }),
  UserRejectedError: class UserRejectedError extends Error {},
  TransactionFailedError: class TransactionFailedError extends Error {},
  TransactionTimeoutError: class TransactionTimeoutError extends Error {},
  truncateTxHash: (h: string) => `${h.slice(0, 8)}…${h.slice(-8)}`,
}));

const mockPosition: PortfolioPosition = {
  id: "pos-1",
  vaultId: "usdc-vault",
  vaultName: "USDC Stable Yield",
  asset: "USDC",
  shares: 100,
  principalDeposited: 100,
  currentValue: 105,
  yieldEarned: 5,
  depositDate: "2025-01-01",
  maturityDate: "2025-06-01",
  isMatured: true,
  daysRemaining: 0,
  earlyWithdrawalPenaltyPct: 0,
};

describe("WithdrawModal restore copy (#1487)", () => {
  it("displays position reactivation copy when restore is required", async () => {
    render(<WithdrawModal open={true} onClose={() => {}} position={mockPosition} />);

    // Enter withdrawal amount
    const input = screen.getByPlaceholderText("0.00");
    fireEvent.change(input, { target: { value: "50" } });

    // Click confirm withdrawal button
    const confirmButton = screen.getByRole("button", { name: /confirm withdrawal/i });
    fireEvent.click(confirmButton);

    // Wait for the restore copy to appear
    await waitFor(() => {
      expect(
        screen.getByText(/Your position needs to be reactivated first/i)
      ).toBeInTheDocument();
    });

    expect(
      screen.getByText(
        /Please approve the initial transaction to restore your archived ledger entries/i
      )
    ).toBeInTheDocument();

    // Resolve transaction and wait for success state so test teardown is clean
    resolveTxPromise({
      txHash: "0123456789abcdef0123456789abcdef",
      explorerUrl: "https://stellar.expert/tx/0123456789abcdef",
      ledger: 100,
    });

    await waitFor(() => {
      expect(screen.getByText(/Withdrawal confirmed/i)).toBeInTheDocument();
    });
  });
});
