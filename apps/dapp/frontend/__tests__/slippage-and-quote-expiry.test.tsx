import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import {
    DEFAULT_SLIPPAGE_PCT,
    MAX_SLIPPAGE_PCT,
    QUOTE_EXPIRY_MS,
    clampSlippagePct,
    applySlippageToStroops,
    applySlippageToAmount,
    isQuoteStale,
    ensureFreshQuote,
} from "@/lib/stellar/slippage";
import { DepositModal } from "@/components/vault/depositModal";
import { WithdrawModal } from "@/components/vault/withdrawModal";
import type { VaultDefinition } from "@/lib/vault-data";
import type { PortfolioPosition } from "@/components/portfolio-provider";

const mockExecuteVaultDeposit = vi.fn();
const mockExecuteVaultWithdraw = vi.fn();
const mockGetWithdrawalQuote = vi.fn();

vi.mock("@/lib/stellar/transaction", async (importOriginal) => {
    const actual = await importOriginal<typeof import("@/lib/stellar/transaction")>();
    return {
        ...actual,
        executeVaultDeposit: (...args: unknown[]) => mockExecuteVaultDeposit(...args),
        executeVaultWithdraw: (...args: unknown[]) => mockExecuteVaultWithdraw(...args),
    };
});

vi.mock("@/components/wallet-provider", () => ({
    useWallet: () => ({
        address: "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF",
    }),
}));

vi.mock("@/hooks/useOfflineStatus", () => ({
    useOfflineStatus: () => ({ isOffline: false }),
}));

vi.mock("@/lib/api/vaults", () => ({
    vaultsApi: {
        registerTransaction: vi.fn().mockResolvedValue({}),
    },
}));

vi.mock("@/components/portfolio-provider", () => ({
    usePortfolio: () => ({
        getAvailableBalance: () => 5000,
        recordPendingDeposit: () => "pending-1",
        confirmPendingDeposit: vi.fn(),
        failPendingDeposit: vi.fn(),
        recordWithdrawal: () => ({ penaltyAmount: 0, netAmount: 100 }),
        refreshBalances: vi.fn(),
        getWithdrawalQuote: (...args: unknown[]) => mockGetWithdrawalQuote(...args),
    }),
}));

const sampleVault: VaultDefinition = {
    id: "conservative",
    name: "Conservative Vault",
    riskTier: "Conservative",
    tagline: "Low-volatility USDC yield",
    description: "Conservative USDC strategy",
    supportedAssets: ["USDC"],
    apyRange: "5% - 7%",
    currentApy: 6,
    tvl: "$1.2M",
    maturityTerms: "30 days lock",
    earlyExitFee: "0.5%",
    allocation: [],
    strategies: [],
    historicalPerformance: [],
    contractAddress: `C${"A".repeat(55)}`,
};

const samplePosition: PortfolioPosition = {
    id: "pos-1",
    vaultId: "conservative",
    vaultName: "Conservative Vault",
    asset: "USDC",
    depositedAmount: 1000,
    currentValue: 1050,
    yieldEarned: 50,
    apy: 0.06,
    shares: 1000,
    depositedAt: new Date(Date.now() - 40 * 86400 * 1000).toISOString(),
    lockDays: 30,
    daysRemaining: 0,
    isMatured: true,
    earlyWithdrawalPenaltyPct: 0,
};

