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
export async function readWalletIsTestnet(): Promise<boolean | null> {
  if (typeof window === "undefined") return null;

  try {
    const { StellarWalletsKit } = await import("@creit.tech/stellar-wallets-kit");
    const walletModule = StellarWalletsKit.selectedModule;
    if (!walletModule) return null;

    // getNetwork is optional on the module interface — not every wallet
    // implements it, which is why "unknown" is a distinct answer from
    // "not testnet" rather than being folded into false.
    const getNetwork = (
      walletModule as unknown as {
        getNetwork?: () => Promise<{ networkPassphrase?: string; network?: string }>;
      }
    ).getNetwork;

    if (typeof getNetwork !== "function") return null;

    const net = await getNetwork.call(walletModule);
    const passphrase = net?.networkPassphrase;
    if (!passphrase) return null;

    return passphrase === NETWORKS.testnet.networkPassphrase;
  } catch {
    // An unreadable network is unknown, not mainnet. Reporting false here
    // would strand a testnet user on the "switch to testnet" step forever.
    return null;
  }
}
