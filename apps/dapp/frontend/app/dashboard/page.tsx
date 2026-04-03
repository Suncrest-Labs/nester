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
import { GuidedTour } from "@/components/onboarding/GuidedTour";

export default function Dashboard() {
    const { isConnected, address } = useWallet();
    const { positions, transactions, balances } = usePortfolio();
    const router = useRouter();
    const [selectedPosition, setSelectedPosition] = useState<PortfolioPosition | null>(null);

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    const stats = useMemo(() => {
        const totalBalance = positions.reduce((sum, p) => sum + p.currentValue, 0);
        const totalYield = positions.reduce((sum, p) => sum + p.yieldEarned, 0);
        return [
            {
                label: "Total Balance",
                value: `$${totalBalance.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`,
                change: null,
                icon: Vault,
            },
            {
                label: "Total Yield Earned",
                value: `$${totalYield.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`,
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
                value: `$${(balances.USDC ?? 0).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`,
                change: null,
                icon: Sparkles,
            },
        ];
    }, [balances.USDC, positions]);

    const recentTransactions = transactions.slice(0, 5);

    if (!isConnected) return null;

    return (
        <div className="min-h-screen bg-white">
            <Navbar />

            <main className="mx-auto max-w-5xl px-4 pb-20 pt-24 md:px-8 md:pb-16 md:pt-32 lg:px-12">

                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.4 }}
                    className="mb-8"
                >
                    <h1 className="text-2xl text-black sm:text-3xl">Welcome back</h1>
                    <p className="mt-1 font-mono text-xs text-black/40 sm:text-sm">
                        {address ? truncateAddress(address, 8) : ""}
                    </p>
                </motion.div>

                {/* Stats grid */}
                <div data-tour="portfolio-overview" className="mb-8 grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
                    {stats.map((stat, index) => (
                        <motion.div
                            key={stat.label}
                            initial={{ opacity: 0, y: 16 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ duration: 0.4, delay: 0.08 + index * 0.07 }}
                            className="rounded-2xl border border-black/8 bg-white p-4 transition-all hover:border-black/15 hover:shadow-sm sm:p-5"
                        >
                            <div className="mb-3 flex items-center justify-between sm:mb-4">
                                <div className="flex h-8 w-8 items-center justify-center rounded-xl bg-black/5 sm:h-9 sm:w-9">
                                    <stat.icon className="h-3.5 w-3.5 text-black/40 sm:h-4 sm:w-4" />
                                </div>
                                {stat.change && (
                                    <span className="flex items-center gap-0.5 text-[10px] text-black/50 sm:text-xs">
                                        <ArrowUpRight className="h-2.5 w-2.5 sm:h-3 sm:w-3" />
                                        {stat.change}
                                    </span>
                                )}
                            </div>
                            <p className="font-mono text-xl text-black sm:text-2xl">{stat.value}</p>
                            <p className="mt-1 text-[10px] leading-tight text-black/40 sm:text-xs">{stat.label}</p>
                        </motion.div>
                    ))}
                </div>

                {/* Your Vaults */}
                <motion.div
                    data-tour="vault-list"
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.4, delay: 0.35 }}
                    className="mb-4 rounded-2xl border border-black/8 bg-white p-5 sm:mb-5 sm:p-6"
                >
                    <div className="mb-5 flex items-center justify-between">
                        <h2 className="text-base text-black sm:text-lg">Your Vaults</h2>
                        <Link
                            href="/dashboard/vaults"
                            data-tour="deposit-cta"
                            className="flex min-h-10 items-center text-xs text-black/40 transition-colors hover:text-black"
                        >
                            Add Deposit
                        </Link>
                    </div>

                    {positions.length === 0 ? (
                        <div className="flex flex-col items-center justify-center py-10 text-center sm:py-12">
                            <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl bg-black/5 sm:h-14 sm:w-14">
                                <Vault className="h-5 w-5 text-black/30 sm:h-6 sm:w-6" />
                            </div>
                            <p className="text-sm text-black/60">No vaults yet</p>
                            <p className="mt-1 max-w-xs text-xs leading-relaxed text-black/35">
                                Create your first vault position to start earning optimized yield across DeFi protocols.
                            </p>
                            <Link href="/dashboard/vaults" className="mt-5">
                                <button className="rounded-full bg-black px-6 py-2.5 text-sm text-white transition-opacity hover:opacity-75">
                                    Get Started
                                </button>
                            </Link>
                        </div>
                    ) : (
                        <div className="space-y-2.5">
                            {positions.map((position) => (
                                <div
                                    key={position.id}
                                    className="rounded-2xl border border-black/8 bg-black/1.5 p-4"
                                >
                                    <div className="flex flex-wrap items-start justify-between gap-4">
                                        <div>
                                            <p className="text-sm text-black">{position.vaultName}</p>
                                            <p className="mt-0.5 text-xs text-black/40">
                                                {position.isMatured
                                                    ? "Matured — penalty free"
                                                    : `${position.daysRemaining} days until maturity`}
                                            </p>
                                        </div>
                                        <div className="text-right">
                                            <p className="font-mono text-sm text-black">
                                                ${position.currentValue.toFixed(2)}
                                            </p>
                                            <p className="mt-0.5 text-xs text-black/50">
                                                +${position.yieldEarned.toFixed(4)} yield
                                            </p>
                                        </div>
                                    </div>
                                    <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
                                        <span className="font-mono text-xs text-black/35">
                                            {position.shares.toFixed(2)} nVault shares
                                        </span>
                                        <button
                                            onClick={() => setSelectedPosition(position)}
                                            className="rounded-full border border-black/10 bg-white px-4 py-1.5 text-xs text-black/60 transition-colors hover:border-black/20 hover:text-black"
                                        >
                                            Withdraw
                                        </button>
                                    </div>
                                </div>
                            ))}
                        </div>
                    )}
                </motion.div>

                {/* Recent Activity */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.4, delay: 0.5 }}
                    className="rounded-2xl border border-black/8 bg-white p-5 sm:p-6"
                >
                    <h2 className="mb-4 text-base text-black sm:text-lg">Recent Activity</h2>
                    {recentTransactions.length === 0 ? (
                        <div className="flex items-center justify-center py-8 sm:py-10">
                            <p className="text-sm text-black/35">No recent transactions</p>
                        </div>
                    ) : (
                        <div className="space-y-2.5">
                            {recentTransactions.map((tx) => (
                                <div
                                    key={tx.id}
                                    className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-black/6 bg-black/1.5 px-4 py-3"
                                >
                                    <div>
                                        <p className="text-sm text-black">
                                            {tx.type} · {tx.vaultName}
                                        </p>
                                        <p className="mt-0.5 text-xs text-black/35">
                                            {new Date(tx.timestamp).toLocaleString()}
                                        </p>
                                    </div>
                                    <div className="text-right">
                                        <p className="font-mono text-sm text-black">
                                            {tx.amount} {tx.asset}
                                        </p>
                                        <p className="mt-0.5 text-xs text-black/40">{tx.status}</p>
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
