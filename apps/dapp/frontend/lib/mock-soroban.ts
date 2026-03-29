"use client";

import { getExplorerUrl } from "@/components/portfolio-provider";
import { config } from "@/lib/config";

export async function buildMockTransactionXdr(
    address: string,
    memo: string
) {
    const StellarSdk = await import("@stellar/stellar-sdk");
    const account = new StellarSdk.Account(address, "1");
    const tx = new StellarSdk.TransactionBuilder(account, {
        fee: StellarSdk.BASE_FEE,
        networkPassphrase: config.stellarNetwork,
    })
        .addOperation(
            StellarSdk.Operation.manageData({
                name: "nester_action",
                value: memo.slice(0, 64),
            })
        )
        .addMemo(StellarSdk.Memo.text(memo.slice(0, 28)))
        .setTimeout(0)
        .build();

    return tx.toXDR();
}

export async function signWithWalletOrMock(txXdr: string) {
    const StellarSdk = await import("@stellar/stellar-sdk");

    try {
        const { StellarWalletsKit } = await import("@creit.tech/stellar-wallets-kit");
        const selectedModule = (StellarWalletsKit as unknown as { selectedModule?: { signTransaction?: (...args: unknown[]) => Promise<unknown> } }).selectedModule;

        if (selectedModule?.signTransaction) {
            const result = await selectedModule.signTransaction(txXdr, {
                networkPassphrase: config.stellarNetwork,
            });
            const signedTxXdr =
                typeof result === "string"
                    ? result
                    : (result as { signedTxXdr?: string })?.signedTxXdr ?? txXdr;

            return { signedTxXdr, walletPopupUsed: true };
        }
    } catch {
        // Fall back to a fully mocked signature path when a wallet cannot sign.
    }

    await new Promise((resolve) => window.setTimeout(resolve, 900));
    return { signedTxXdr: txXdr, walletPopupUsed: false };
}

export async function simulateSubmission() {
    await new Promise((resolve) => window.setTimeout(resolve, 1200));
    const alphabet = "abcdef0123456789";
    const txHash = Array.from(
        { length: 64 },
        () => alphabet[Math.floor(Math.random() * alphabet.length)]
    ).join("");

    return {
        txHash,
        explorerUrl: getExplorerUrl(txHash),
    };
}

