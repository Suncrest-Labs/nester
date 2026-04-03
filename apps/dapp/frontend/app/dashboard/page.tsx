"use client";

import Link from "next/link";
import { useWallet } from "@/components/wallet-provider";
import { Navbar } from "@/components/navbar";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { motion } from "framer-motion";
import {
    ArrowDownToLine,
    ArrowUpRight,
    Sparkles,
    TrendingUp,
    Vault,
} from "lucide-react";

import {
    usePortfolio,
    type PortfolioPosition,
} from "@/components/portfolio-provider";
import { WithdrawModal } from "@/components/vault-action-modals";
import { truncateAddress } from "@/lib/utils";
import { PrometheusPanel } from "@/components/ai/prometheusPanel";
import { GuidedTour } from "@/components/onboarding/GuidedTour";

export default function Dashboard() {
    const { isConnected, isInitializing, address } = useWallet();
    const { positions, transactions, balances } = usePortfolio();
    const router = useRouter();
    const [selectedPosition, setSelectedPosition] =
        useState<PortfolioPosition | null>(null);

    useEffect(() => {
        if (!isInitializing && !isConnected) {
            router.push("/");
        }
    }, [isConnected, isInitializing, router]);

    const stats = useMemo(() => {
        const totalBalance = positions.reduce(
            (sum, position) => sum + position.currentValue,
            0
        );
        const totalYield = positions.reduce(
            (sum, position) => sum + position.yieldEarned,
            0
        );

        return [
            {
                label: "Total Balance",
                value: `$${totalBalance.toLocaleString("en-US", {
                    minimumFractionDigits: 2,
                    maximumFractionDigits: 2,
                })}`,
                change: null,
                icon: Vault,
            },
            {
                label: "Total Yield Earned",
                value: `$${totalYield.toLocaleString("en-US", {
                    minimumFractionDigits: 2,
                    maximumFractionDigits: 2,
                })}`,
                change: positions.length ? "+Live" : null,
                icon: TrendingUp,
            },
            {
                label: "Active Vaults",
                value: String(positions.length),
                change: null,
                icon: ArrowDownToLine,
            },
            {
                label: "Wallet USDC Balance",
                value: `$${(balances.USDC ?? 0).toLocaleString("en-US", {
                    minimumFractionDigits: 2,
                    maximumFractionDigits: 2,
                })}`,
                change: null,
                icon: Sparkles,
            },
        ];
    }, [balances.USDC, positions]);

    const recentTransactions = transactions.slice(0, 5);

    if (isInitializing || !isConnected) return null;

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
                    <h1 className="font-heading text-xl font-light text-foreground sm:text-2xl md:text-3xl">
                        Welcome back
                    </h1>
                    <p className="mt-1 font-mono text-xs text-muted-foreground sm:text-sm">
                        {address ? truncateAddress(address, 8) : ""}
                    </p>
                </motion.div>

                <div data-tour="portfolio-overview" className="mb-8 grid grid-cols-2 gap-3 sm:mb-10 sm:gap-4 lg:grid-cols-4">
                    {stats.map((stat, index) => (
                        <motion.div
                            key={stat.label}
                            initial={{ opacity: 0, y: 20 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ duration: 0.5, delay: 0.1 + index * 0.08 }}
                            className="group rounded-2xl border border-border bg-white p-4 transition-all hover:border-black/15 hover:shadow-sm sm:p-5"
                        >
                            <div className="mb-3 flex items-center justify-between sm:mb-4">
                                <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-secondary sm:h-9 sm:w-9">
                                    <stat.icon className="h-3.5 w-3.5 text-foreground/50 sm:h-4 sm:w-4" />
                                </div>
                                {stat.change && (
                                    <span className="flex items-center gap-0.5 text-[10px] font-medium text-emerald-600 sm:text-xs">
                                        <ArrowUpRight className="h-2.5 w-2.5 sm:h-3 sm:w-3" />
                                        {stat.change}
                                    </span>
                                )}
                            </div>
                            <p className="font-heading text-xl font-light text-foreground sm:text-2xl">
                                {stat.value}
                            </p>
                            <p className="mt-1 text-[10px] leading-tight text-muted-foreground sm:text-xs">
                                {stat.label}
                            </p>
                        </motion.div>
                    ))}
                </div>

                <div className="grid grid-cols-1 gap-4 sm:gap-6 lg:grid-cols-2">
                    {/* ── Vaults ── */}
                    <motion.div
                        data-tour="vault-list"
                        initial={{ opacity: 0, y: 20 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.5, delay: 0.4 }}
                        className="rounded-2xl border border-border bg-white p-5 sm:p-6"
                    >
                        <div className="mb-5 flex items-center justify-between sm:mb-6">
                            <h2 className="font-heading text-base font-light text-foreground sm:text-lg">
                                Your Vaults
                            </h2>
                            <Link
                                href="/dashboard/vaults"
                                data-tour="deposit-cta"
                                className="flex min-h-11 items-center px-2 text-xs font-medium text-foreground/60 transition-colors hover:text-foreground"
                            >
                                Add Deposit
                            </Link>
                        </div>

                        {positions.length === 0 ? (
                            <div className="flex flex-col items-center justify-center py-10 text-center sm:py-12">
                                <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-secondary sm:h-14 sm:w-14">
                                    <Vault className="h-5 w-5 text-muted-foreground sm:h-6 sm:w-6" />
                                </div>
                                <p className="text-sm font-medium text-foreground/80">
                                    No vaults yet
                                </p>
                                <p className="mt-1 max-w-xs text-xs leading-relaxed text-muted-foreground">
                                    Create your first vault position to start earning optimized yield across DeFi protocols.
                                </p>
                                <div className="mt-5 inline-block rounded-full border border-black/15 bg-white p-[3px] shadow-lg">
                                    <Link href="/dashboard/vaults">
                                        <button className="min-h-11 rounded-full bg-linear-to-r from-[#0a0a0a] to-[#1a1a2e] px-6 py-2.5 text-sm font-medium text-white transition-all hover:from-[#1a1a2e] hover:to-[#0a0a0a]">
                                            Get Started
                                        </button>
                                    </Link>
                                </div>
                            </div>
                        ) : (
                            <div className="space-y-3">
                                {positions.map((position) => (
                                    <div
                                        key={position.id}
                                        className="rounded-2xl border border-border bg-secondary/20 p-4"
                                    >
                                        <div className="flex flex-wrap items-start justify-between gap-4">
                                            <div>
                                                <p className="text-sm font-medium text-foreground">
                                                    {position.vaultName}
                                                </p>
                                                <p className="mt-1 text-xs text-muted-foreground">
                                                    {position.isMatured
                                                        ? "Matured and penalty free"
                                                        : `${position.daysRemaining} days until maturity`}
                                                </p>
                                            </div>
                                            <div className="text-right">
                                                <p className="text-sm font-medium text-foreground">
                                                    ${position.currentValue.toFixed(2)}
                                                </p>
                                                <p className="mt-1 text-xs text-emerald-600">
                                                    +${position.yieldEarned.toFixed(2)} yield
                                                </p>
                                            </div>
                                        </div>
                                        <div className="mt-4 flex flex-wrap items-center justify-between gap-3">
                                            <div className="text-xs text-muted-foreground">
                                                {position.shares.toFixed(2)} nVault shares
                                            </div>
                                            <button
                                                onClick={() => setSelectedPosition(position)}
                                                className="rounded-full border border-border bg-white px-4 py-2 text-xs font-medium text-foreground transition-colors hover:border-black/15"
                                            >
                                                Withdraw
                                            </button>
                                        </div>
                                    </div>
                                ))}
                            </div>
                        )}
                    </motion.div>

                    {/* ── Prometheus Advisory Panel ── */}
                    <motion.div
                        initial={{ opacity: 0, y: 20 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.5, delay: 0.5 }}
                    >
                        <div className="mb-4 flex items-center justify-between">
                            <h2 className="font-heading text-base font-light text-foreground sm:text-lg">
                                <span className="font-display italic">Prometheus</span>{" "}
                                Insights
                            </h2>
                            <div className="flex items-center gap-1.5">
                                <div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
                                <span className="text-xs text-muted-foreground">
                                    AI Advisory
                                </span>
                            </div>
                        </div>

                        {address ? (
                            <PrometheusPanel userId={address} />
                        ) : (
                            <div className="rounded-2xl border border-border bg-white p-5">
                                <p className="text-xs text-muted-foreground">
                                    Connect your wallet to activate Prometheus.
                                </p>
                            </div>
                        )}
                    </motion.div>
                </div>

                {/* ── Recent Activity ── */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.5, delay: 0.6 }}
                    className="mt-4 rounded-2xl border border-border bg-white p-5 sm:mt-6 sm:p-6"
                >
                    <h2 className="mb-4 font-heading text-base font-light text-foreground sm:text-lg">
                        Recent Activity
                    </h2>
                    {recentTransactions.length === 0 ? (
                        <div className="flex items-center justify-center py-8 sm:py-10">
                            <p className="text-sm text-muted-foreground">
                                No recent transactions
                            </p>
                        </div>
                    ) : (
                        <div className="space-y-3">
                            {recentTransactions.map((transaction) => (
                                <div
                                    key={transaction.id}
                                    className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border bg-secondary/10 px-4 py-3"
                                >
                                    <div>
                                        <p className="text-sm font-medium text-foreground">
                                            {transaction.type} · {transaction.vaultName}
                                        </p>
                                        <p className="mt-1 text-xs text-muted-foreground">
                                            {new Date(
                                                transaction.timestamp
                                            ).toLocaleString()}
                                        </p>
                                    </div>
                                    <div className="text-right">
                                        <p className="text-sm font-medium text-foreground">
                                            {transaction.amount} {transaction.asset}
                                        </p>
                                        <p className="mt-1 text-xs text-muted-foreground">
                                            {transaction.status}
                                        </p>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </motion.div>
            </main>

            <WithdrawModal
                open={!!selectedPosition}
                onClose={() => setSelectedPosition(null)}
                position={selectedPosition}
            />
            <GuidedTour />
        </div>
    );
}