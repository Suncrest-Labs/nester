"use client";

import { useEffect } from "react";
import { useParams, useRouter, notFound } from "next/navigation";
import { useWallet } from "@/components/wallet-provider";
import { Navbar } from "@/components/navbar";
import Link from "next/link";
import { motion } from "framer-motion";
import { ArrowLeft, TrendingUp, Users } from "lucide-react";
import { getVaultById, formatTvl } from "@/lib/mock-vaults";
import { APYChart } from "@/components/vaults/apy-chart";
import { AllocationDonut } from "@/components/vaults/allocation-donut";
import { UserPosition } from "@/components/vaults/user-position";
import Image from "next/image";

export default function VaultDetailPage() {
    const { isConnected } = useWallet();
    const router = useRouter();
    const { id } = useParams();

    useEffect(() => {
        if (!isConnected) router.push("/");
    }, [isConnected, router]);

    if (!isConnected) return null;

    const vault = getVaultById(id?.toString() ?? "");
    if (!vault) notFound();

    return (
        <div className="min-h-screen bg-white">
            <Navbar />

            <main className="mx-auto max-w-5xl px-4 pb-20 pt-24 md:px-8 md:pb-16 md:pt-32 lg:px-12">

                {/* Back */}
                <motion.div
                    initial={{ opacity: 0, x: -8 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ duration: 0.3 }}
                    className="mb-7"
                >
                    <Link
                        href="/dashboard/vaults"
                        className="inline-flex items-center gap-1.5 text-xs text-black/40 hover:text-black transition-colors"
                    >
                        <ArrowLeft className="h-3.5 w-3.5" />
                        All Vaults
                    </Link>
                </motion.div>

                {/* Header */}
                <motion.div
                    initial={{ opacity: 0, y: 12 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.35, delay: 0.05 }}
                    className="mb-8"
                >
                    <div className="flex items-start justify-between gap-4 flex-wrap">
                        <div>
                            <span className="text-[10px] uppercase tracking-widest text-black/35 mb-2 block">
                                {vault.riskTier === "DeFi500" ? "DeFi500 Index" : vault.riskTier}
                            </span>
                            <h1 className="text-2xl text-black sm:text-3xl">{vault.name}</h1>
                            <p className="mt-2 max-w-xl text-sm leading-relaxed text-black/45">
                                {vault.description}
                            </p>
                        </div>
                        <div className="flex items-center gap-2 shrink-0 rounded-xl border border-black/8 px-4 py-2">
                            <TrendingUp className="h-3.5 w-3.5 text-black/35" />
                            <span className="text-xs text-black/45">Target APY</span>
                            <span className="font-mono text-sm text-black">{vault.apyRange}</span>
                        </div>
                    </div>
                </motion.div>

                {/* Key metrics strip */}
                <motion.div
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.3, delay: 0.1 }}
                    className="mb-8 grid grid-cols-3 gap-3 sm:gap-4"
                >
                    {[
                        { label: "Current APY", value: `${vault.currentApy.toFixed(1)}%` },
                        { label: "TVL",          value: formatTvl(vault.tvl) },
                        { label: "Depositors",   value: vault.userCount.toLocaleString() },
                    ].map((m) => (
                        <div key={m.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                            <p className="font-mono text-xl text-black sm:text-2xl">{m.value}</p>
                            <p className="mt-0.5 text-[11px] text-black/35">{m.label}</p>
                        </div>
                    ))}
                </motion.div>

                {/* Two-column layout */}
                <div className="grid gap-5 lg:grid-cols-5">

                    {/* Left: Charts */}
                    <motion.div
                        initial={{ opacity: 0, y: 16 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.4, delay: 0.15 }}
                        className="space-y-5 lg:col-span-3"
                    >
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <p className="mb-4 text-xs text-black/35 uppercase tracking-widest">APY History</p>
                            <APYChart data={vault.apyHistory} />
                        </div>
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <p className="mb-4 text-xs text-black/35 uppercase tracking-widest">Allocation</p>
                            <AllocationDonut allocations={vault.allocations} />
                        </div>
                    </motion.div>

                    {/* Right: Info + actions */}
                    <motion.div
                        initial={{ opacity: 0, y: 16 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.4, delay: 0.2 }}
                        className="space-y-4 lg:col-span-2"
                    >
                        {/* Supported assets */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <p className="mb-3 text-xs text-black/35 uppercase tracking-widest">Supported Assets</p>
                            <div className="flex gap-2 flex-wrap">
                                {vault.supportedAssets
                                    .filter((a) => ["USDC", "XLM"].includes(a))
                                    .map((asset) => (
                                        <div key={asset} className="flex items-center gap-1.5 rounded-full border border-black/8 px-3 py-1.5">
                                            <Image
                                                src={`/${asset.toLowerCase()}.png`}
                                                alt={asset}
                                                width={16}
                                                height={16}
                                                className="rounded-full"
                                            />
                                            <span className="text-xs text-black/60">{asset}</span>
                                        </div>
                                    ))}
                            </div>
                        </div>

                        {/* Terms */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5 space-y-3">
                            <p className="text-xs text-black/35 uppercase tracking-widest">Terms</p>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">Maturity</span>
                                <span className="text-black">{vault.maturityTerms}</span>
                            </div>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">Early withdrawal</span>
                                <span className="text-black">{vault.earlyWithdrawalPenalty}</span>
                            </div>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">Depositors</span>
                                <span className="inline-flex items-center gap-1 text-black">
                                    <Users className="h-3 w-3 text-black/30" />
                                    {vault.userCount.toLocaleString()}
                                </span>
                            </div>
                        </div>

                        {/* User position */}
                        <UserPosition />

                        {/* Deposit CTA */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <button
                                disabled
                                className="w-full rounded-xl bg-black py-3.5 text-sm text-white transition-opacity disabled:opacity-35 disabled:cursor-not-allowed"
                            >
                                Deposit into {vault.name}
                            </button>
                            <p className="mt-2.5 text-center text-[11px] text-black/30">
                                Deposit flow coming soon —{" "}
                                <span className="font-mono">#30</span>
                            </p>
                        </div>
                    </motion.div>
                </div>
            </main>
        </div>
    );
}
