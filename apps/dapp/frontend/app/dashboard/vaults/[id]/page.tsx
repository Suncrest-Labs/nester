"use client";

import { useEffect } from "react";
import { useParams, useRouter, notFound } from "next/navigation";
import { useWallet } from "@/components/wallet-provider";
import { Navbar } from "@/components/navbar";
import Link from "next/link";
import { motion } from "framer-motion";
import { ArrowLeft, TrendingUp } from "lucide-react";
import { cn } from "@/lib/utils";
import { getVaultById } from "@/lib/mock-vaults";
import type { RiskTier } from "@/lib/types/vault";
import { APYChart } from "@/components/vaults/apy-chart";
import { AllocationDonut } from "@/components/vaults/allocation-donut";
import { VaultMetrics } from "@/components/vaults/vault-metrics";
import { VaultTerms } from "@/components/vaults/vault-terms";
import { UserPosition } from "@/components/vaults/user-position";

const RISK_STYLES: Record<RiskTier, string> = {
  Conservative: "bg-emerald-100 text-emerald-700",
  Balanced: "bg-blue-100 text-blue-700",
  Growth: "bg-orange-100 text-orange-700",
  DeFi500: "bg-purple-100 text-purple-700",
};

function RiskBadge({ tier }: { tier: RiskTier }) {
  return (
    <span
      className={cn(
        "px-2.5 py-1 rounded-full text-xs font-medium",
        RISK_STYLES[tier],
      )}
    >
      {tier === "DeFi500" ? "DeFi500 Index" : tier}
    </span>
  );
}

export default function VaultDetailPage() {
  const { isConnected } = useWallet();
  const router = useRouter();
  const { id } = useParams();

  useEffect(() => {
    if (!isConnected) {
      router.push("/");
    }
  }, [isConnected, router]);

  if (!isConnected) return null;

  const vault = getVaultById(id?.toString() ?? "");
  if (!vault) notFound();

  return (
    <div className="min-h-screen bg-background">
      <Navbar />

      <main className="mx-auto max-w-384 px-4 md:px-8 lg:px-12 xl:px-16 pt-28 pb-16">
        {/* Back Button */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.4 }}
        >
          <Link
            href="/dashboard/vaults"
            className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors mb-6"
          >
            <ArrowLeft className="h-4 w-4" />
            All Vaults
          </Link>
        </motion.div>

        {/* Page Header */}
        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5, delay: 0.05 }}
          className="mb-8"
        >
          <div className="mb-3">
            <RiskBadge tier={vault.riskTier} />
          </div>
          <h1 className="font-heading text-3xl font-light text-foreground sm:text-4xl">
            {vault.name}
          </h1>
          <p className="mt-2 max-w-2xl text-muted-foreground">
            {vault.description}
          </p>
          <div className="mt-3 inline-flex items-center gap-2 px-3 py-1 rounded-full bg-secondary text-sm text-foreground/70">
            <TrendingUp className="h-3.5 w-3.5 text-emerald-600" />
            <span>
              Target APY:{" "}
              <span className="font-medium text-emerald-600">
                {vault.apyRange}
              </span>
            </span>
          </div>
        </motion.div>

        {/* Two-Column Layout */}
        <div className="grid gap-6 lg:grid-cols-5">
          {/* Left Column — Charts */}
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.1 }}
            className="lg:col-span-3 space-y-6"
          >
            <APYChart data={vault.apyHistory} />
            <AllocationDonut allocations={vault.allocations} />
          </motion.div>

          {/* Right Column — Metrics & Actions */}
          <motion.div
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5, delay: 0.15 }}
            className="lg:col-span-2 space-y-5"
          >
            <VaultMetrics
              currentApy={vault.currentApy}
              tvl={vault.tvl}
              userCount={vault.userCount}
            />

            {/* Supported Assets */}
            <div className="rounded-2xl border border-border bg-white p-5">
              <p className="font-heading text-sm font-medium text-foreground mb-3">
                Supported Assets
              </p>
              <div className="flex gap-2">
                {vault.supportedAssets.map((asset) => (
                  <span
                    key={asset}
                    className="flex items-center gap-1.5 px-3 py-1.5 rounded-full border border-border text-sm font-medium text-foreground"
                  >
                    <span
                      className="h-2 w-2 rounded-full"
                      style={{ background: "#2EBAC6" }}
                    />
                    {asset}
                  </span>
                ))}
              </div>
            </div>

            <VaultTerms
              maturityTerms={vault.maturityTerms}
              earlyWithdrawalPenalty={vault.earlyWithdrawalPenalty}
            />

            <UserPosition />

            {/* Deposit CTA */}
            <div className="rounded-2xl border border-border bg-white p-5">
              <button
                disabled
                className="w-full rounded-xl bg-foreground text-background py-4 text-sm font-medium disabled:opacity-40 disabled:cursor-not-allowed transition-all"
              >
                Deposit into {vault.name}
              </button>
              <p className="text-center text-[11px] text-muted-foreground mt-2.5">
                Deposit flow coming soon —{" "}
                <span className="font-mono text-foreground/40">#30</span>
              </p>
            </div>
          </motion.div>
        </div>
      </main>
    </div>
  );
}
