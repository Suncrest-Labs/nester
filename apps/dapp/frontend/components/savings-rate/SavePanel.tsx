"use client";

import { useId, useRef, useState, type KeyboardEvent } from "react";
import Decimal from "decimal.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";

import { Card } from "@/components/ui/card";
import { SlippageControl } from "@/components/slippage-control";
import { useWallet } from "@/components/wallet-provider";
import { useSlippage } from "@/hooks/useSlippage";
import { useWalletUsdcBalance, walletUsdcQueryKey } from "@/hooks/useWalletUsdcBalance";
import config from "@/lib/config";
import { cn } from "@/lib/utils";
import { parseAmountToStroops } from "@/lib/decimal";
import type { SavingsPosition } from "@/lib/api/savings";
import {
  executeSavingsDeposit,
  executeSavingsRedeem,
  fetchDepositsPaused,
  quoteMinAssets,
  quoteMinShares,
} from "@/lib/stellar/savings";
import { DEPOSITS_PAUSED_MESSAGE, describeSavingsError } from "./errors";
import { formatShares, formatUsdc } from "./format";

type Mode = "deposit" | "withdraw";
type Status =
  | { kind: "idle" }
  | { kind: "working" }
  | { kind: "success"; mode: Mode; explorerUrl: string }
  | { kind: "error"; message: string };

const MODES: { id: Mode; label: string }[] = [
  { id: "deposit", label: "Deposit" },
  { id: "withdraw", label: "Withdraw" },
];

const SHARE_DECIMALS = 7;

const BLEND_FAUCET_URL = "https://testnet.blend.capital";

interface SavePanelProps {
  /** USDC per share, as a decimal string. */
  sharePrice: string;
  position?: SavingsPosition;
}

function toStroops(value: Decimal): bigint {
  return BigInt(value.times(new Decimal(10).pow(SHARE_DECIMALS)).toFixed(0, Decimal.ROUND_DOWN));
}

