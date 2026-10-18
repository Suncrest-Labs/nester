import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";

const mockUseNetwork = vi.fn();

vi.mock("@/hooks/useNetwork", () => ({
  useNetwork: () => mockUseNetwork(),
}));

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({
    disconnect: vi.fn(),
    isConnected: false,
  }),
}));

vi.mock("@/components/notifications-provider", () => ({
  useNotifications: () => ({
    addNotification: vi.fn(),
  }),
}));

import {
  NetworkBanner,
  NETWORK_BANNER_COPY,
  MAINNET_RISK_DISCLOSURES_PATH,
  MainnetDepositAcknowledgementModal,
} from "@/components/network/NetworkSelector";
import {
  hasMainnetRiskAcknowledgement,
  setMainnetRiskAcknowledgement,
} from "@/lib/storageKeys";

describe("NetworkBanner and Mainnet risk acknowledgement (#1507)", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.clearAllMocks();
  });

  it("renders Testnet copy when active network is testnet", () => {
    mockUseNetwork.mockReturnValue({
      currentNetwork: { id: "testnet", name: "Testnet" },
      setNetwork: vi.fn(),
    });

    render(<NetworkBanner />);

    expect(screen.getByText(NETWORK_BANNER_COPY.testnet)).toBeInTheDocument();
    expect(screen.queryByText(NETWORK_BANNER_COPY.mainnet)).not.toBeInTheDocument();
  });

  it("renders Mainnet real-funds warning and links to /security, risk disclosures, and /terms", () => {
    mockUseNetwork.mockReturnValue({
      currentNetwork: { id: "mainnet", name: "Mainnet" },
      setNetwork: vi.fn(),
    });

    render(<NetworkBanner />);

    expect(screen.getByText(NETWORK_BANNER_COPY.mainnet)).toBeInTheDocument();
    expect(screen.queryByText(NETWORK_BANNER_COPY.testnet)).not.toBeInTheDocument();

    const securityLink = screen.getByRole("link", { name: /security & audit/i });
    expect(securityLink).toHaveAttribute("href", "/security");

    const disclosuresLink = screen.getByRole("link", { name: /risk disclosures/i });
    expect(disclosuresLink).toHaveAttribute(
      "data-disclosure-path",
      MAINNET_RISK_DISCLOSURES_PATH
    );

    const termsLink = screen.getByRole("link", { name: /terms/i });
    expect(termsLink).toHaveAttribute("href", "/terms");
  });

  it("persists Mainnet risk acknowledgement per wallet address", () => {
    const walletA = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF";
    const walletB = "GBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBWHF";

    expect(hasMainnetRiskAcknowledgement(walletA)).toBe(false);
    expect(hasMainnetRiskAcknowledgement(walletB)).toBe(false);

    setMainnetRiskAcknowledgement(walletA);

    expect(hasMainnetRiskAcknowledgement(walletA)).toBe(true);
    expect(hasMainnetRiskAcknowledgement(walletB)).toBe(false);
  });

  it("renders one-time MainnetDepositAcknowledgementModal and invokes onAcknowledge", () => {
    const wallet = "GCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCWHF";
    const onAck = vi.fn(() => setMainnetRiskAcknowledgement(wallet));
    const onCancel = vi.fn();

    render(
      <MainnetDepositAcknowledgementModal
        open={true}
        onAcknowledge={onAck}
        onCancel={onCancel}
      />
    );

    expect(
      screen.getByText(/Mainnet Real-Funds Risk Acknowledgement/i)
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /I Understand & Continue/i }));

    expect(onAck).toHaveBeenCalledTimes(1);
    expect(hasMainnetRiskAcknowledgement(wallet)).toBe(true);
  });
});
