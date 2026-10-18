import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import {
  NETWORKS,
  resolveDefaultNetwork,
  hasPlaceholderContracts,
  NetworkConfig,
} from "@/lib/networks";
import { NetworkBanner } from "@/components/network/NetworkSelector";

const mockUseNetwork = vi.fn();

vi.mock("@/hooks/useNetwork", () => ({
  useNetwork: () => mockUseNetwork(),
}));

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({ disconnect: vi.fn(), isConnected: false }),
}));

vi.mock("@/components/notifications-provider", () => ({
  useNotifications: () => ({ addNotification: vi.fn() }),
}));

describe("Mainnet Build & Read-Only Smoke Verification (#1545)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("resolves default network to mainnet when NEXT_PUBLIC_STELLAR_NETWORK=mainnet", () => {
    expect(resolveDefaultNetwork("mainnet").id).toBe("mainnet");
    expect(resolveDefaultNetwork("MAINNET").id).toBe("mainnet");
    expect(resolveDefaultNetwork("testnet").id).toBe("testnet");
    expect(resolveDefaultNetwork(undefined).id).toBe("testnet");
  });

  it("detects placeholder contract addresses vs valid 56-character Soroban addresses", () => {
    expect(hasPlaceholderContracts(NETWORKS.mainnet)).toBe(true);

    const configuredMainnet: NetworkConfig = {
      ...NETWORKS.mainnet,
      contracts: {
        vault: "CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75",
        vaultToken: "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
        yieldRegistry: "CBJ4Z3PK5MVPB7TG3FA2KQMZM2RJQ2GV7E2KXQMZM2RJQ2GV7E2KXQMZ",
        allocationStrategy: "CA7QYNF7SOWQ3GLR2BGMZEHXAVIRZA4KVWLTJJFC7MGXUA74P7UJVWLT",
        treasury: "CBLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
        accessControl: "CDW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75",
      },
    };
    expect(hasPlaceholderContracts(configuredMainnet)).toBe(false);
  });

  it("renders the mainnet banner and hides the testnet banner when on mainnet", () => {
    mockUseNetwork.mockReturnValue({
      currentNetwork: NETWORKS.mainnet,
      setNetwork: vi.fn(),
    });

    render(<NetworkBanner />);

    const banner = screen.getByTestId("mainnet-banner");
    expect(banner).toBeInTheDocument();
    expect(banner.textContent).toContain("Stellar Mainnet — live network");
    expect(screen.queryByTestId("testnet-banner")).toBeNull();
  });

  it("catches a broken mainnet banner when network mistakenly falls back to testnet", () => {
    mockUseNetwork.mockReturnValue({
      currentNetwork: NETWORKS.testnet,
      setNetwork: vi.fn(),
    });

    render(<NetworkBanner />);

    // A mainnet smoke assertion expecting data-testid="mainnet-banner" must fail when on testnet
    expect(screen.queryByTestId("mainnet-banner")).toBeNull();
    expect(screen.getByTestId("testnet-banner")).toBeInTheDocument();
  });
});
