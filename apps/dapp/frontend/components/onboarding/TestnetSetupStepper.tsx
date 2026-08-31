"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { Check, ExternalLink, Loader2, X } from "lucide-react";

import { useWallet } from "@/components/wallet-provider";
import { usePortfolio } from "@/components/portfolio-provider";
import { useNetwork } from "@/hooks/useNetwork";
import { readWalletIsTestnet } from "@/lib/stellar/wallet-network";
import {
  STEP_ORDER,
  type OnboardingSignals,
  type StepId,
  completedSteps,
  currentStep,
  progress,
  readDismissed,
  writeDismissed,
} from "@/lib/onboarding/testnet-steps";
import { cn } from "@/lib/utils";

/**
 * First-run onboarding through to a funded testnet vault (nester#1127).
 *
 * The four steps are derived from observable state, not from a stored step
 * counter — see lib/onboarding/testnet-steps.ts. That is what makes the flow
 * both auto-advancing and resumable: a reload recomputes the same answer, and
 * a user who arrives already set up is never walked through the setup.
 */

const STEP_COPY: Record<StepId, { title: string; body: string }> = {
  install: {
    title: "Install a Stellar wallet",
    body: "You need a browser wallet to sign transactions. Freighter is the usual choice.",
  },
  network: {
    title: "Switch the wallet to testnet",
    body: "Testnet uses free XLM, so nothing here costs real money.",
  },
  fund: {
    title: "Fund your testnet account",
    body: "Friendbot gives testnet accounts free XLM to pay transaction fees.",
  },
  deposit: {
    title: "Make your first deposit",
    body: "Open a vault and deposit to see the full flow end to end.",
  },
};

const FREIGHTER_URL = "https://www.freighter.app/";

