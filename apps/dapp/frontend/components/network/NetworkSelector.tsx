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

  const handleKeyDown = (e: React.KeyboardEvent, networkId: 'testnet' | 'mainnet') => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      handleSwitch(networkId);
    }
  };

  return (
    <div className="relative inline-block text-left">
      <button
        onClick={() => setIsOpen(!isOpen)}
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        aria-controls="network-dropdown"
        className="flex items-center gap-2 rounded-full border border-border bg-white px-3 py-1.5 text-sm font-medium transition-colors hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
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
              id="network-dropdown"
              role="listbox"
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: 8 }}
              className="absolute right-0 z-50 mt-2 w-48 origin-top-right rounded-xl border border-border bg-white p-1 shadow-xl shadow-black/15 ring-1 ring-black/5" // Enhanced shadow and border
            >
              <div className="flex flex-col space-y-1">
                <div
                  role="option"
                  tabIndex={0}
                  aria-selected={currentNetwork.id === 'testnet'}
                  onClick={() => handleSwitch('testnet')}
                  onKeyDown={(e) => handleKeyDown(e, 'testnet')}
                  className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary ${
                    currentNetwork.id === 'testnet' ? 'bg-secondary font-medium' : 'hover:bg-secondary/50'
                  }`}
                >
                  <span className="h-2 w-2 rounded-full bg-yellow-400"></span>
                  Testnet
                </div>
                <div
                  role="option"
                  tabIndex={0}
                  aria-selected={currentNetwork.id === 'mainnet'}
                  onClick={() => handleSwitch('mainnet')}
                  onKeyDown={(e) => handleKeyDown(e, 'mainnet')}
                  className={`flex items-center gap-2 rounded-lg px-3 py-2 text-sm transition-colors cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary ${
                    currentNetwork.id === 'mainnet' ? 'bg-secondary font-medium' : 'hover:bg-secondary/50'
                  }`}
                >
                  <span className="h-2 w-2 rounded-full bg-emerald-500"></span>
                  Mainnet
                </div>
              </div>
            </motion.div>
          </>
        )}
      </AnimatePresence>
    </div>
  );
}

export function NetworkBanner() {
  const { currentNetwork } = useNetwork();

  if (currentNetwork.id !== 'testnet') return null;

  return (
    <div className="fixed top-0 left-0 right-0 z-60 flex h-10 items-center justify-center border-b border-yellow-200 bg-black px-4">
      <p className="flex items-center gap-2 text-xs font-medium text-white">
        <AlertTriangle className="h-3.5 w-3.5" />
        You are on Testnet — tokens have no real value
      </p>
    </div>
  );
}
