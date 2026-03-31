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
        className="flex items-center gap-2 rounded-full border border-border bg-white px-3 py-1.5 text-sm font-medium transition-colors hover:bg-secondary"
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
              className="absolute right-0 z-50 mt-2 w-48 origin-top-right rounded-xl border border-border bg-white p-1 shadow-lg"
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

export function NetworkBanner() {
  const { currentNetwork } = useNetwork();

  if (currentNetwork.id !== 'testnet') return null;

  return (
    <div className="w-full bg-amber-500/10 px-4 py-2 text-center border-b border-amber-500/20">
      <p className="flex items-center justify-center gap-2 text-sm font-medium text-amber-700">
        <AlertTriangle className="h-4 w-4" />
        ⚠️ You are on Testnet — tokens have no real value
      </p>
    </div>
  );
}
