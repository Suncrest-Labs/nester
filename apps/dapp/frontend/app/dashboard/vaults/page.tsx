"use client";

import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { motion } from "framer-motion";
import {
    ArrowDown,
    ArrowUpRight,
    ShieldCheck,
    TrendingUp,
    Vault as VaultIcon,
} from "lucide-react";

import { Navbar } from "@/components/navbar";
import { DepositModal } from "@/components/vault/depositModal";
import { usePortfolio } from "@/components/portfolio-provider";
import { useWallet } from "@/components/wallet-provider";
import { vaultDefinitions, type VaultDefinition } from "@/lib/vault-data";

export default function VaultsPage() {
    const { isConnected } = useWallet();
    const { positions } = usePortfolio();
    const router = useRouter();
    const [selectedVault, setSelectedVault] = useState<VaultDefinition | null>(
        null
    );

    useEffect(() => {
        if (!isConnected) {
            router.push("/");
        }
    }, [isConnected, router]);

    const exposureByVault = useMemo(() => {
        return positions.reduce<Record<string, number>>((acc, position) => {
            acc[position.vaultId] = (acc[position.vaultId] ?? 0) + position.currentValue;
            return acc;
        }, {});
    }, [positions]);

    if (!isConnected) return null;

    return (
        <div className="min-h-screen bg-background">
            <Navbar />

            <main className="mx-auto max-w-[1536px] px-4 pb-24 pt-20 md:px-8 md:pb-16 md:pt-28 lg:px-12 xl:px-16">
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.5 }}
                    className="mb-8 md:mb-10"
                >
                    <div className="mb-2 flex items-center gap-2 text-primary">
                        <VaultIcon className="h-3.5 w-3.5 sm:h-4 sm:w-4" />
                        <span className="text-[10px] font-mono font-medium uppercase tracking-wider sm:text-xs">
                            Vaults Engine
                        </span>
                    </div>
                    <h1 className="font-heading text-2xl font-light text-foreground sm:text-3xl md:text-4xl">
                        Optimize your Yield
                    </h1>
                    <p className="mt-2 max-w-2xl text-sm leading-relaxed text-muted-foreground sm:text-base">
                        Choose a vault, review lock terms and penalties, and simulate wallet signing before the live Soroban contracts are deployed.
                    </p>
                </motion.div>

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 sm:gap-6">
                    {vaultDefinitions.map((vault, index) => {
                        const currentExposure = exposureByVault[vault.id] ?? 0;

                        return (
                            <motion.div
                                key={vault.id}
                                initial={{ opacity: 0, y: 20 }}
                                animate={{ opacity: 1, y: 0 }}
                                transition={{ duration: 0.5, delay: index * 0.08 }}
                                className="group relative overflow-hidden rounded-2xl border border-border bg-white p-6 transition-all hover:border-black/15 hover:shadow-xl sm:rounded-3xl sm:p-8"
                            >
                                <div className="flex h-full flex-col">
                                    <div className="mb-5 flex items-start justify-between sm:mb-6">
                                        <div className="rounded-xl bg-secondary p-2.5 text-foreground/70 sm:rounded-2xl sm:p-3">
                                            <vault.icon className="h-5 w-5 sm:h-6 sm:w-6" />
                                        </div>
                                        <div className="text-right">
                                            <p className="text-[10px] font-medium uppercase tracking-tight text-muted-foreground sm:text-sm">
                                                target apy
                                            </p>
                                            <p className="font-heading text-2xl font-light text-emerald-600 sm:text-3xl">
                                                {vault.apyLabel}
                                            </p>
                                        </div>
                                    </div>

                                    <div className="mb-6 sm:mb-8">
                                        <h3 className="mb-2 font-heading text-lg font-light text-foreground sm:text-xl">
                                            {vault.name}
                                        </h3>
                                        <p className="text-sm leading-relaxed text-muted-foreground">
                                            {vault.description}
                                        </p>
                                    </div>

                                    <div className="mb-5 mt-auto flex flex-wrap gap-2 border-t border-border pt-5 sm:mb-6 sm:pt-6">
                                        {vault.strategies.map((strategy) => (
                                            <span
                                                key={strategy}
                                                className="rounded-full bg-secondary px-2.5 py-1 text-[10px] font-medium uppercase text-foreground/60"
                                            >
                                                {strategy}
                                            </span>
                                        ))}
                                    </div>

                                    <div className="rounded-2xl border border-border bg-secondary/20 p-4">
                                        <div className="flex items-center justify-between text-xs text-muted-foreground">
                                            <span>Lock period</span>
                                            <span className="font-medium text-foreground">
                                                {vault.lockDays} days
                                            </span>
                                        </div>
                                        <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
                                            <span>Early exit penalty</span>
                                            <span className="font-medium text-foreground">
                                                {vault.earlyWithdrawalPenaltyPct.toFixed(1)}%
                                            </span>
                                        </div>
                                        <div className="mt-2 flex items-center justify-between text-xs text-muted-foreground">
                                            <span>Your current exposure</span>
                                            <span className="font-medium text-foreground">
                                                {currentExposure.toLocaleString("en-US", {
                                                    minimumFractionDigits: 2,
                                                    maximumFractionDigits: 2,
                                                })}{" "}
                                                USDC
                                            </span>
                                        </div>
                                    </div>

                                    <div className="mt-6 flex items-center justify-between">
                                        <div className="flex items-center gap-1.5">
                                            <div
                                                className={`h-1.5 w-1.5 rounded-full ${
                                                    vault.risk === "Low"
                                                        ? "bg-emerald-500"
                                                        : vault.risk === "Medium"
                                                          ? "bg-blue-500"
                                                          : vault.risk === "Moderate High"
                                                            ? "bg-orange-500"
                                                            : "bg-purple-500"
                                                }`}
                                            />
                                            <span className="text-xs font-medium text-muted-foreground">
                                                {vault.risk} Risk
                                            </span>
                                        </div>
                                        <button
                                            onClick={() => setSelectedVault(vault)}
                                            className="flex min-h-[44px] items-center gap-1.5 px-1 text-sm font-medium text-foreground transition-all hover:gap-2"
                                        >
                                            Deposit <ArrowUpRight className="h-4 w-4" />
                                        </button>
                                    </div>
                                </div>
                            </motion.div>
                        );
                    })}
                </div>

                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.5, delay: 0.6 }}
                    className="mt-8 rounded-2xl border border-border bg-secondary/30 p-5 sm:mt-12 sm:rounded-3xl sm:p-8"
                >
                    <div className="grid grid-cols-1 gap-6 sm:grid-cols-3 sm:gap-8">
                        <div className="flex flex-col gap-3">
                            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-border bg-white">
                                <TrendingUp className="h-5 w-5 text-emerald-600" />
                            </div>
                            <h4 className="font-heading font-medium text-foreground">
                                Auto-Rebalancing
                            </h4>
                            <p className="text-xs leading-relaxed text-muted-foreground">
                                The deposit flow previews yield terms while keeping the signing and submission steps mockable until contracts are live on testnet.
                            </p>
                        </div>
                        <div className="flex flex-col gap-3">
                            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-border bg-white">
                                <ShieldCheck className="h-5 w-5 text-blue-600" />
                            </div>
                            <h4 className="font-heading font-medium text-foreground">
                                Risk Guarded
                            </h4>
                            <p className="text-xs leading-relaxed text-muted-foreground">
                                Maturity dates and early withdrawal penalties are surfaced before every deposit so the withdrawal flow stays transparent.
                            </p>
                        </div>
                        <div className="flex flex-col gap-3">
                            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-border bg-white">
                                <ArrowDown className="h-5 w-5 text-purple-600" />
                            </div>
                            <h4 className="font-heading font-medium text-foreground">
                                Flexible Liquidity
                            </h4>
                            <p className="text-xs leading-relaxed text-muted-foreground">
                                Deposits mint nVault shares 1:1 in mock mode. Later, the same UI can switch to live Soroban contract calls without changing the user journey.
                            </p>
                        </div>
                    </div>
                </motion.div>
            </main>

            <DepositModal
                open={!!selectedVault}
                onClose={() => setSelectedVault(null)}
                vault={selectedVault}
            />
        </div>
    );
}
