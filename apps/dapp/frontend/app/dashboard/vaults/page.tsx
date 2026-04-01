"use client";

import { useEffect, Suspense, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";

import { motion } from "framer-motion";
import { cn } from "@/lib/utils";

import { Navbar } from "@/components/navbar";
import { DepositModal } from "@/components/vault/depositModal";
import { useWallet } from "@/components/wallet-provider";

import {
  ArrowUpRight,
  Vault as VaultIcon,
} from "lucide-react";

import {
  formatTvl,
  type Vault as VaultType,
  type RiskTier,
} from "@/lib/mock-vaults";
import Image from "next/image";


import { useVaultFilters } from "@/hooks/use-vault-filters";


// -------------------- RISK STYLES --------------------

const RISK_STYLES: Record<RiskTier, { badge: string; dot: string }> = {
  Conservative: { badge: "bg-black text-white", dot: "bg-emerald-500" },
  Balanced: { badge: "bg-black text-white", dot: "bg-blue-500" },
  Growth: { badge: "bg-black text-white", dot: "bg-orange-500" },
  DeFi500: { badge: "bg-black text-white", dot: "bg-purple-500" },
};


// -------------------- COMPONENTS --------------------

function RiskBadge({ tier }: { tier: RiskTier }) {
  // Only change label for display, not type
  const label = tier === "DeFi500" ? "DeFi500 Index" : tier;
  return (
    <span className={cn("px-2.5 py-1 rounded-full text-xs font-medium", RISK_STYLES[tier].badge)}>
      {label}
    </span>
  );
}

function EmptyState() {
  return (
    <motion.div
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      className="col-span-2 flex flex-col items-center justify-center py-20 text-center"
    >
      <div className="mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-secondary">
        <VaultIcon className="h-6 w-6 text-muted-foreground" />
      </div>
      <p className="text-sm font-medium text-foreground/80">
        No vaults match your filter
      </p>
      <p className="mt-1 text-xs text-muted-foreground">
        Try changing the filter.
      </p>
    </motion.div>
  );
}

function VaultCard({
  vault,
  index,
  onSelect,
}: {
  vault: VaultType;
  index: number;
  onSelect: (v: VaultType) => void;
}) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 20 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.5, delay: 0.2 + index * 0.08 }}
      className="group relative flex flex-col overflow-hidden rounded-2xl border border-border bg-white p-6 transition-all hover:border-black/15 hover:shadow-xl"
    >
      <div className="mb-4">
        <RiskBadge tier={vault.riskTier} />
      </div>

      <div className="mb-5">
        <h3 className="mb-1.5 text-xl font-heading font-light text-foreground">
          {vault.name}
        </h3>
        <p className="line-clamp-2 text-sm text-muted-foreground">
          {vault.description}
        </p>
      </div>

      <div className="mb-5 flex items-end gap-6">
        <div>
          <p className="mb-1 text-[11px] uppercase text-muted-foreground">APY</p>
          <p className="text-3xl font-heading text-black">
            {vault.currentApy.toFixed(1)}%
          </p>
        </div>

        <div>
          <p className="mb-1 text-[11px] uppercase text-muted-foreground">TVL</p>
          <p className="text-3xl font-heading text-black">{formatTvl(vault.tvl)}</p>
        </div>
      </div>

      {/* Removed allocation tags section */}

      <div className="mb-5 flex items-center -space-x-2">
        {/* Show XLM for all vaults except Savings Vault */}
        {vault.id !== "savings" && (
          <Image
            src="/xlm.png"
            alt="XLM"
            title="XLM"
            width={28}
            height={28}
            unoptimized
          />
        )}
        {/* Show USDC if supported */}
        {vault.supportedAssets.includes("USDC") && (
          <Image src="/usdc.png" alt="USDC" title="USDC" width={28} height={28} />
        )}
      </div>

      <div className="mt-auto flex items-center justify-end">

        <div className="flex gap-3">
          <Link href={`/dashboard/vaults/${vault.id}`}>
            <button className="text-sm hover:text-primary">
              Details
            </button>
          </Link>

          <button
            onClick={() => onSelect(vault)}
            className="flex items-center gap-1.5 text-sm font-medium"
          >
            Deposit <ArrowUpRight className="h-4 w-4" />
          </button>
        </div>
      </div>
    </motion.div>
  );
}


// -------------------- FILTER CONFIG --------------------




// -------------------- MAIN CONTENT --------------------

function VaultsPageContent({ onSelect }: { onSelect: (v: VaultType) => void }) {
  // Add Savings Vault to the list
  const savingsVault: VaultType = {
    id: "savings",
    name: "Savings Vault",
    description: "A simple, low-risk vault for stable USDC savings. Earn yield with no lockup.",
    riskTier: "Conservative",
    currentApy: 5.2,
    apyRange: "4–6%",
    tvl: 1_200_000,
    userCount: 800,
    allocations: [
      { protocol: "USDC Savings", percentage: 100, apy: 5.2, color: "#2775CA" },
    ],
    supportedAssets: ["USDC"],
    maturityTerms: "Flexible — withdraw anytime",
    earlyWithdrawalPenalty: "None",
    apyHistory: [],
  };
  const { filteredAndSorted } = useVaultFilters();
  const vaultsWithSavings = [...filteredAndSorted, savingsVault];
  return (
    <div className="grid gap-6 sm:grid-cols-2">
      {vaultsWithSavings.length === 0 ? (
        <EmptyState />
      ) : (
        vaultsWithSavings.map((v, i) => (
          <VaultCard key={v.id} vault={v} index={i} onSelect={onSelect} />
        ))
      )}
    </div>
  );
}


// -------------------- PAGE --------------------

export default function VaultsPage() {
  const { isConnected } = useWallet();
  const router = useRouter();

  const [selectedVault, setSelectedVault] = useState<VaultType | null>(null);

  useEffect(() => {
    if (!isConnected) router.push("/");
  }, [isConnected, router]);

  if (!isConnected) return null;

  return (
    <div className="min-h-screen bg-background">
      <Navbar />

      <main className="mx-auto max-w-6xl px-4 pt-36 pb-16">
        <h1 className="mb-6 text-3xl font-light">Vaults</h1>

        <Suspense>
          <VaultsPageContent onSelect={setSelectedVault} />
        </Suspense>
      </main>

      <DepositModal
        open={!!selectedVault}
        onClose={() => setSelectedVault(null)}
        vault={selectedVault}
      />
    </div>
  );
}