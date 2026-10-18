import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import {
  MainnetDepositAcknowledgement,
  NetworkBanner,
} from "@/components/network/NetworkSelector";
import {
  acknowledgeMainnetRisk,
  hasAcknowledgedMainnetRisk,
} from "@/lib/mainnet-acknowledgement";

const mockUseNetwork = vi.fn();

vi.mock("@/hooks/useNetwork", () => ({
  useNetwork: () => mockUseNetwork(),
}));

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({
    disconnect: vi.fn(),
    isConnected: true,
  }),
}));

vi.mock("@/components/notifications-provider", () => ({
  useNotifications: () => ({
    addNotification: vi.fn(),
  }),
}));

describe("NetworkBanner and MainnetDepositAcknowledgement (#1507)", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("renders Testnet banner copy when current network is testnet", () => {
    mockUseNetwork.mockReturnValue({
      currentNetwork: { id: "testnet", name: "Testnet" },
      setNetwork: vi.fn(),
    });

    render(<NetworkBanner />);

    expect(
      screen.getByText(/You are on Testnet — tokens have no real value/i)
    ).toBeInTheDocument();
  });

  it("renders Mainnet real-funds warning and links to /security, risk disclosures, and terms", () => {
    mockUseNetwork.mockReturnValue({
      currentNetwork: { id: "mainnet", name: "Mainnet" },
      setNetwork: vi.fn(),
    });

    render(<NetworkBanner />);

    expect(
      screen.getByText(
        /You are on Mainnet — real funds are at risk\. Smart contract risk applies, deposits are subject to vault caps, and on-chain transactions are irreversible\./i
      )
    ).toBeInTheDocument();

    expect(
      screen.getByRole("link", { name: /Security & Audits/i })
    ).toHaveAttribute("href", "/security");
    expect(
      screen.getByRole("link", { name: /Risk Disclosures/i })
    ).toHaveAttribute("href", "/docs/security/mainnet-risk-disclosures.md");
    expect(screen.getByRole("link", { name: /Terms/i })).toHaveAttribute(
      "href",
      "/terms"
    );
  });

  it("persists Mainnet risk acknowledgement per wallet address", () => {
    const walletA = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF";
    const walletB = "GBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBWJX";

    expect(hasAcknowledgedMainnetRisk(walletA)).toBe(false);
    expect(hasAcknowledgedMainnetRisk(walletB)).toBe(false);

    acknowledgeMainnetRisk(walletA);

    expect(hasAcknowledgedMainnetRisk(walletA)).toBe(true);
    expect(hasAcknowledgedMainnetRisk(walletB)).toBe(false);
  });

  it("renders one-time Mainnet acknowledgement prompt and hides after acknowledgement", () => {
    mockUseNetwork.mockReturnValue({
      currentNetwork: { id: "mainnet", name: "Mainnet" },
      setNetwork: vi.fn(),
    });

    const wallet = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF";
    const onAck = vi.fn();

    render(
      <MainnetDepositAcknowledgement
        walletAddress={wallet}
        onAcknowledged={onAck}
      />
    );

    expect(
      screen.getByTestId("mainnet-deposit-acknowledgement")
    ).toBeInTheDocument();

    fireEvent.click(
      screen.getByRole("button", {
        name: /I understand and acknowledge Mainnet risks/i,
      })
    );

    expect(onAck).toHaveBeenCalledTimes(1);
    expect(hasAcknowledgedMainnetRisk(wallet)).toBe(true);
    expect(
      screen.queryByTestId("mainnet-deposit-acknowledgement")
    ).not.toBeInTheDocument();
  });
});
