import { NETWORKS, DEFAULT_NETWORK } from "@/lib/networks";
import { readNetworkId } from "@/lib/storageKeys";

// #1233: readNetworkId routes through safeStorage, so a throwing accessor
// (private browsing, full quota) falls back to DEFAULT_NETWORK rather than
// an unguarded read crashing whichever component calls the exports below.
const getCurrentNetwork = () => {
  const savedNetwork = readNetworkId();
  return savedNetwork ? NETWORKS[savedNetwork] : DEFAULT_NETWORK;
};

export const getExplorerTxUrl = (hash: string) => {
  const currentNetwork = getCurrentNetwork();
  return `${currentNetwork.explorerUrl}/transactions/${hash}`;
};

export const getExplorerAccountUrl = (address: string) => {
  const currentNetwork = getCurrentNetwork();
  return `${currentNetwork.explorerUrl}/accounts/${address}`;
};