import { NETWORKS } from "@/lib/networks";

/**
 * Reads the network the connected wallet is actually pointed at (nester#1127).
 *
 * This is deliberately not the app's own network preference. That value lives
 * in localStorage under `nester_network_id` and says which network the user
 * asked the app to use — it carries no information about what the wallet
 * extension is set to, and the two disagree exactly when it matters.
 *
 * Returns:
 *   - `true`  — the wallet reports testnet
 *   - `false` — the wallet reports something else (mainnet, a custom passphrase)
 *   - `null`  — unknown: no wallet, or a wallet module that does not implement
 *               `getNetwork`. Callers must treat this as unproven rather than
 *               assuming testnet; see lib/onboarding/testnet-steps.ts.
 */
export function resolveNetworkDisplayName(passphraseOrName: string): string {
  const normalized = passphraseOrName.trim();
  if (
    normalized === NETWORKS.mainnet.networkPassphrase ||
    normalized.toLowerCase() === "mainnet" ||
    normalized.toUpperCase() === "PUBLIC"
  ) {
    return "Mainnet";
  }
  if (
    normalized === NETWORKS.testnet.networkPassphrase ||
    normalized.toLowerCase() === "testnet"
  ) {
    return "Testnet";
  }
  return normalized;
}

interface WalletNetworkModuleLike {
  getNetworkDetails?: () => Promise<{
    networkPassphrase?: string;
    network?: string;
  }>;
  getNetwork?: () => Promise<{
    networkPassphrase?: string;
    network?: string;
  }>;
  networkPassphrase?: string;
}

/**
 * Reads the active network passphrase reported by the connected wallet
 * (`getNetworkDetails` or `getNetwork` on the wallet kit module, Freighter API,
 * or injected E2E mock wallet).
 */
export async function readWalletNetworkPassphrase(): Promise<string | null> {
  if (typeof window === "undefined") return null;

  try {
    const win = window as unknown as {
      __NESTER_MOCK_WALLET__?: WalletNetworkModuleLike;
      freighterApi?: WalletNetworkModuleLike;
    };

    if (win.__NESTER_MOCK_WALLET__) {
      const mock = win.__NESTER_MOCK_WALLET__;
      if (typeof mock.getNetworkDetails === "function") {
        const details = await mock.getNetworkDetails();
        if (details?.networkPassphrase) return details.networkPassphrase;
      }
      if (typeof mock.getNetwork === "function") {
        const net = await mock.getNetwork();
        if (net?.networkPassphrase) return net.networkPassphrase;
      }
      if (typeof mock.networkPassphrase === "string" && mock.networkPassphrase) {
        return mock.networkPassphrase;
      }
    }

    try {
      const { StellarWalletsKit } = await import("@creit.tech/stellar-wallets-kit");
      const walletModule = StellarWalletsKit.selectedModule as unknown as
        | WalletNetworkModuleLike
        | undefined;

      if (walletModule) {
        if (typeof walletModule.getNetworkDetails === "function") {
          const details = await walletModule.getNetworkDetails.call(walletModule);
          if (details?.networkPassphrase) return details.networkPassphrase;
        }
        if (typeof walletModule.getNetwork === "function") {
          const net = await walletModule.getNetwork.call(walletModule);
          if (net?.networkPassphrase) return net.networkPassphrase;
        }
      }
    } catch {
      // StellarWalletsKit may not be initialized yet; fall through to freighterApi
    }

    if (win.freighterApi && typeof win.freighterApi.getNetworkDetails === "function") {
      const details = await win.freighterApi.getNetworkDetails();
      if (details?.networkPassphrase) return details.networkPassphrase;
    }

    return null;
  } catch {
    return null;
  }
}

export async function readWalletIsTestnet(): Promise<boolean | null> {
  const passphrase = await readWalletNetworkPassphrase();
  if (!passphrase) return null;
  return passphrase === NETWORKS.testnet.networkPassphrase;
}

