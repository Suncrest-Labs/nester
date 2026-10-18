import React from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

import { DepositModal } from "@/components/vault/depositModal";
import { WithdrawModal } from "@/components/vault/withdrawModal";
import {
  buildWithdrawalFeePreview,
  getDepositFeeScheduleSummary,
} from "@/lib/stellar/fee-preview";
import type { Vault } from "@/lib/types/vault";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({
    address: "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF",
    isConnected: true,
  }),
}));

vi.mock("@/hooks/useOfflineStatus", () => ({
  useOfflineStatus: () => ({ isOffline: false }),
}));

vi.mock("@/components/portfolio-provider", () => ({
  usePortfolio: () => ({
    getAvailableBalance: () => 5_000,
    recordPendingDeposit: () => "pending-1",
    confirmPendingDeposit: vi.fn(),
    failPendingDeposit: vi.fn(),
    refreshBalances: vi.fn(),
    getWithdrawalQuote: (_id: string, amount: number) => ({
      grossAmount: amount,
      netAmount: amount * 0.995,
      penaltyAmount: amount * 0.005,
      sharesBurned: amount,
    }),
    recordWithdrawal: vi.fn(),
  }),
}));

const MOCK_VAULT: Vault = {
  id: "usdc-conservative-1",
  name: "Conservative USDC Vault",
  description: "Low-volatility USDC yield vault",
  riskLevel: "Conservative",
  apyRange: "6.5% - 8.2%",
  currentApy: 7.4,
  tvl: 120_000,
  depositCap: 250_000,
  maturityTerms: "30 days",
  allocations: [],
  supportedAssets: ["USDC"],
  contractAddress: "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM",
};

describe("Deposit & Withdrawal Fee Preview (#1556)", () => {
  it("computes fee amount, current tier, next lower tier date, and net received from withdrawal_fee_preview", () => {
    const fixedNow = Date.UTC(2026, 9, 18); // Oct 18, 2026
    const preview = buildWithdrawalFeePreview({
      rawPreview: {
        gross_asset_value: BigInt(1_000_0000000), // 1,000 USDC
        early_withdrawal_fee_deducted: BigInt(5_0000000), // 5 USDC (50 bps)
        performance_fee_deducted: BigInt(2_5000000), // 2.50 USDC
        management_fee_deducted: BigInt(0),
        net_amount_received: BigInt(992_5000000), // 992.50 USDC
      },
      rawSchedule: {
        tenure_secs: 3 * 86_400, // 3 days held
        current_exit_fee_bps: 50,
        current_performance_fee_bps: 1000,
        next_boundary_secs: 7 * 86_400, // Tier drops at 7 days (+4 days = Oct 22, 2026)
        next_boundary_exit_fee_bps: 25,
      },
      nowMs: fixedNow,
    });

    expect(preview.grossAssetValue).toBeCloseTo(1000, 2);
    expect(preview.feeAmount).toBeCloseTo(7.5, 2);
    expect(preview.netReceived).toBeCloseTo(992.5, 2);
    expect(preview.currentTierBps).toBe(50);
    expect(preview.currentTierLabel).toBe("Early Exit Tier (0.50%)");
    expect(preview.nextLowerTierDate).toBe("Oct 22, 2026");
    expect(preview.nextLowerTierBps).toBe(25);
  });

  it("shows the deposit fee schedule summary and link before signing in DepositModal", () => {
    const summary = getDepositFeeScheduleSummary({
      managementFeePct: 0.5,
      performanceFeePct: 10,
      earlyExitPenaltyPct: 0.5,
      lockDays: 30,
    });
    expect(summary.depositFeeLabel).toContain("0.00%");
    expect(summary.scheduleHref).toBe("/docs/fees#fee-schedule");

    render(<DepositModal open={true} onClose={vi.fn()} vault={MOCK_VAULT} />);

    const scheduleBox = screen.getByTestId("deposit-fee-schedule");
    expect(scheduleBox).toHaveTextContent("Fee Schedule Summary");
    expect(scheduleBox).toHaveTextContent("0.00% (No deposit fee)");

    const scheduleLink = screen.getByTestId("deposit-fee-schedule-link");
    expect(scheduleLink).toHaveAttribute("href", "/docs/fees#fee-schedule");
  });

  it("shows withdrawal fee amount, current tier, next lower tier date, and net received in WithdrawModal", async () => {
    render(
      <WithdrawModal
        open={true}
        onClose={vi.fn()}
        position={{
          id: "pos-1",
          vaultId: "usdc-conservative-1",
          vaultName: "Conservative USDC Vault",
          asset: "USDC",
          principal: 1000,
          currentValue: 1050,
          yieldEarned: 50,
          apy: 0.074,
          shares: 1000,
          depositedAt: new Date(Date.now() - 5 * 86_400_000).toISOString(),
          lockDays: 30,
          daysRemaining: 25,
          isMatured: false,
          earlyWithdrawalPenaltyPct: 0.5,
        }}
      />,
    );

    const input = screen.getByPlaceholderText("0.00") as HTMLInputElement;
    fireEvent.change(input, { target: { value: "1000" } });

    await waitFor(() => {
      expect(screen.getByTestId("withdraw-fee-amount")).toHaveTextContent("5.00 USDC");
      expect(screen.getByTestId("withdraw-current-tier")).toHaveTextContent("Early Exit Tier (0.50%)");
      expect(screen.getByTestId("withdraw-next-tier-date")).not.toHaveTextContent("Lowest tier active");
      expect(screen.getByTestId("withdraw-net-received")).toHaveTextContent("995.00 USDC");
    });
  });
});
