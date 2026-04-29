import { NETWORKS, DEFAULT_NETWORK } from "@/lib/networks";

const getCurrentNetwork = () => {
  if (typeof window !== "undefined") {
    const savedNetwork = localStorage.getItem("nester_network_id");
    if (savedNetwork && (savedNetwork === "testnet" || savedNetwork === "mainnet")) {
      return NETWORKS[savedNetwork];
    }
  }
  return DEFAULT_NETWORK;
};

export const getExplorerTxUrl = (hash: string) => {
  const currentNetwork = getCurrentNetwork();
  return `${currentNetwork.explorerUrl}/tx/${hash}`;
};

export const getExplorerAccountUrl = (address: string) => {
  const currentNetwork = getCurrentNetwork();
  return `${currentNetwork.explorerUrl}/account/${address}`;
};