import { afterEach, describe, expect, it, vi } from "vitest";

import { NETWORKS } from "@/lib/networks";
import { writeNetworkId } from "@/lib/storageKeys";
import { buildDepositTransaction } from "@/lib/stellar/transaction";
import {
  NetworkNotAvailableError,
  assertNetworkMatchesBuild,
} from "@/lib/stellar/network-guard";

/**
 * Contract ids are one env set per build (lib/contracts.ts); the Network
 * selector only changes the passphrase. Selecting a network the build is not
 * configured for would sign a transaction for one network against contracts
 * deployed on another, so the money path must refuse instead.
 */

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("assertNetworkMatchesBuild", () => {
  it("accepts the network the build is configured for", () => {
    vi.stubEnv("NEXT_PUBLIC_STELLAR_NETWORK", NETWORKS.testnet.networkPassphrase);
    expect(() => assertNetworkMatchesBuild(NETWORKS.testnet)).not.toThrow();

    vi.stubEnv("NEXT_PUBLIC_STELLAR_NETWORK", NETWORKS.mainnet.networkPassphrase);
    expect(() => assertNetworkMatchesBuild(NETWORKS.mainnet)).not.toThrow();
  });

  it("treats an unset build network as testnet", () => {
    vi.stubEnv("NEXT_PUBLIC_STELLAR_NETWORK", "");
    expect(() => assertNetworkMatchesBuild(NETWORKS.testnet)).not.toThrow();
  });

  it("refuses mainnet on a testnet build", () => {
    vi.stubEnv("NEXT_PUBLIC_STELLAR_NETWORK", NETWORKS.testnet.networkPassphrase);
    expect(() => assertNetworkMatchesBuild(NETWORKS.mainnet)).toThrow(NetworkNotAvailableError);
    expect(() => assertNetworkMatchesBuild(NETWORKS.mainnet)).toThrow(/Mainnet is not available/);
  });

  it("refuses testnet on a mainnet build", () => {
    vi.stubEnv("NEXT_PUBLIC_STELLAR_NETWORK", NETWORKS.mainnet.networkPassphrase);
    expect(() => assertNetworkMatchesBuild(NETWORKS.testnet)).toThrow(NetworkNotAvailableError);
    expect(() => assertNetworkMatchesBuild(NETWORKS.testnet)).toThrow(/Testnet is not available/);
  });

  it("refuses everything when the build passphrase is unrecognised", () => {
    vi.stubEnv("NEXT_PUBLIC_STELLAR_NETWORK", "Some Other Network ; 2030");
    expect(() => assertNetworkMatchesBuild(NETWORKS.testnet)).toThrow(NetworkNotAvailableError);
    expect(() => assertNetworkMatchesBuild(NETWORKS.mainnet)).toThrow(NetworkNotAvailableError);
  });
});

describe("transaction builders", () => {
  it("refuse to build for a network the build is not configured for", async () => {
    vi.stubEnv("NEXT_PUBLIC_STELLAR_NETWORK", NETWORKS.testnet.networkPassphrase);
    writeNetworkId("mainnet");

    await expect(
      buildDepositTransaction({
        walletAddress: `G${"A".repeat(55)}`,
        contractId: `C${"A".repeat(55)}`,
        amount: 1,
      })
    ).rejects.toThrow(NetworkNotAvailableError);

    writeNetworkId("testnet");
  });
});
