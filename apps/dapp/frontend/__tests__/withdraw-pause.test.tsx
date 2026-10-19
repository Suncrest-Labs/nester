import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { WithdrawModal } from "@/components/vault-action-modals";
import type { PortfolioPosition } from "@/components/portfolio-provider";

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
    getWithdrawalQuote: () => null,
    recordWithdrawal: vi.fn(),
    getAvailableBalance: () => 1000,
  }),
}));

vi.mock("@/hooks/useTokenPrices", () => ({ useTokenPrices: () => ({ prices: { XLM: 0.1 } }) }));
vi.mock("@/hooks/useStellarFeeEstimate", () => ({
  useStellarFeeEstimate: () => ({ estimate: null, loading: false }),
}));
vi.mock("@/hooks/useDepositPreview", () => ({
  useDepositPreview: () => ({ preview: null, loading: false }),
}));
vi.mock("@/hooks/useNetwork", () => ({
  useNetwork: () => ({ currentNetwork: { id: "testnet", name: "Testnet" } }),
}));
vi.mock("@/lib/stellar/transaction", () => ({
  executeVaultWithdraw: vi.fn(),
  UserRejectedError: class UserRejectedError extends Error {},
  NetworkMismatchError: class NetworkMismatchError extends Error {},
  WalletDisconnectedError: class WalletDisconnectedError extends Error {},
  TransactionFailedError: class TransactionFailedError extends Error {},
  TransactionTimeoutError: class TransactionTimeoutError extends Error {},
  truncateTxHash: (h: string) => h.slice(0, 8),
}));

const position = {
  id: "pos-1",
  vaultId: "usdc",
  vaultName: "USDC Market",
  asset: "USDC",
  principal: 500,
  shares: 500,
  apy: 10,
  depositedAt: "2026-01-01T00:00:00Z",
  maturityAt: "2026-01-01T00:00:00Z",
  earlyWithdrawalPenaltyPct: 0,
  currentValue: 500,
  yieldEarned: 0,
  isMatured: true,
  daysRemaining: 0,
} as unknown as PortfolioPosition;

describe("WithdrawModal when withdrawals are globally paused", () => {
  beforeEach(() => {
    status.withdrawal = { paused: false, reason: "" };
  });

  it("shows no notice when nothing is paused", () => {
    render(<WithdrawModal open position={position} onClose={() => {}} />);
    expect(screen.queryByText(/temporarily paused/i)).not.toBeInTheDocument();
  });

  it("explains the pause honestly and keeps confirm disabled", () => {
    status.withdrawal = { paused: true, reason: "" };
    render(<WithdrawModal open position={position} onClose={() => {}} />);

    expect(
      screen.getByText("Withdrawals are temporarily paused for maintenance.")
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /confirm withdrawal/i })).toBeDisabled();
  });
});
