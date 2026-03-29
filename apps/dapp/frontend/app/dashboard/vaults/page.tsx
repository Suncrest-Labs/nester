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
  TrendingUp,
  ShieldCheck,
  ArrowUpRight,
  ArrowDown,
  Users,
  Vault as VaultIcon,
} from "lucide-react";

import {
  formatTvl,
  type Vault as VaultType,
  type RiskTier,
} from "@/lib/mock-vaults";

import { useVaultFilters, type SortKey } from "@/hooks/use-vault-filters";


// -------------------- RISK STYLES --------------------

const RISK_STYLES: Record<RiskTier, { badge: string; dot: string }> = {
  Conservative: { badge: "bg-emerald-100 text-emerald-700", dot: "bg-emerald-500" },
  Balanced: { badge: "bg-blue-100 text-blue-700", dot: "bg-blue-500" },
  Growth: { badge: "bg-orange-100 text-orange-700", dot: "bg-orange-500" },
  DeFi500: { badge: "bg-purple-100 text-purple-700", dot: "bg-purple-500" },
};


// -------------------- COMPONENTS --------------------

function RiskBadge({ tier }: { tier: RiskTier }) {
  return (
    <span className={cn("px-2.5 py-1 rounded-full text-xs font-medium", RISK_STYLES[tier].badge)}>
      {tier === "DeFi500" ? "DeFi500 Index" : tier}
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
          <p className="text-3xl font-heading text-emerald-600">
            {vault.currentApy.toFixed(1)}%
          </p>
        </div>

        <div>
          <p className="mb-1 text-[11px] uppercase text-muted-foreground">TVL</p>
          <p className="text-xl text-foreground">{formatTvl(vault.tvl)}</p>
        </div>

        <div className="ml-auto flex items-center gap-1 text-muted-foreground">
          <Users className="h-3.5 w-3.5" />
          <span className="text-xs">{vault.userCount.toLocaleString()}</span>
        </div>
      </div>

      <div className="mb-4 border-t border-border pt-4">
        <div className="flex flex-wrap gap-1.5">
          {vault.allocations.slice(0, 3).map((a) => (
            <span
              key={a.protocol}
              className="rounded-full bg-secondary px-2 py-0.5 text-[11px]"
            >
              {a.percentage}% {a.protocol}
            </span>
          ))}
        </div>
      </div>

      <div className="mb-5 flex gap-2">
        {vault.supportedAssets.map((asset) => (
          <span key={asset} className="rounded-full border px-2.5 py-1 text-xs">
            {asset}
          </span>
        ))}
      </div>

      <div className="mt-auto flex items-center justify-between">
        <span className="text-xs text-muted-foreground">
          {vault.riskTier} Risk
        </span>

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

const SORT_BUTTONS: { key: SortKey; label: string }[] = [
  { key: "apy", label: "APY" },
  { key: "tvl", label: "TVL" },
  { key: "risk", label: "Risk" },
];

const FILTER_BUTTONS: { key: RiskTier | "all"; label: string }[] = [
  { key: "all", label: "All" },
  { key: "Conservative", label: "Conservative" },
  { key: "Balanced", label: "Balanced" },
  { key: "Growth", label: "Growth" },
  { key: "DeFi500", label: "DeFi500" },
];


// -------------------- MAIN CONTENT --------------------

function VaultsPageContent({ onSelect }: { onSelect: (v: VaultType) => void }) {
  const { sortBy, filterTier, setSort, setFilter, filteredAndSorted } =
    useVaultFilters();

  return (
    <>
      <div className="mb-8 space-y-3">
        <div className="flex flex-wrap gap-2">
          {SORT_BUTTONS.map(({ key, label }) => (
            <button key={key} onClick={() => setSort(key)}>
              {label}
            </button>
          ))}
        </div>

        <div className="flex flex-wrap gap-2">
          {FILTER_BUTTONS.map(({ key, label }) => (
            <button key={key} onClick={() => setFilter(key)}>
              {label}
            </button>
          ))}
        </div>
      </div>

      <div className="grid gap-6 sm:grid-cols-2">
        {filteredAndSorted.length === 0 ? (
          <EmptyState />
        ) : (
          filteredAndSorted.map((v, i) => (
            <VaultCard key={v.id} vault={v} index={i} onSelect={onSelect} />
          ))
        )}
      </div>
    </>
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

      <main className="mx-auto max-w-6xl px-4 pt-28 pb-16">
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