export function TestnetSetupStepper() {
  const { address, isConnected, wallets, walletsLoaded, connect } = useWallet();
  const { positions, balances } = usePortfolio();
  const { currentNetwork } = useNetwork();

  const [dismissed, setDismissed] = useState(true); // assume hidden until read
  const [funding, setFunding] = useState(false);
  const [fundError, setFundError] = useState<string | null>(null);
  const [fundedViaFriendbot, setFundedViaFriendbot] = useState(false);

  // Read the dismissal after mount. Reading during render would differ
  // between the server pass and the client, which React reports as a
  // hydration mismatch.
  useEffect(() => {
    setDismissed(readDismissed());
  }, []);

  const walletAvailable = walletsLoaded && wallets.some((w) => w.isAvailable);

  // The wallet's real network, not the app's own preference. currentNetwork
  // comes from localStorage and says which network the user asked the app to
  // use — it says nothing about what the extension is pointed at, and the two
  // disagree exactly when this step matters. Reading the preference here
  // marked a mainnet user complete and walked them into a Friendbot call that
  // cannot work.
  //
  // null means unproven (no wallet, or a wallet that does not expose
  // getNetwork); completedSteps treats that as not-yet-done unless the
  // account turns out to be funded.
  const [onTestnet, setOnTestnet] = useState<boolean | null>(null);

  useEffect(() => {
    let cancelled = false;
    if (!isConnected || !address) {
      setOnTestnet(null);
      return;
    }
    void readWalletIsTestnet().then((v) => {
      if (!cancelled) setOnTestnet(v);
    });
    return () => {
      cancelled = true;
    };
  }, [isConnected, address]);

  const funded = fundedViaFriendbot || (balances?.XLM ?? 0) > 0;
  const hasDeposit = (positions?.length ?? 0) > 0;

  const signals: OnboardingSignals = useMemo(
    () => ({
      walletAvailable,
      walletConnected: isConnected && Boolean(address),
      onTestnet,
      funded,
      hasDeposit,
    }),
    [walletAvailable, isConnected, address, onTestnet, funded, hasDeposit],
  );

  const done = completedSteps(signals);
  const active = currentStep(signals);
  const pct = Math.round(progress(signals) * 100);

  const dismiss = useCallback(() => {
    setDismissed(true);
    writeDismissed(true);
  }, []);

  const fundAccount = useCallback(async () => {
    if (!address) return;
    setFunding(true);
    setFundError(null);
    try {
      const friendbot = currentNetwork?.friendbotUrl ?? "https://friendbot.stellar.org";
      const res = await fetch(`${friendbot}?addr=${encodeURIComponent(address)}`);
      // Friendbot answers 400 for an account it has already funded, which is
      // a success from the user's point of view — they have XLM either way.
      if (!res.ok && res.status !== 400) {
        throw new Error(`Friendbot returned ${res.status}`);
      }
      setFundedViaFriendbot(true);
    } catch (err) {
      setFundError(err instanceof Error ? err.message : "Could not reach Friendbot");
    } finally {
      setFunding(false);
    }
  }, [address, currentNetwork]);

  // Nothing to show once the user is set up, or once they have dismissed it.
  if (dismissed || active === null) return null;

  return (
    <section
      data-testid="testnet-onboarding"
      data-active-step={active}
      aria-label="Testnet setup"
      className="rounded-xl border border-border bg-card p-5 shadow-sm"
    >
      <header className="mb-4 flex items-start justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold">Get set up on testnet</h2>
          <p className="text-sm text-muted-foreground">
            Four steps to your first deposit. This picks up where you left off.
          </p>
        </div>
        <button
          type="button"
          onClick={dismiss}
          aria-label="Dismiss testnet setup"
          className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <X className="h-4 w-4" />
        </button>
      </header>

      <div
        className="mb-5 h-1.5 w-full overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Setup progress"
      >
        <div className="h-full bg-primary transition-all" style={{ width: `${pct}%` }} />
      </div>

      <ol className="space-y-3">
        {STEP_ORDER.map((step, index) => {
          const isDone = done[step];
          const isActive = step === active;
          return (
            <li
              key={step}
              data-testid={`onboarding-step-${step}`}
              data-state={isDone ? "done" : isActive ? "active" : "todo"}
              className={cn(
                "flex gap-3 rounded-lg border p-3 transition-colors",
                isActive ? "border-primary bg-primary/5" : "border-transparent",
                isDone && "opacity-60",
              )}
            >
              <span
                aria-hidden
                className={cn(
                  "mt-0.5 flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-medium",
                  isDone ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground",
                )}
              >
                {isDone ? <Check className="h-3.5 w-3.5" /> : index + 1}
              </span>

              <div className="min-w-0 flex-1">
                <p className="text-sm font-medium">{STEP_COPY[step].title}</p>
                {isActive && (
                  <>
                    <p className="mt-0.5 text-sm text-muted-foreground">{STEP_COPY[step].body}</p>
                    <div className="mt-2">{renderAction(step)}</div>
                  </>
                )}
              </div>
            </li>
          );
        })}
      </ol>
    </section>
  );

  function renderAction(step: StepId) {
    switch (step) {
      case "install":
        // Two different states share this step: no wallet extension at all,
        // and one installed but not yet connected.
        return walletAvailable ? (
          <button
            type="button"
            onClick={() => {
              const first = wallets.find((w) => w.isAvailable);
              if (first) void connect(first.id);
            }}
            className="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground"
          >
            Connect wallet
          </button>
        ) : (
          <a
            href={FREIGHTER_URL}
            target="_blank"
            rel="noreferrer noopener"
            className="inline-flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm font-medium"
          >
            Install Freighter <ExternalLink className="h-3.5 w-3.5" />
          </a>
        );

      case "network":
        // Switching networks happens inside the wallet extension; the app
        // cannot do it for the user, so this explains rather than acts.
        return (
          <p className="text-sm text-muted-foreground">
            Open your wallet and switch it to <span className="font-medium">Testnet</span>. This
            panel advances on its own once it does.
          </p>
        );

      case "fund":
        return (
          <div className="space-y-1.5">
            <button
              type="button"
              onClick={() => void fundAccount()}
              disabled={funding}
              className="inline-flex items-center gap-2 rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground disabled:opacity-60"
            >
              {funding && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {funding ? "Funding…" : "Fund with Friendbot"}
            </button>
            {fundError && (
              <p role="alert" className="text-sm text-destructive">
                {fundError}. You can also fund the account directly at friendbot.stellar.org.
              </p>
            )}
          </div>
        );

      case "deposit":
        return (
          <a
            href="/dashboard"
            className="inline-flex rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground"
          >
            Open a vault
          </a>
        );
    }
  }
}
