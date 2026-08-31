"use client";

import React, { createContext, useContext, useEffect, useState, ReactNode } from "react";
import { NETWORKS, NetworkConfig, DEFAULT_NETWORK } from "@/lib/networks";
import { safeStorage } from "@/lib/storage";
import { PORTFOLIO_CACHE_PREFIX, readNetworkId, writeNetworkId } from "@/lib/storageKeys";

interface NetworkContextType {
  currentNetwork: NetworkConfig;
  setNetwork: (networkId: 'testnet' | 'mainnet') => void;
}

const NetworkContext = createContext<NetworkContextType>({
  currentNetwork: DEFAULT_NETWORK,
  setNetwork: () => {},
});

export function NetworkProvider({ children }: { children: ReactNode }) {
  const [currentNetwork, setCurrentNetworkState] = useState<NetworkConfig>(DEFAULT_NETWORK);
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    let isMounted = true;
    const timer = setTimeout(() => {
      if (isMounted) setMounted(true);
    }, 0);
    // safeStorage never throws (#1233) — a throwing accessor (private
    // browsing, full quota) falls back to the in-memory map, so this always
    // resolves to either the saved network or the safe DEFAULT_NETWORK
    // already set as initial state, never an undefined/crashed provider.
    const savedNetwork = readNetworkId();
    if (savedNetwork) {
      const timer2 = setTimeout(() => {
        if (isMounted) setCurrentNetworkState(NETWORKS[savedNetwork]);
      }, 0);
      return () => { isMounted = false; clearTimeout(timer); clearTimeout(timer2); };
    }
    return () => { isMounted = false; clearTimeout(timer); };
  }, []);

  const setNetwork = (networkId: 'testnet' | 'mainnet') => {
    const newNetwork = NETWORKS[networkId];
    if (newNetwork && newNetwork.id !== currentNetwork.id) {
      // Persist FIRST, then update React state (#1233): writeNetworkId
      // never throws — a failing localStorage write falls back to the
      // in-memory map instead — so by the time state changes, a subsequent
      // readNetworkId() call is guaranteed to agree with it, whether that
      // read lands on localStorage or the in-memory fallback. Doing this in
      // the opposite order (state first) is what could previously leave
      // React on the new network while persistence never happened at all.
      writeNetworkId(networkId);
      safeStorage.removeByPrefix(PORTFOLIO_CACHE_PREFIX);
      setCurrentNetworkState(newNetwork);

      // The wallet disconnection and confirmation will be handled
      // where the switch is triggered or by a wrapper component,
      // but the state change itself happens here.
    }
  };

  // Prevent hydration mismatch
  if (!mounted) {
    return (
      <NetworkContext.Provider value={{ currentNetwork: DEFAULT_NETWORK, setNetwork }}>
        {children}
      </NetworkContext.Provider>
    );
  }

  return (
    <NetworkContext.Provider value={{ currentNetwork, setNetwork }}>
      {children}
    </NetworkContext.Provider>
  );
}

export function useNetwork() {
  const context = useContext(NetworkContext);
  if (context === undefined) {
    throw new Error("useNetwork must be used within a NetworkProvider");
  }
  return context;
}
