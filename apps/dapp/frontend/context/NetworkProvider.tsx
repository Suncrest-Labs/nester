"use client";

import React, { createContext, useContext, useEffect, useState, ReactNode } from "react";
import { NETWORKS, NetworkConfig, DEFAULT_NETWORK } from "@/lib/networks";

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
    setMounted(true);
    const savedNetwork = localStorage.getItem("nester_network_id");
    if (savedNetwork && (savedNetwork === "testnet" || savedNetwork === "mainnet")) {
      setCurrentNetworkState(NETWORKS[savedNetwork]);
    }
  }, []);

  const setNetwork = (networkId: 'testnet' | 'mainnet') => {
    const newNetwork = NETWORKS[networkId];
    if (newNetwork && newNetwork.id !== currentNetwork.id) {
      setCurrentNetworkState(newNetwork);
      localStorage.setItem("nester_network_id", networkId);
      
      // Clear cached data based on requirements
      Object.keys(localStorage).forEach(key => {
        if (key.startsWith("nester_portfolio_v1:")) {
          localStorage.removeItem(key);
        }
      });
      
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
