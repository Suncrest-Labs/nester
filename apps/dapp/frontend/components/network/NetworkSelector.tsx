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

import Link from "next/link";
import {
  NETWORK_BANNER_COPY,
  acknowledgeMainnetRisk,
  hasAcknowledgedMainnetRisk,
} from "@/lib/mainnet-acknowledgement";

export function NetworkBanner() {
  const { currentNetwork } = useNetwork();
  const copy = NETWORK_BANNER_COPY[currentNetwork.id] ?? NETWORK_BANNER_COPY.testnet;
  const isMainnet = currentNetwork.id === "mainnet";

  return (
    <div
      data-testid={`network-banner-${currentNetwork.id}`}
      className={`fixed top-0 left-0 right-0 z-60 flex min-h-10 items-center justify-center border-b px-4 py-1.5 ${
        isMainnet
          ? "border-amber-400/50 bg-amber-950 text-amber-50"
          : "border-yellow-200 bg-black text-white"
      }`}
    >
      <div className="flex flex-wrap items-center justify-center gap-x-3 gap-y-1 text-xs font-medium">
        <span className="flex items-center gap-2">
          <AlertTriangle className="h-3.5 w-3.5 shrink-0" />
          <span>{copy.message}</span>
        </span>
        {copy.links.length > 0 && (
          <span className="flex items-center gap-2">
            {copy.links.map((link) => (
              <Link
                key={link.href}
                href={link.href}
                className="underline underline-offset-2 hover: opacity-90"
              >
                {link.label}
              </Link>
            ))}
          </span>
        )}
      </div>
    </div>
  );
}

export function MainnetDepositAcknowledgement({
  walletAddress,
  onAcknowledged,
}: {
  walletAddress: string | null | undefined;
  onAcknowledged?: () => void;
}) {
  const { currentNetwork } = useNetwork();
  const [acknowledged, setAcknowledged] = useState(() =>
    hasAcknowledgedMainnetRisk(walletAddress)
  );

  React.useEffect(() => {
    setAcknowledged(hasAcknowledgedMainnetRisk(walletAddress));
  }, [walletAddress]);

  if (currentNetwork.id !== "mainnet" || !walletAddress || acknowledged) {
    return null;
  }

  const handleAcknowledge = () => {
    acknowledgeMainnetRisk(walletAddress);
    setAcknowledged(true);
    onAcknowledged?.();
  };

  return (
    <div
      data-testid="mainnet-deposit-acknowledgement"
      role="region"
      aria-label="Mainnet Risk Acknowledgement"
      className="rounded-2xl border border-amber-500/40 bg-amber-500/10 p-4 text-xs text-foreground"
    >
      <p className="font-semibold text-amber-900 dark:text-amber-200">
        Mainnet Real-Funds Acknowledgement Required
      </p>
      <p className="mt-1 text-muted-foreground">
        You are depositing real funds on Stellar Mainnet. Smart contract risk applies,
        vault caps limit maximum deposits, and all on-chain transactions are irreversible.
        Review our{" "}
        <Link href="/security" className="underline">
          Security & Audits
        </Link>
        ,{" "}
        <Link href="/docs/security/mainnet-risk-disclosures.md" className="underline">
          Risk Disclosures (docs/security/mainnet-risk-disclosures.md)
        </Link>
        , and{" "}
        <Link href="/terms" className="underline">
          Terms of Service
        </Link>{" "}
        before continuing.
      </p>
      <button
        type="button"
        onClick={handleAcknowledge}
        className="mt-3 rounded-lg bg-amber-600 px-3 py-1.5 font-medium text-white transition-colors hover:bg-amber-500"
      >
        I understand and acknowledge Mainnet risks
      </button>
    </div>
  );
}

