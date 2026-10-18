"use client";

import React, { useState } from "react";
import { useNetwork } from "@/hooks/useNetwork";
import { useWallet } from "@/components/wallet-provider";
import { useNotifications } from "@/components/notifications-provider";
import { ChevronDown, AlertTriangle } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";

export function NetworkSelector() {
  const { currentNetwork, setNetwork } = useNetwork();
  const { disconnect, isConnected } = useWallet();
  const { addNotification } = useNotifications();
  const [isOpen, setIsOpen] = useState(false);

  const handleSwitch = (networkId: 'testnet' | 'mainnet') => {
    if (networkId === currentNetwork.id) {
      setIsOpen(false);
      return;
    }

    // 1. Disconnect wallet session
    if (isConnected) {
      disconnect();
    }

    // 2 & 3. Clear cached data and update network state (handled in setNetwork)
    setNetwork(networkId);

    // 4 & 5. Show confirmation message
    addNotification({
      title: "Network Switched",
      message: `Switching to ${networkId === 'testnet' ? 'Testnet' : 'Mainnet'}. Please reconnect your wallet.`,
      type: "info",
    });

    setIsOpen(false);
  };

  return (
    <div className="relative inline-block text-left">
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="flex items-center gap-2 rounded-full border border-border bg-white dark:bg-[#100F0F] px-3 py-1.5 text-sm font-medium transition-colors hover:bg-secondary"
      >
        <span className="relative flex h-2.5 w-2.5">
          {currentNetwork.id === 'testnet' ? (
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-yellow-400"></span>
          ) : (
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-emerald-500"></span>
          )}
        </span>
        <span className="capitalize">{currentNetwork.id}</span>
        <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${isOpen ? "rotate-180" : ""}`} />
      </button>

      <AnimatePresence>
        {isOpen && (
          <>
            <div
              className="fixed inset-0 z-40" 
              onClick={() => setIsOpen(false)} 
            />
            <motion.div
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 8 }}
              className="absolute right-0 z-50 mt-2 w-48 origin-top-right rounded-xl border border-border bg-white dark:bg-[#100F0F] p-1 shadow-xl shadow-black/15 ring-1 ring-black/5" // Enhanced shadow and border
            >
              <div className="flex flex-col space-y-1">
                <button
                  onClick={() => handleSwitch('testnet')}
                  className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors ${
                    currentNetwork.id === 'testnet' ? 'bg-secondary font-medium' : 'hover:bg-secondary/50'
                  }`}
                >
                  <span className="h-2 w-2 rounded-full bg-yellow-400"></span>
                  Testnet
                </button>
                <button
                  onClick={() => handleSwitch('mainnet')}
                  className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors ${
                    currentNetwork.id === 'mainnet' ? 'bg-secondary font-medium' : 'hover:bg-secondary/50'
                  }`}
                >
                  <span className="h-2 w-2 rounded-full bg-emerald-500"></span>
                  Mainnet
                </button>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}

export const NETWORK_BANNER_COPY = {
  testnet: "You are on Testnet — tokens have no real value",
  mainnet:
    "You are on Mainnet — real funds are at risk. Smart contract risk applies, deposits are bounded by the vault cap, and transactions are irreversible.",
} as const;

export const MAINNET_RISK_DISCLOSURES_PATH = "docs/security/mainnet-risk-disclosures.md";
export const MAINNET_RISK_DISCLOSURES_HREF = `/${MAINNET_RISK_DISCLOSURES_PATH}`;

export function NetworkBanner() {
  const { currentNetwork } = useNetwork();

  if (currentNetwork.id === "testnet") {
    return (
      <div className="fixed top-0 left-0 right-0 z-60 flex h-10 items-center justify-center border-b border-yellow-200 bg-black px-4">
        <p className="flex items-center gap-2 text-xs font-medium text-white">
          <AlertTriangle className="h-3.5 w-3.5" />
          {NETWORK_BANNER_COPY.testnet}
        </p>
      </div>
    );
  }

  return (
    <div className="fixed top-0 left-0 right-0 z-60 flex min-h-10 items-center justify-center border-b border-emerald-500/30 bg-black px-4 py-1.5">
      <p className="flex flex-wrap items-center justify-center gap-2 text-xs font-medium text-white">
        <AlertTriangle className="h-3.5 w-3.5 text-amber-400" />
        <span>{NETWORK_BANNER_COPY.mainnet}</span>
        <a href="/security" className="underline hover:text-emerald-300">
          Security &amp; Audit
        </a>
        <span aria-hidden="true">·</span>
        <a
          href={MAINNET_RISK_DISCLOSURES_HREF}
          data-disclosure-path={MAINNET_RISK_DISCLOSURES_PATH}
          className="underline hover:text-emerald-300"
        >
          Risk Disclosures
        </a>
        <span aria-hidden="true">·</span>
        <a href="/terms" className="underline hover:text-emerald-300">
          Terms
        </a>
      </p>
    </div>
  );
}

export function MainnetDepositAcknowledgementModal({
  open,
  onAcknowledge,
  onCancel,
}: {
  open: boolean;
  onAcknowledge: () => void;
  onCancel: () => void;
}) {
  if (!open) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="mainnet-risk-ack-title"
      className="fixed inset-0 z-[110] flex items-center justify-center bg-black/60 px-4"
    >
      <div className="w-full max-w-md rounded-2xl border border-border bg-white p-6 shadow-2xl dark:bg-[#100F0F]">
        <div className="flex items-center gap-2 text-amber-600 dark:text-amber-400">
          <AlertTriangle className="h-5 w-5" />
          <h3 id="mainnet-risk-ack-title" className="text-base font-semibold text-foreground">
            Mainnet Real-Funds Risk Acknowledgement
          </h3>
        </div>
        <p className="mt-3 text-sm text-muted-foreground">
          You are about to deposit real funds on Stellar Mainnet. Please confirm that you understand:
        </p>
        <ul className="mt-3 list-disc space-y-1.5 pl-5 text-xs text-muted-foreground">
          <li>Transactions are irreversible once signed and confirmed on-chain.</li>
          <li>Smart contract and external yield-adapter risks apply to all deposited assets.</li>
          <li>Deposits are bounded by each vault&apos;s on-chain deposit cap.</li>
        </ul>
        <p className="mt-3 text-xs text-muted-foreground">
          Review{" "}
          <a href="/security" className="underline">
            /security
          </a>
          ,{" "}
          <a href={MAINNET_RISK_DISCLOSURES_HREF} className="underline">
            {MAINNET_RISK_DISCLOSURES_PATH}
          </a>
          , and{" "}
          <a href="/terms" className="underline">
            /terms
          </a>{" "}
          before proceeding.
        </p>
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="rounded-lg border border-border px-4 py-2 text-xs font-medium text-foreground hover:bg-secondary"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onAcknowledge}
            className="rounded-lg bg-foreground px-4 py-2 text-xs font-semibold text-background hover:opacity-90"
          >
            I Understand &amp; Continue
          </button>
        </div>
      </div>
    </div>
  );
}

