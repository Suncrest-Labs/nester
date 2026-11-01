import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

vi.mock("@/components/wallet-provider", () => ({
  useWallet: () => ({ address: "GAWALLET" }),
}));

const fetchDepositsPaused = vi.fn();
const quoteMinShares = vi.fn();
const quoteMinAssets = vi.fn();
const executeSavingsDeposit = vi.fn();
const executeSavingsRedeem = vi.fn();
vi.mock("@/lib/stellar/savings", () => ({
  fetchDepositsPaused: (...a: unknown[]) => fetchDepositsPaused(...a),
  quoteMinShares: (...a: unknown[]) => quoteMinShares(...a),
  quoteMinAssets: (...a: unknown[]) => quoteMinAssets(...a),
  executeSavingsDeposit: (...a: unknown[]) => executeSavingsDeposit(...a),
  executeSavingsRedeem: (...a: unknown[]) => executeSavingsRedeem(...a),
}));

const fetchWalletUsdcBalance = vi.fn();
vi.mock("@/lib/stellar/wallet-balance", () => ({
  fetchWalletUsdcBalance: (...a: unknown[]) => fetchWalletUsdcBalance(...a),
}));

import { describeSavingsError } from "../errors";
import { RateHero } from "../RateHero";
import { SavePanel } from "../SavePanel";
import { PositionCard } from "../PositionCard";
import { AllocationCard } from "../AllocationCard";
import { ReserveCard } from "../ReserveCard";
import { PendingChangesCard } from "../PendingChangesCard";
import type { SavingsSummary } from "@/lib/api/savings";

const summary: SavingsSummary = {
  rate7d: "5.35",
  rate30d: "4.90",
  adapters: [
    {
      address: "CBLEND",
      name: "Blend",
      value: "600.00",
      absCap: "1000.00",
      relCapBps: 6000,
      share: "0.6",
    },
  ],
  pending: [{ id: 1, kind: "RaiseCap", eta: 2_000_000 }],
  reserve: { shares: "10", value: "20.00" },
  sharePrice: "1.0",
  totalAssets: "1000.00",
  idle: "400.00",
  feeBps: 1000,
  timelockSeconds: 259200,
};

function wrap(ui: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
  fetchDepositsPaused.mockResolvedValue(false);
  fetchWalletUsdcBalance.mockResolvedValue({ status: "funded", balance: "1000000.0000000" });
});

describe("RateHero", () => {
  it("renders the rate with the not-guaranteed label", () => {
    render(<RateHero rate7d="5.35" rate30d="4.90" />);
    expect(screen.getByText("5.35%")).toBeTruthy();
    expect(screen.getByText("Realized rate, last 7 days — not guaranteed")).toBeTruthy();
    expect(screen.getByText(/4\.90%/)).toBeTruthy();
  });

  it("renders not-enough-history state", () => {
    render(<RateHero rate7d={null} rate30d={null} />);
    expect(screen.getAllByText("Not enough history yet").length).toBeGreaterThan(0);
    expect(screen.queryByText(/%/)).toBeNull();
  });
});

describe("PositionCard", () => {
  it("shows negative change in red with a minus sign", () => {
    render(<PositionCard position={{ shares: "100", value: "95.00", deposited: "100.00" }} />);
    const change = screen.getByTestId("position-change");
    expect(change.className).toMatch(/red/);
    expect(change.textContent).toContain("−");
    expect(change.textContent).toContain("5.00");
  });

  it("shows positive change with a plus sign and no red", () => {
    render(<PositionCard position={{ shares: "100", value: "101.00", deposited: "100.00" }} />);
    const change = screen.getByTestId("position-change");
    expect(change.className).not.toMatch(/red/);
    expect(change.textContent).toContain("+");
  });

  it("hides change since deposit when deposited is null", () => {
    render(<PositionCard position={{ shares: "100", value: "95.00", deposited: null }} />);
    expect(screen.queryByTestId("position-change")).toBeNull();
    expect(screen.getByText("$95.00")).toBeTruthy();
    expect(screen.getByText("100")).toBeTruthy();
  });
});

describe("AllocationCard", () => {
  it("shows idle and adapter rows with percent and cap usage", () => {
    render(<AllocationCard summary={summary} />);
    expect(screen.getByText("Idle")).toBeTruthy();
    expect(screen.getByText("Blend")).toBeTruthy();
    expect(screen.getByText("60%")).toBeTruthy();
    expect(screen.getByText("40%")).toBeTruthy();
    const bar = screen.getByRole("progressbar", { name: /Blend cap usage/i });
    expect(bar.getAttribute("aria-valuenow")).toBe("100");
  });
});

