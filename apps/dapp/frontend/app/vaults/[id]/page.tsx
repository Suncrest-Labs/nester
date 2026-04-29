"use client";

import { useEffect } from "react";
import { useParams } from "next/navigation";
import { useRouter } from "next/navigation";
import { ProtectedRoute } from "@/components/protected-route";
import { AppShell } from "@/components/app-shell";
import Link from "next/link";
import Image from "next/image";
import { motion } from "framer-motion";
import { useState } from "react";
import { ArrowLeft, TrendingUp, Info } from "lucide-react";
import { AnimatePresence } from "framer-motion";
import { useVaults, formatTvl } from "@/hooks/useVaults";
import { UserPosition } from "@/components/vaults/user-position";
import { cn } from "@/lib/utils";

function InfoTooltip({ text }: { text: string }) {
    const [show, setShow] = useState(false);
    return (
        <div className="relative" onMouseEnter={() => setShow(true)} onMouseLeave={() => setShow(false)}>
            <button
                className="flex h-4 w-4 items-center justify-center rounded-full border border-black/12 text-black/30 hover:border-black/25 hover:text-black/55 transition-colors"
                tabIndex={-1}
            >
                <Info className="h-2.5 w-2.5" />
            </button>
            <AnimatePresence>
                {show && (
                    <motion.div
                        initial={{ opacity: 0, y: 4 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: 4 }}
                        transition={{ duration: 0.13 }}
                        className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-20 w-56 rounded-xl border border-black/8 bg-white px-3 py-2.5 shadow-lg text-xs text-black/50 leading-relaxed pointer-events-none"
                    >
                        {text}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}



export default function VaultDetailPage() {
    const { id } = useParams();
    const router = useRouter();
    
    const { data: vaults = [], isLoading } = useVaults();
    const vault = vaults.find(v => v.id === id?.toString());

    // If vault isn't found after loading, redirect to vaults list
    useEffect(() => {
        if (!isLoading && !vault) {
            router.replace("/vaults");
        }
    }, [vault, isLoading, router]);

    return (
        <ProtectedRoute
            fallback={<div className="flex items-center justify-center min-h-screen">Loading...</div>}
        >
            {isLoading || !vault ? (
                <div className="flex items-center justify-center min-h-screen">Loading...</div>
            ) : (
            <AppShell>
                {/* Back */}
                <motion.div
                    initial={{ opacity: 0, x: -8 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ duration: 0.3 }}
                    className="mb-7"
                >
                    <Link
                        href="/vaults"
                        className="inline-flex items-center gap-1.5 text-xs text-black/40 hover:text-black transition-colors"
                    >
                        <ArrowLeft className="h-3.5 w-3.5" />
                        All Markets
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
                            <div className="flex items-center gap-2 mb-2">
                                <div className="flex items-center">
                                    <Image
                                        src={vault.name.toLowerCase().includes('xlm') ? '/xlm.png' : '/usdc.png'}
                                        alt={vault.name.toLowerCase().includes('xlm') ? 'XLM' : 'USDC'}
                                        width={28}
                                        height={28}
                                        className="rounded-full border-2 border-white"
                                    />
                                </div>
                                <span className="text-[10px] uppercase tracking-widest text-black/35">
                                    {vault.strategy}
                                </span>
                            </div>
                            <h1 className="text-2xl text-black sm:text-3xl">{vault.name}</h1>
                            <p className="mt-2 max-w-xl text-sm leading-relaxed text-black/45">
                                Contract Address: {vault.contractAddress}
                            </p>
                        </div>
                        <div className="flex items-center gap-2 shrink-0 rounded-xl border border-black/8 px-4 py-2">
                            <div>
                                <div className="flex items-center gap-1.5">
                                    <TrendingUp className="h-3.5 w-3.5 text-black/35" />
                                    <span className="text-xs text-black/45">Target APY</span>
                                    <span className="font-mono text-sm text-black">{vault.apy !== undefined ? `${vault.apy.toFixed(1)}%` : 'TBD'}</span>
                                </div>
                                {vault.apy !== undefined && (
                                    <p className="text-[9px] text-black/40 mt-1 max-w-[200px]">APY is variable and based on recent performance. Past performance is not indicative of future results.</p>
                                )}
                            </div>
                        </div>
                    </div>
                </motion.div>

                {/* Key metrics strip */}
                <motion.div
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.3, delay: 0.1 }}
                    className="mb-8 grid grid-cols-2 gap-3 sm:grid-cols-2 sm:gap-4"
                >
                    {[
                        { label: "Current APY", value: vault.apy !== undefined ? `${vault.apy.toFixed(1)}%` : 'TBD', tooltip: "The current annualized yield rate for supplying assets to this market." },
                        { label: "TVL", value: vault.tvl !== undefined ? formatTvl(vault.tvl) : 'TBD', tooltip: "Total Value Locked — the total amount of assets currently deposited in this market." },
                    ].map((m) => (
                        <div key={m.label} className="rounded-2xl border border-black/8 bg-white px-5 py-4">
                            <p className="font-mono text-xl text-black sm:text-2xl">{m.value}</p>
                            <div className="mt-0.5 flex items-center gap-1.5">
                                <span className="text-[11px] text-black/35">{m.label}</span>
                                <InfoTooltip text={m.tooltip} />
                            </div>
                        </div>
                    ))}
                </motion.div>

                {/* Two-column layout */}
                <div className="grid gap-5 lg:grid-cols-5">
                    {/* Left: Info + actions */}
                    <motion.div
                        initial={{ opacity: 0, y: 16 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ duration: 0.4, delay: 0.2 }}
                        className="space-y-4 lg:col-span-5"
                    >
                        {/* Market info */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5 space-y-3">
                            <p className="text-xs text-black/35 uppercase tracking-widest">Market Info</p>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">Strategy</span>
                                <span className="text-black">{vault.strategy}</span>
                            </div>
                            <div className="flex justify-between text-xs">
                                <span className="text-black/40">TVL</span>
                                <span className="font-mono text-black">{vault.tvl !== undefined ? formatTvl(vault.tvl) : 'TBD'}</span>
                            </div>
                        </div>

                        {/* User position */}
                        <UserPosition />

                        {/* Supply CTA */}
                        <div className="rounded-2xl border border-black/8 bg-white p-5">
                            <button
                                disabled
                                className="w-full rounded-xl bg-black py-3.5 text-sm text-white transition-opacity disabled:opacity-35 disabled:cursor-not-allowed"
                            >
                                Supply to {vault.name}
                            </button>
                            <p className="mt-2.5 text-center text-[11px] text-black/30">
                                Supply flow coming soon
                            </p>
                        </div>
                    </motion.div>
                </div>
            </AppShell>
            )}
        </ProtectedRoute>
    );
}
