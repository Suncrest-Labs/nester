import { NETWORKS, type NetworkConfig } from "@/lib/networks";

/**
 * The network this build's contract ids belong to.
 *
 * Contract ids are one env set per build (lib/contracts.ts), and the build is
 * pinned to a network by NEXT_PUBLIC_STELLAR_NETWORK (the passphrase). Written
 * out literally so Next.js inlines it into the browser bundle; unset means
 * testnet, matching lib/config.ts.
 */
function buildPassphrase(): string {
  return process.env.NEXT_PUBLIC_STELLAR_NETWORK?.trim() || NETWORKS.testnet.networkPassphrase;
}

/**
 * Thrown when the selected network is not the one this build is deployed for.
 * Building anyway would sign a transaction for one network against contracts
 * that live on another.
 */
export class NetworkNotAvailableError extends Error {
  readonly network: NetworkConfig;

  constructor(network: NetworkConfig) {
    super(
      `${network.name} is not available in this deployment. ` +
        `This build is configured for a different Stellar network.`
    );
    this.name = "NetworkNotAvailableError";
    this.network = network;
  }
}

/** Throws NetworkNotAvailableError unless `network` is the build's network. */
export function assertNetworkMatchesBuild(network: NetworkConfig): void {
  if (network.networkPassphrase !== buildPassphrase()) {
    throw new NetworkNotAvailableError(network);
  }
}