describe("ReserveCard", () => {
  it("shows value, percent and the locked text", () => {
    render(<ReserveCard reserve={summary.reserve} totalAssets={summary.totalAssets} />);
    expect(screen.getByText("$20.00")).toBeTruthy();
    expect(screen.getByText(/2%/)).toBeTruthy();
    expect(
      screen.getByText("Locked. Can only be used to cover saver losses, after a public timelock.")
    ).toBeTruthy();
  });

  it.each([
    ["4182.30", "1000000.00", "0.42% of total assets"],
    ["0.01", "1000000.00", "<0.01% of total assets"],
    ["0", "1000000.00", "0% of total assets"],
    ["0", "0", "0% of total assets"],
    ["150.00", "1000.00", "15% of total assets"],
  ])("shows a small reserve share with enough precision (%s of %s)", (value, total, text) => {
    render(<ReserveCard reserve={{ shares: "1", value }} totalAssets={total} />);
    expect(screen.getByText(text)).toBeTruthy();
  });
});

describe("PendingChangesCard", () => {
  it("renders nothing when empty", () => {
    const { container } = render(<PendingChangesCard pending={[]} now={0} />);
    expect(container.firstChild).toBeNull();
  });

  it("shows kind and countdown", () => {
    render(<PendingChangesCard pending={summary.pending} now={2_000_000 - 90_000} />);
    expect(screen.getByText("Raise cap")).toBeTruthy();
    expect(screen.getByText(/1d 1h/)).toBeTruthy();
  });

  it("marks ready changes", () => {
    render(<PendingChangesCard pending={summary.pending} now={2_000_001} />);
    expect(screen.getByText(/Ready to execute/)).toBeTruthy();
  });
});

