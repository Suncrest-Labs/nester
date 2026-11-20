import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { DepositModal } from "@/components/vault/depositModal";
import type { Vault as VaultType } from "@/lib/types/vault";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({
    address: "GABC1234567890123456789012345678901234567890123456789012345678",
  }),
}));

vi.mock("@/components/portfolio-provider", () => ({
  usePortfolio: () => ({
    getAvailableBalance: () => 1000,
    recordDeposit: vi.fn(),
    refreshBalances: vi.fn(),
  }),
}));

const preflightMock = vi.hoisted(() => ({ result: null as unknown }));
vi.mock("@/hooks/useDepositPreflight", () => ({
  useDepositPreflight: () => ({
    result: preflightMock.result,
    refresh: vi.fn(),
    horizonUrl: "https://horizon-testnet.stellar.org",
    networkPassphrase: "Test SDF Network ; September 2015",
  }),
}));

vi.mock("@/lib/stellar/preflight", async () => {
  const actual = await vi.importActual<typeof import("@/lib/stellar/preflight")>("@/lib/stellar/preflight");
  return { ...actual, addUsdcTrustline: vi.fn() };
});

vi.mock("@/lib/stellar/transaction", () => ({
  executeVaultDeposit: vi.fn(),
  resolveDepositMinSharesOut: vi.fn(),
  previewDeposit: vi.fn().mockResolvedValue({ sharesExpected: BigInt(0), available: false }),
  UserRejectedError: class UserRejectedError extends Error {},
  TransactionFailedError: class TransactionFailedError extends Error {},
  TransactionTimeoutError: class TransactionTimeoutError extends Error {},
  truncateTxHash: (h: string) => h.slice(0, 8),
}));

const mockVault: VaultType = {
  id: "usdc",
  name: "USDC Market",
  description: "Test market",
  marketType: "single",
  tokens: ["USDC"],
  currentApy: 10,
  apyRange: "8-12%",
  tvl: 1000000,
  utilization: 80,
  allocations: [],
  supportedAssets: ["USDC"],
  maturityTerms: "Flexible",
  earlyWithdrawalPenalty: "None",
  apyHistory: [],
  strategies: []
};

describe("DepositModal", () => {
  it("validates amount input", () => {
    render(<DepositModal open vault={mockVault} onClose={() => {}} />);
    const input = screen.getByPlaceholderText("0.00");
    fireEvent.change(input, { target: { value: "0" } });
    expect(input).toHaveValue("0");
  });

  it("shows amount preview when valid value entered", () => {
    render(<DepositModal open vault={mockVault} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "100" } });
    expect(screen.getByText(/estimated annual yield/i)).toBeInTheDocument();
  });

  it("shows fee schedule summary and link to full schedule before deposit", () => {
    render(<DepositModal open vault={mockVault} onClose={() => {}} />);
    const summary = screen.getByTestId("fee-schedule-summary");
    expect(summary).toHaveTextContent(/Tier 1/i);
    expect(summary).toHaveTextContent(/Tier 2/i);
    expect(summary).toHaveTextContent(/Tier 3/i);
    const link = screen.getByTestId("full-fee-schedule-link");
    expect(link).toHaveAttribute("href", "/vaults#fee-schedule");
  });

  it("displays error for insufficient balance", () => {
    render(<DepositModal open vault={mockVault} onClose={() => {}} />);
    fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "5000" } });
    expect(screen.getByText(/insufficient balance/i)).toBeInTheDocument();
  });

  describe("preflight states", () => {
    const fillAmount = () =>
      fireEvent.change(screen.getByPlaceholderText("0.00"), { target: { value: "100" } });
    const confirm = () => screen.getByRole("button", { name: /confirm deposit/i });

    it("enables Confirm Deposit when preflight is ready", () => {
      preflightMock.result = { status: "ready" };
      render(<DepositModal open vault={mockVault} onClose={() => {}} />);
      fillAmount();
      expect(confirm()).toBeEnabled();
      expect(screen.queryByTestId("preflight-banner")).toBeNull();
    });

    it("enables Confirm Deposit while preflight is unavailable", () => {
      preflightMock.result = null;
      render(<DepositModal open vault={mockVault} onClose={() => {}} />);
      fillAmount();
      expect(confirm()).toBeEnabled();
    });

    it("blocks and offers a trustline button when the USDC trustline is missing", () => {
      preflightMock.result = { status: "no_trustline" };
      render(<DepositModal open vault={mockVault} onClose={() => {}} />);
      fillAmount();
      expect(confirm()).toBeDisabled();
      expect(screen.getByTestId("preflight-banner")).toHaveTextContent(/no USDC trustline/i);
      expect(screen.getByRole("button", { name: /add usdc trustline/i })).toBeInTheDocument();
    });

    it("blocks with copy when USDC on the trustline is insufficient", () => {
      preflightMock.result = { status: "insufficient_usdc", balance: 5, required: 100 };
      render(<DepositModal open vault={mockVault} onClose={() => {}} />);
      fillAmount();
      expect(confirm()).toBeDisabled();
      expect(screen.getByTestId("preflight-banner")).toHaveTextContent(/5\.00 USDC.*100\.00 USDC/);
      expect(screen.queryByRole("button", { name: /add usdc trustline/i })).toBeNull();
    });

    it("blocks with copy explaining how much XLM is needed", () => {
      preflightMock.result = { status: "insufficient_xlm", requiredXlm: 0.6, availableXlm: 0.1 };
      render(<DepositModal open vault={mockVault} onClose={() => {}} />);
      fillAmount();
      expect(confirm()).toBeDisabled();
      expect(screen.getByTestId("preflight-banner")).toHaveTextContent(/0\.60 XLM.*0\.10 XLM/);
    });

    it("blocks with copy when the account is not funded", () => {
      preflightMock.result = { status: "no_account" };
      render(<DepositModal open vault={mockVault} onClose={() => {}} />);
      fillAmount();
      expect(confirm()).toBeDisabled();
      expect(screen.getByTestId("preflight-banner")).toHaveTextContent(/not funded/i);
    });
  });
});