describe("Slippage tolerance and quote expiry (#1503)", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockGetWithdrawalQuote.mockImplementation((_id: string, amount: number) => ({
            grossAmount: amount,
            penaltyAmount: 0,
            netAmount: amount,
            sharesBurned: amount,
        }));
        mockExecuteVaultDeposit.mockResolvedValue({
            txHash: "abc123deposit",
            explorerUrl: "https://stellar.expert/explorer/testnet/tx/abc123deposit",
            ledger: 100,
            feeCharged: "100",
        });
        mockExecuteVaultWithdraw.mockResolvedValue({
            txHash: "abc123withdraw",
            explorerUrl: "https://stellar.expert/explorer/testnet/tx/abc123withdraw",
            ledger: 101,
            feeCharged: "100",
        });
    });

    it("clamps slippage percentage between 0 and 3% and defaults to 0.5%", () => {
        expect(DEFAULT_SLIPPAGE_PCT).toBe(0.5);
        expect(MAX_SLIPPAGE_PCT).toBe(3);
        expect(clampSlippagePct(1)).toBe(1);
        expect(clampSlippagePct(3)).toBe(3);
        expect(clampSlippagePct(5)).toBe(3);
        expect(clampSlippagePct(-1)).toBe(0);
        expect(clampSlippagePct(Number.NaN)).toBe(DEFAULT_SLIPPAGE_PCT);
    });

    it("computes minimum shares and assets with floor rounding in stroops and display units", () => {
        // 100 USDC (1_000_000_000 stroops) with 0.5% slippage -> 995_000_000 stroops
        expect(applySlippageToStroops(1_000_000_000n, 0.5)).toBe(995_000_000n);
        // 1% slippage -> 990_000_000 stroops
        expect(applySlippageToStroops(1_000_000_000n, 1)).toBe(990_000_000n);
        // 3% slippage -> 970_000_000 stroops
        expect(applySlippageToStroops(1_000_000_000n, 3)).toBe(970_000_000n);

        expect(applySlippageToAmount(100, 0.5)).toBe(99.5);
        expect(applySlippageToAmount(100, 1)).toBe(99);
        expect(applySlippageToAmount(100, 3)).toBe(97);
    });

    it("detects stale quotes (> 30s) and re-quotes before signing", async () => {
        const t0 = 1_700_000_000_000;
        expect(isQuoteStale(t0, t0 + QUOTE_EXPIRY_MS)).toBe(false);
        expect(isQuoteStale(t0, t0 + QUOTE_EXPIRY_MS + 1)).toBe(true);

        const requote = vi.fn().mockResolvedValue(980_000_000n);

        // Fresh quote: does not call requote
        const fresh = await ensureFreshQuote(
            { value: 1_000_000_000n, quotedAtMs: t0 },
            requote,
            t0 + 10_000
        );
        expect(fresh.refreshed).toBe(false);
        expect(fresh.value).toBe(1_000_000_000n);
        expect(requote).not.toHaveBeenCalled();

        // Stale quote (> 30s): calls requote and returns updated value
        const stale = await ensureFreshQuote(
            { value: 1_000_000_000n, quotedAtMs: t0 },
            requote,
            t0 + QUOTE_EXPIRY_MS + 5_000
        );
        expect(stale.refreshed).toBe(true);
        expect(stale.value).toBe(980_000_000n);
        expect(requote).toHaveBeenCalledTimes(1);
    });

    it("DepositModal updates Minimum received and minSharesOut when slippage changes", async () => {
        render(<DepositModal open={true} onClose={vi.fn()} vault={sampleVault} />);

        const amountInput = screen.getByPlaceholderText("0.00");
        fireEvent.change(amountInput, { target: { value: "100" } });

        // Default 0.5% slippage -> 99.50 nVault minimum received
        expect(screen.getByTestId("deposit-minimum-received").textContent).toContain("99.50");

        // Switch to 1% preset -> 99.00 nVault minimum received
        fireEvent.click(screen.getByRole("button", { name: "1%" }));
        expect(screen.getByTestId("deposit-minimum-received").textContent).toContain("99.00");

        // Switch to 3% preset -> 97.00 nVault minimum received
        fireEvent.click(screen.getByRole("button", { name: "3%" }));
        expect(screen.getByTestId("deposit-minimum-received").textContent).toContain("97.00");

        // Submit deposit and verify minSharesOut is 97 (100 * (1 - 0.03))
        const submitBtn = screen.getByRole("button", { name: /Confirm Deposit/i });
        fireEvent.click(submitBtn);

        await waitFor(() => {
            expect(mockExecuteVaultDeposit).toHaveBeenCalledWith(
                expect.objectContaining({
                    amount: 100,
                    minSharesOut: 97,
                })
            );
        });
    });

    it("WithdrawModal updates Minimum received and minAssetsOut when slippage changes", async () => {
        render(<WithdrawModal open={true} onClose={vi.fn()} position={samplePosition} />);

        const amountInput = screen.getByPlaceholderText("0.00");
        fireEvent.change(amountInput, { target: { value: "200" } });

        // Default 0.5% slippage on 200 USDC -> 199.00 USDC
        expect(screen.getByTestId("withdraw-minimum-received").textContent).toContain("199.00");

        // Switch to 1% preset -> 198.00 USDC
        fireEvent.click(screen.getByRole("button", { name: "1%" }));
        expect(screen.getByTestId("withdraw-minimum-received").textContent).toContain("198.00");

        // Submit withdrawal and verify minAssetsOut is 198
        const submitBtn = screen.getByRole("button", { name: /Confirm Withdrawal/i });
        fireEvent.click(submitBtn);

        await waitFor(() => {
            expect(mockExecuteVaultWithdraw).toHaveBeenCalledWith(
                expect.objectContaining({
                    shares: 200,
                    minAssetsOut: 198,
                })
            );
        });
    });
});