describe("SavePanel", () => {
  const position = { shares: "100", value: "100.00", deposited: null };

  it("disables deposit when paused, with the reason", async () => {
    fetchDepositsPaused.mockResolvedValue(true);
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await waitFor(() => expect(screen.getByText(/Deposits are paused/)).toBeTruthy());
    expect(
      (screen.getByRole("button", { name: "Deposit USDC" }) as HTMLButtonElement).disabled
    ).toBe(true);
  });

  it("keeps withdrawals available while deposits are paused", async () => {
    fetchDepositsPaused.mockResolvedValue(true);
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await waitFor(() => expect(screen.getByText(/Deposits are paused/)).toBeTruthy());
    await userEvent.click(screen.getByRole("tab", { name: "Withdraw" }));
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    expect(
      (screen.getByRole("button", { name: "Withdraw USDC" }) as HTMLButtonElement).disabled
    ).toBe(false);
  });

  it("tabs are keyboard operable", () => {
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    const deposit = screen.getByRole("tab", { name: "Deposit" });
    deposit.focus();
    fireEvent.keyDown(deposit, { key: "ArrowRight" });
    expect(screen.getByRole("tab", { name: "Withdraw" }).getAttribute("aria-selected")).toBe(
      "true"
    );
  });

  it("previews shares and submits a deposit with a slippage-guarded minimum", async () => {
    quoteMinShares.mockResolvedValue(BigInt(995_000_000));
    executeSavingsDeposit.mockResolvedValue({
      txHash: "abc",
      explorerUrl: "https://x/abc",
      ledger: 1,
    });
    wrap(<SavePanel sharePrice="2.0" position={position} />);
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "100");
    expect(screen.getByTestId("save-preview").textContent).toContain("50");
    await userEvent.click(screen.getByRole("button", { name: "Deposit USDC" }));
    await waitFor(() => expect(executeSavingsDeposit).toHaveBeenCalled());
    expect(executeSavingsDeposit).toHaveBeenCalledWith({
      wallet: "GAWALLET",
      amount: "100",
      minShares: BigInt(995_000_000),
    });
    await waitFor(() => expect(screen.getByText(/Deposit confirmed/)).toBeTruthy());
  });

  it("renders the user-facing message when the wallet is on the wrong network", async () => {
    quoteMinShares.mockResolvedValue(BigInt(995_000_000));
    const mismatch = new Error("Switch your wallet to Testnet");
    mismatch.name = "WalletNetworkMismatchError";
    executeSavingsDeposit.mockRejectedValue(mismatch);
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    await userEvent.click(screen.getByRole("button", { name: "Deposit USDC" }));
    const alert = await screen.findByRole("alert");
    expect(within(alert).getByText(/Switch your wallet to Testnet/)).toBeTruthy();
  });

  it("shows a readable message when the contract rejects with DepositsPaused", async () => {
    quoteMinShares.mockRejectedValue(new Error("HostError: Error(Contract, #5)"));
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    await userEvent.click(screen.getByRole("button", { name: "Deposit USDC" }));
    const alert = await screen.findByRole("alert");
    expect(within(alert).getByText(/Deposits are paused/)).toBeTruthy();
  });

  it("shows a readable message when an adapter is unavailable", async () => {
    quoteMinShares.mockRejectedValue(new Error("HostError: Error(Contract, #6)"));
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    await userEvent.click(screen.getByRole("button", { name: "Deposit USDC" }));
    const alert = await screen.findByRole("alert");
    expect(within(alert).getByText(/can't be read right now/)).toBeTruthy();
  });
});

describe("SavePanel review fixes", () => {
  const position = { shares: "100", value: "100.00", deposited: null };

  it("ignores tab switches and disables tabs while a transaction is in flight", async () => {
    quoteMinShares.mockResolvedValue(BigInt(1));
    executeSavingsDeposit.mockReturnValue(new Promise(() => {}));
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    await userEvent.click(screen.getByRole("button", { name: "Deposit USDC" }));
    await screen.findByText("Confirm in your wallet");
    const withdrawTab = screen.getByRole("tab", { name: "Withdraw" }) as HTMLButtonElement;
    expect(withdrawTab.disabled).toBe(true);
    fireEvent.keyDown(screen.getByRole("tab", { name: "Deposit" }), { key: "ArrowRight" });
    expect(screen.getByRole("tab", { name: "Deposit" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByText("Confirm in your wallet")).toBeTruthy();
  });

  it("clamps partial withdraw shares to the position's shares", async () => {
    quoteMinAssets.mockResolvedValue(BigInt(1));
    executeSavingsRedeem.mockResolvedValue({ txHash: "a", explorerUrl: "https://x/a", ledger: 1 });
    wrap(<SavePanel sharePrice="0.1" position={{ shares: "10", value: "100.00", deposited: null }} />);
    await userEvent.click(screen.getByRole("tab", { name: "Withdraw" }));
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "50");
    await userEvent.click(screen.getByRole("button", { name: "Withdraw USDC" }));
    await waitFor(() => expect(executeSavingsRedeem).toHaveBeenCalled());
    expect(executeSavingsRedeem.mock.calls[0][0].shares).toBe(BigInt(100_000_000));
  });

  it("disables submit with a reason when the share price is zero", async () => {
    wrap(<SavePanel sharePrice="0" position={position} />);
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    const button = screen.getByRole("button", { name: "Deposit USDC" }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(screen.getByText(/share price is unavailable/i)).toBeTruthy();
  });

  it("links the block reason to the disabled button", async () => {
    fetchDepositsPaused.mockResolvedValue(true);
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    const reason = await screen.findByText(/Deposits are paused/);
    const button = screen.getByRole("button", { name: "Deposit USDC" });
    expect(button.getAttribute("aria-describedby")).toBe(reason.id);
    expect(reason.id).not.toBe("");
  });
});

describe("SavePanel wallet balance", () => {
  const position = { shares: "100", value: "100.00", deposited: null };

  it("shows the wallet's USDC balance and Max fills the amount", async () => {
    fetchWalletUsdcBalance.mockResolvedValue({ status: "funded", balance: "250.5000000" });
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    const balance = await screen.findByTestId("wallet-balance");
    expect(balance.textContent).toBe("Wallet balance: 250.5 USDC");
    expect(fetchWalletUsdcBalance).toHaveBeenCalledWith("GAWALLET");
    await userEvent.click(screen.getByRole("button", { name: "Max" }));
    expect((screen.getByLabelText("Amount in USDC") as HTMLInputElement).value).toBe("250.5");
    expect(
      (screen.getByRole("button", { name: "Deposit USDC" }) as HTMLButtonElement).disabled
    ).toBe(false);
  });

  it("blocks a deposit larger than the wallet balance", async () => {
    fetchWalletUsdcBalance.mockResolvedValue({ status: "funded", balance: "25.0000000" });
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await screen.findByTestId("wallet-balance");
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "30");
    const button = screen.getByRole("button", { name: "Deposit USDC" }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    const reason = screen.getByText("Your wallet holds 25 USDC. Enter that amount or less.");
    expect(button.getAttribute("aria-describedby")).toBe(reason.id);
  });

  it("keeps the savings max in withdraw mode", async () => {
    fetchWalletUsdcBalance.mockResolvedValue({ status: "funded", balance: "25.0000000" });
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await screen.findByTestId("wallet-balance");
    await userEvent.click(screen.getByRole("tab", { name: "Withdraw" }));
    expect(screen.queryByTestId("wallet-balance")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Max" }));
    expect((screen.getByLabelText("Amount in USDC") as HTMLInputElement).value).toBe("100");
    expect(
      (screen.getByRole("button", { name: "Withdraw USDC" }) as HTMLButtonElement).disabled
    ).toBe(false);
  });

  it("points an unfunded wallet at Friendbot", async () => {
    fetchWalletUsdcBalance.mockResolvedValue({ status: "unfunded" });
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await screen.findByText("Your wallet needs testnet XLM first.");
    const link = screen.getByRole("link", { name: "Fund with Friendbot" });
    expect(link.getAttribute("href")).toBe("https://friendbot.stellar.org/?addr=GAWALLET");
    expect(link.getAttribute("target")).toBe("_blank");
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    expect(
      (screen.getByRole("button", { name: "Deposit USDC" }) as HTMLButtonElement).disabled
    ).toBe(true);
  });

  it.each([
    ["no trustline", { status: "no_trustline" }],
    ["zero balance", { status: "funded", balance: "0.0000000" }],
  ])("points a wallet with %s at the Blend faucet", async (_label, result) => {
    fetchWalletUsdcBalance.mockResolvedValue(result);
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await screen.findByText("Your wallet has no testnet USDC yet.");
    const link = screen.getByRole("link", { name: "Get testnet USDC from the Blend faucet" });
    expect(link.getAttribute("href")).toBe("https://testnet.blend.capital");
    expect(link.getAttribute("target")).toBe("_blank");
    expect(screen.getByTestId("wallet-help").textContent).toMatch(/sends 1,000 test USDC/);
    expect(screen.queryByRole("button", { name: "Max" })).toBeNull();
  });

  it("does not block deposits when the balance can't be loaded", async () => {
    fetchWalletUsdcBalance.mockRejectedValue(new Error("Horizon account lookup failed (500)"));
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await screen.findByText(/couldn't load your wallet balance/i, undefined, { timeout: 4000 });
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    expect(
      (screen.getByRole("button", { name: "Deposit USDC" }) as HTMLButtonElement).disabled
    ).toBe(false);
  });

  it("refetches the wallet balance after a deposit", async () => {
    quoteMinShares.mockResolvedValue(BigInt(1));
    executeSavingsDeposit.mockResolvedValue({ txHash: "a", explorerUrl: "https://x/a", ledger: 1 });
    wrap(<SavePanel sharePrice="1.0" position={position} />);
    await screen.findByTestId("wallet-balance");
    expect(fetchWalletUsdcBalance).toHaveBeenCalledTimes(1);
    await userEvent.type(screen.getByLabelText("Amount in USDC"), "10");
    await userEvent.click(screen.getByRole("button", { name: "Deposit USDC" }));
    await screen.findByText(/Deposit confirmed/);
    await waitFor(() => expect(fetchWalletUsdcBalance).toHaveBeenCalledTimes(2));
  });
});

describe("describeSavingsError", () => {
  const e = (code: string) => new Error(`HostError: Error(Contract, #${code})`);

  it("maps slippage, insufficient shares and insufficient liquidity", () => {
    expect(describeSavingsError(e("2"))).toMatch(/price moved/i);
    expect(describeSavingsError(e("3"))).toMatch(/more shares than you hold/i);
    expect(describeSavingsError(e("4"))).toMatch(/not enough liquid/i);
  });

  it("maps #1 ZeroAmount", () => {
    expect(describeSavingsError(e("1"))).toMatch(/greater than zero/i);
  });

  it("anchors on the first contract error, not nested adapter errors in the log", () => {
    const msg = [
      "HostError: Error(Contract, #5)",
      "Event log (newest first):",
      "   0: [Diagnostic Event] data:[failing with contract error, HostError: Error(Contract, #2)]",
    ].join(" | ");
    expect(describeSavingsError(new Error(msg))).toMatch(/Deposits are paused/i);
    const msg2 = ["HostError: Error(Contract, #1)", "  nested: Error(Contract, #4)"].join(" | ");
    expect(describeSavingsError(new Error(msg2))).toMatch(/greater than zero/i);
  });

  it("does not match two-digit codes", () => {
    expect(describeSavingsError(e("21"))).toContain("#21");
    expect(describeSavingsError(e("42"))).toContain("#42");
    expect(describeSavingsError(e("52"))).toContain("#52");
  });
});

describe("copy", () => {
  it("copy never says guaranteed", () => {
    const { container } = wrap(
      <>
        <RateHero rate7d="5.35" rate30d="4.90" />
        <PositionCard position={{ shares: "1", value: "1.00", deposited: "1.00" }} />
        <AllocationCard summary={summary} />
        <ReserveCard reserve={summary.reserve} totalAssets={summary.totalAssets} />
        <PendingChangesCard pending={summary.pending} now={0} />
        <SavePanel sharePrice="1.0" />
      </>
    );
    const text = (container.textContent ?? "").replace(
      "Realized rate, last 7 days — not guaranteed",
      ""
    );
    expect(text).not.toMatch(/guarantee|only goes up/i);
  });
});