export function SavePanel({ sharePrice, position }: SavePanelProps) {
  const { address } = useWallet();
  const queryClient = useQueryClient();
  const { bps, setBps } = useSlippage();
  const [mode, setMode] = useState<Mode>("deposit");
  const [amount, setAmount] = useState("");
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const tabRefs = useRef<Record<Mode, HTMLButtonElement | null>>({ deposit: null, withdraw: null });
  const baseId = useId();

  const paused = useQuery({
    queryKey: ["savings", "depositsPaused", address],
    queryFn: () => fetchDepositsPaused(address as string),
    enabled: !!address,
    staleTime: 30_000,
  });
  const depositsPaused = paused.data === true;

  const walletBalance = useWalletUsdcBalance(address);
  const wallet = walletBalance.data;
  const walletUsdc = wallet?.status === "funded" ? new Decimal(wallet.balance) : null;
  const walletHelp =
    mode !== "deposit" || !address || !wallet
      ? null
      : wallet.status === "unfunded"
        ? "unfunded"
        : !walletUsdc || walletUsdc.isZero()
          ? "no_usdc"
          : null;
  const walletHelpReason =
    walletHelp === "unfunded"
      ? "Your wallet needs testnet XLM first."
      : walletHelp === "no_usdc"
        ? "Your wallet has no testnet USDC yet."
        : null;

  const parsed = amount.trim() === "" ? null : parseAmountToStroops(amount, SHARE_DECIMALS);
  const amountError = parsed && !parsed.valid ? parsed.error?.message : undefined;
  const validAmount = parsed?.valid ? new Decimal(amount.trim()) : null;
  const price = new Decimal(sharePrice);

  const maxValue = position ? new Decimal(position.value) : new Decimal(0);
  const overBalance = mode === "withdraw" && validAmount !== null && validAmount.gt(maxValue);
  const overWallet =
    mode === "deposit" && validAmount !== null && walletUsdc !== null && validAmount.gt(walletUsdc);

  const shares =
    validAmount && !price.isZero() ? validAmount.div(price).toDecimalPlaces(SHARE_DECIMALS, Decimal.ROUND_DOWN) : null;

  let blockReason: string | null = null;
  if (!address) blockReason = "Connect your wallet to continue.";
  else if (mode === "deposit" && depositsPaused) blockReason = DEPOSITS_PAUSED_MESSAGE;
  else if (walletHelpReason) blockReason = walletHelpReason;
  else if (mode === "withdraw" && !position) blockReason = "You have no savings to withdraw.";
  else if (price.lte(0)) blockReason = "The share price is unavailable right now. Try again shortly.";
  else if (overBalance) blockReason = `You can withdraw up to ${formatUsdc(position?.value ?? "0")}.`;
  else if (overWallet && walletUsdc)
    blockReason = `Your wallet holds ${formatShares(walletUsdc.toFixed())} USDC. Enter that amount or less.`;

  const working = status.kind === "working";
  const canSubmit = !!validAmount && !blockReason && !working;

  function switchMode(next: Mode) {
    if (working) return;
    setMode(next);
    setStatus({ kind: "idle" });
    tabRefs.current[next]?.focus();
  }

  function onTabKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (working) return;
    const index = MODES.findIndex((m) => m.id === mode);
    let next = index;
    if (event.key === "ArrowRight") next = (index + 1) % MODES.length;
    else if (event.key === "ArrowLeft") next = (index + MODES.length - 1) % MODES.length;
    else if (event.key === "Home") next = 0;
    else if (event.key === "End") next = MODES.length - 1;
    else return;
    event.preventDefault();
    switchMode(MODES[next].id);
  }

  async function submit() {
    if (!canSubmit || !address || !validAmount) return;
    setStatus({ kind: "working" });
    try {
      let receipt;
      if (mode === "deposit") {
        const minShares = await quoteMinShares(address, amount.trim(), bps);
        receipt = await executeSavingsDeposit({ wallet: address, amount: amount.trim(), minShares });
      } else {
        const held = toStroops(new Decimal(position?.shares ?? "0"));
        const computed = validAmount.gte(maxValue) ? held : toStroops(shares as Decimal);
        const redeemShares = computed < held ? computed : held;
        const minAssets = await quoteMinAssets(address, redeemShares, bps);
        receipt = await executeSavingsRedeem({ wallet: address, shares: redeemShares, minAssets });
      }
      setAmount("");
      setStatus({ kind: "success", mode, explorerUrl: receipt.explorerUrl });
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ["savings"] }),
        queryClient.invalidateQueries({ queryKey: walletUsdcQueryKey(address) }),
      ]);
    } catch (err) {
      setStatus({ kind: "error", message: describeSavingsError(err) });
    }
  }

  const inputId = `${baseId}-amount`;
  const actionLabel = mode === "deposit" ? "Deposit USDC" : "Withdraw USDC";

  return (
    <Card className="bg-card">
      <div role="tablist" aria-label="Savings action" className="flex gap-1 rounded-full border border-border p-1">
        {MODES.map((m) => (
          <button
            key={m.id}
            ref={(el) => {
              tabRefs.current[m.id] = el;
            }}
            type="button"
            role="tab"
            id={`${baseId}-tab-${m.id}`}
            disabled={working}
            aria-selected={mode === m.id}
            aria-controls={`${baseId}-panel`}
            tabIndex={mode === m.id ? 0 : -1}
            onClick={() => switchMode(m.id)}
            onKeyDown={onTabKeyDown}
            className={cn(
              "flex-1 rounded-full px-4 py-2 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-foreground",
              mode === m.id
                ? "bg-foreground text-background"
                : "text-foreground/70 hover:text-foreground"
            )}
          >
            {m.label}
          </button>
        ))}
      </div>

      <div
        role="tabpanel"
        id={`${baseId}-panel`}
        aria-labelledby={`${baseId}-tab-${mode}`}
        className="mt-5"
      >
        <label htmlFor={inputId} className="text-sm font-medium text-foreground">
          Amount in USDC
        </label>
        <div
          className={cn(
            "mt-2 flex items-center rounded-xl border px-4 py-3",
            amountError ? "border-destructive" : "border-border"
          )}
        >
          <input
            id={inputId}
            type="text"
            inputMode="decimal"
            autoComplete="off"
            placeholder="0.00"
            value={amount}
            disabled={working}
            aria-invalid={!!amountError}
            aria-describedby={`${baseId}-hint`}
            onChange={(event) => {
              setAmount(event.target.value);
              if (status.kind !== "working") setStatus({ kind: "idle" });
            }}
            className="w-full bg-transparent font-heading text-2xl font-light tabular-nums outline-none placeholder:text-muted-foreground/50"
          />
          {mode === "deposit" && walletUsdc && walletUsdc.gt(0) && (
            <button
              type="button"
              disabled={working}
              onClick={() => setAmount(walletUsdc.toFixed())}
              className="ml-3 rounded-full border border-border px-3 py-1 text-xs font-medium text-foreground/70 hover:text-foreground"
            >
              Max
            </button>
          )}
          {mode === "withdraw" && position && (
            <button
              type="button"
              disabled={working}
              onClick={() => setAmount(new Decimal(position.value).toFixed())}
              className="ml-3 rounded-full border border-border px-3 py-1 text-xs font-medium text-foreground/70 hover:text-foreground"
            >
              Max
            </button>
          )}
        </div>

        <p id={`${baseId}-hint`} className="mt-2 min-h-5 text-xs text-muted-foreground">
          {amountError ? (
            <span className="text-destructive">{amountError}</span>
          ) : mode === "withdraw" && position ? (
            <>Available: {formatUsdc(position.value)}</>
          ) : mode === "deposit" && walletUsdc ? (
            <span data-testid="wallet-balance">Wallet balance: {formatShares(walletUsdc.toFixed())} USDC</span>
          ) : mode === "deposit" && walletBalance.isError ? (
            <>We couldn&apos;t load your wallet balance.</>
          ) : null}
        </p>

        <p data-testid="save-preview" className="mt-3 text-sm text-foreground/80">
          {validAmount && shares
            ? mode === "deposit"
              ? `You receive about ${formatShares(shares.toFixed())} shares`
              : `You receive about ${formatUsdc(validAmount.toFixed())}`
            : "Enter an amount to see a preview"}
        </p>

        <SlippageControl bps={bps} onChange={setBps} disabled={working} />

        {blockReason && (
          <p id={`${baseId}-reason`} className="mt-4 text-sm text-foreground/80">
            {blockReason}
          </p>
        )}
        {walletHelpReason && address && blockReason === walletHelpReason && (
          <p data-testid="wallet-help" className="mt-1 text-sm text-muted-foreground">
            {walletHelp === "unfunded" ? (
              <a
                href={`${config.friendbotUrl.replace(/\/$/, "")}/?addr=${encodeURIComponent(address)}`}
                target="_blank"
                rel="noopener noreferrer"
                className="underline underline-offset-2 hover:text-foreground"
              >
                Fund with Friendbot
              </a>
            ) : (
              <>
                <a
                  href={BLEND_FAUCET_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="underline underline-offset-2 hover:text-foreground"
                >
                  Get testnet USDC from the Blend faucet
                </a>
                . One signature adds USDC to your wallet and sends 1,000 test USDC.
              </>
            )}
          </p>
        )}

        <button
          type="button"
          disabled={!canSubmit}
          aria-describedby={blockReason ? `${baseId}-reason` : undefined}
          onClick={submit}
          className="mt-5 inline-flex w-full items-center justify-center gap-2 rounded-full bg-foreground px-5 py-3 text-sm font-medium text-background transition-opacity disabled:cursor-not-allowed disabled:opacity-40"
        >
          {working && <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />}
          {working ? "Confirm in your wallet" : actionLabel}
        </button>

        <div aria-live="polite" className="mt-3 text-sm">
          {status.kind === "success" && (
            <p className="text-foreground">
              {status.mode === "deposit" ? "Deposit" : "Withdrawal"} confirmed.{" "}
              <a
                href={status.explorerUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="underline underline-offset-2"
              >
                View transaction
              </a>
            </p>
          )}
        </div>
        {status.kind === "error" && (
          <p role="alert" className="mt-3 text-sm text-destructive">
            {status.message}
          </p>
        )}
      </div>
    </Card>
  );
}
