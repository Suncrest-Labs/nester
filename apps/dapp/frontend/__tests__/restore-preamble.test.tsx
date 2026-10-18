import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import {
    Account,
    SorobanDataBuilder,
    Transaction,
    Networks,
} from "@stellar/stellar-sdk";

const {
    mockGetAccount,
    mockSimulateTransaction,
    mockSendTransaction,
    mockGetTransaction,
    mockAssembleTransaction,
    mockSignTransaction,
    mockGetAddress,
} = vi.hoisted(() => ({
    mockGetAccount: vi.fn(),
    mockSimulateTransaction: vi.fn(),
    mockSendTransaction: vi.fn(),
    mockGetTransaction: vi.fn(),
    mockAssembleTransaction: vi.fn(),
    mockSignTransaction: vi.fn(),
    mockGetAddress: vi.fn(),
}));

vi.mock("@stellar/stellar-sdk", async (importOriginal) => {
    const actual = await importOriginal<typeof import("@stellar/stellar-sdk")>();
    class MockServer {
        getAccount = mockGetAccount;
        simulateTransaction = mockSimulateTransaction;
        sendTransaction = mockSendTransaction;
        getTransaction = mockGetTransaction;
    }
    return {
        ...actual,
        rpc: {
            ...actual.rpc,
            Server: MockServer,
            assembleTransaction: (...args: unknown[]) => mockAssembleTransaction(...args),
        },
    };
});

vi.mock("@creit.tech/stellar-wallets-kit", () => ({
    StellarWalletsKit: {
        selectedModule: {
            getAddress: mockGetAddress,
            signTransaction: mockSignTransaction,
        },
    },
}));

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
        getWithdrawalQuote: (_id: string, amount: number) => ({
            grossAmount: amount,
            penaltyAmount: 0,
            netAmount: amount,
            sharesBurned: amount,
        }),
        recordWithdrawal: () => ({ penaltyAmount: 0, netAmount: 100 }),
        refreshBalances: vi.fn(),
    }),
}));

import {
    executeVaultWithdraw,
    RESTORE_FOOTPRINT_NOTICE,
} from "@/lib/stellar/transaction";
import { WithdrawModal } from "@/components/vault/withdrawModal";
import type { PortfolioPosition } from "@/components/portfolio-provider";

const WALLET = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF";
const CONTRACT_ID = "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAABSC4";

const samplePosition: PortfolioPosition = {
    id: "pos-restore-1",
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

describe("simulateTransaction restorePreamble handling (#1487)", () => {
    beforeEach(() => {
        vi.clearAllMocks();
        mockGetAddress.mockResolvedValue({ address: WALLET });
    });

    it("produces two signed transactions in order (RestoreFootprint first, then withdraw invocation) when simulateTransaction returns restorePreamble", async () => {
        let seq = 100n;
        mockGetAccount.mockImplementation(async () => new Account(WALLET, (seq++).toString()));

        const restoreSorobanData = new SorobanDataBuilder();
        // First simulation returns a restorePreamble
        mockSimulateTransaction.mockResolvedValueOnce({
            id: "sim-restore",
            latestLedger: 500,
            events: [],
            _parsed: true,
            transactionData: new SorobanDataBuilder(),
            minResourceFee: "50000",
            cost: { cpuInsns: "1000", memBytes: "1000" },
            result: { auth: [], retval: {} },
            restorePreamble: {
                minResourceFee: "75000",
                transactionData: restoreSorobanData,
            },
        });

        // Second simulation (after RestoreFootprint is submitted) succeeds normally
        mockSimulateTransaction.mockResolvedValueOnce({
            id: "sim-ok",
            latestLedger: 501,
            events: [],
            _parsed: true,
            transactionData: new SorobanDataBuilder(),
            minResourceFee: "40000",
            cost: { cpuInsns: "1000", memBytes: "1000" },
            result: { auth: [], retval: {} },
        });

        mockAssembleTransaction.mockImplementation((tx: Transaction) => ({
            build: () => tx,
        }));

        const signedTransactions: Transaction[] = [];
        mockSignTransaction.mockImplementation(async (txXdr: string) => {
            const parsed = new Transaction(txXdr, Networks.TESTNET);
            signedTransactions.push(parsed);
            return { signedTxXdr: txXdr };
        });

        let sendCounter = 0;
        mockSendTransaction.mockImplementation(async () => ({
            status: "PENDING",
            hash: `txhash_${++sendCounter}`,
        }));

        mockGetTransaction.mockImplementation(async () => ({
            status: "SUCCESS",
            ledger: 502,
        }));

        const onRestoreRequired = vi.fn();
        const receipt = await executeVaultWithdraw({
            walletAddress: WALLET,
            vaultId: "conservative",
            contractId: CONTRACT_ID,
            asset: "USDC",
            shares: 100,
            minAssetsOut: 99.5,
            onRestoreRequired,
        });

        // 1. Callback fired with the reactivation notice
        expect(onRestoreRequired).toHaveBeenCalledTimes(1);
        expect(onRestoreRequired).toHaveBeenCalledWith(RESTORE_FOOTPRINT_NOTICE);

        // 2. Exactly two transactions were signed in order:
        //    First: restoreFootprint operation (with BASE_FEE 100 + 75000 = 75100 fee)
        //    Second: invokeHostFunction operation (withdraw)
        expect(signedTransactions).toHaveLength(2);
        expect(signedTransactions[0].operations[0].type).toBe("restoreFootprint");
        expect(signedTransactions[0].fee).toBe("75100");
        expect(signedTransactions[1].operations[0].type).toBe("invokeHostFunction");

        // 3. Receipt confirms restoration occurred and returns the second tx hash
        expect(receipt.restoredFootprint).toBe(true);
        expect(receipt.txHash).toBe("txhash_2");
    });

    it("withdraw modal copy explains that archived positions need to be reactivated first", () => {
        render(<WithdrawModal open={true} onClose={vi.fn()} position={samplePosition} />);

        expect(
            screen.getByText(/Your position needs to be reactivated first/i)
        ).toBeDefined();
    });
});
