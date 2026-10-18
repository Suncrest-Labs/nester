export interface BannerLink {
  label: string;
  href: string;
}

export interface NetworkBannerCopy {
  networkId: "testnet" | "mainnet";
  message: string;
  links: BannerLink[];
}

export const NETWORK_BANNER_COPY: Record<"testnet" | "mainnet", NetworkBannerCopy> = {
  testnet: {
    networkId: "testnet",
    message: "You are on Testnet — tokens have no real value",
    links: [],
  },
  mainnet: {
    networkId: "mainnet",
    message:
      "You are on Mainnet — real funds are at risk. Smart contract risk applies, deposits are subject to vault caps, and on-chain transactions are irreversible.",
    links: [
      { label: "Security & Audits", href: "/security" },
      {
        label: "Risk Disclosures",
        href: "/docs/security/mainnet-risk-disclosures.md",
      },
      { label: "Terms", href: "/terms" },
    ],
  },
};

const ACK_KEY_PREFIX = "nester_mainnet_risk_ack:";

export function getMainnetAckStorageKey(walletAddress: string): string {
  return `${ACK_KEY_PREFIX}${walletAddress.trim().toUpperCase()}`;
}

export function hasAcknowledgedMainnetRisk(
  walletAddress: string | null | undefined,
  storage?: Pick<Storage, "getItem">
): boolean {
  if (!walletAddress || !walletAddress.trim()) return false;
  const store =
    storage ?? (typeof window !== "undefined" ? window.localStorage : undefined);
  if (!store) return false;
  try {
    return Boolean(store.getItem(getMainnetAckStorageKey(walletAddress)));
  } catch {
    return false;
  }
}

export function acknowledgeMainnetRisk(
  walletAddress: string,
  storage?: Pick<Storage, "setItem">
): void {
  if (!walletAddress || !walletAddress.trim()) return;
  const store =
    storage ?? (typeof window !== "undefined" ? window.localStorage : undefined);
  if (!store) return;
  try {
    store.setItem(
      getMainnetAckStorageKey(walletAddress),
      new Date().toISOString()
    );
  } catch {
    // Ignore storage write errors in restricted browsing modes
  }
}
