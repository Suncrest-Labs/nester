"use client";

import { useWallet, type WalletInfo } from "@/components/wallet-provider";
import { motion, AnimatePresence } from "framer-motion";
import { ArrowRight, Loader2, AlertCircle, ChevronDown } from "lucide-react";
import Image from "next/image";
import { useState } from "react";
import { config } from "@/lib/config";

const FEATURED_IDS = config.featuredWallets;

function WalletGridCard({
    wallet,
    onSelect,
    isConnecting,
    connectingId,
}: {
    wallet: WalletInfo;
    onSelect: (id: string) => void;
    isConnecting: boolean;
    connectingId: string | null;
}) {
    const isThisConnecting = isConnecting && connectingId === wallet.id;

    return (
        <button
            onClick={() => onSelect(wallet.id)}
            disabled={isConnecting}
            className="group flex h-[84px] w-full flex-col items-center justify-center gap-2 rounded-xl border border-border bg-white transition-all hover:-translate-y-[2px] hover:border-black/20 hover:shadow-sm disabled:opacity-50 disabled:cursor-not-allowed active:scale-[0.97]"
        >
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-secondary/60 overflow-hidden">
                {isThisConnecting ? (
                    <Loader2 className="h-4 w-4 animate-spin text-brand-purple" />
                ) : wallet.icon ? (
                    <img
                        src={wallet.icon}
                        alt={wallet.name}
                        width={22}
                        height={22}
                        className="h-5.5 w-5.5 object-contain"
                    />
                ) : (
                    <div className="h-5 w-5 rounded-full bg-muted" />
                )}
            </div>
            <p className="text-sm font-medium text-foreground">{wallet.name}</p>
        </button>
    );
}

function WalletListItem({
    wallet,
    onSelect,
    isConnecting,
    connectingId,
}: {
    wallet: WalletInfo;
    onSelect: (id: string) => void;
    isConnecting: boolean;
    connectingId: string | null;
}) {
    const isThisConnecting = isConnecting && connectingId === wallet.id;

    return (
        <button
            onClick={() => onSelect(wallet.id)}
            disabled={isConnecting}
            className="group flex w-full items-center gap-3 rounded-xl border border-border bg-white p-3 transition-all hover:border-black/20 hover:shadow-sm disabled:opacity-50 disabled:cursor-not-allowed min-h-[52px] active:scale-[0.98]"
        >
            <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-secondary overflow-hidden">
                {wallet.icon ? (
                    <img
                        src={wallet.icon}
                        alt={wallet.name}
                        width={20}
                        height={20}
                        className="h-5 w-5 object-contain"
                    />
                ) : (
                    <div className="h-5 w-5 rounded-full bg-muted" />
                )}
            </div>
            <p className="flex-1 text-left text-sm font-medium text-foreground">
                {wallet.name}
            </p>
            <div className="shrink-0">
                {isThisConnecting ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin text-brand-purple" />
                ) : (
                    <ArrowRight className="h-3.5 w-3.5 text-muted-foreground/40 transition-all group-hover:translate-x-0.5 group-hover:text-foreground" />
                )}
            </div>
        </button>
    );
}

export function ConnectWallet() {
    const { connect, isConnecting, wallets, walletsLoaded } = useWallet();

    const [error, setError] = useState<string | null>(null);
    const [connectingId, setConnectingId] = useState<string | null>(null);
    const [showAll, setShowAll] = useState(false);

    const handleSelect = async (walletId: string) => {
        setError(null);
        setConnectingId(walletId);
        try {
            await connect(walletId);
        } catch (err) {
            setError(
                err instanceof Error ? err.message : "Failed to connect wallet"
            );
        } finally {
            setConnectingId(null);
        }
    };

    const sorted = [...wallets].sort((a, b) => {
        if (a.isAvailable && !b.isAvailable) return -1;
        if (!a.isAvailable && b.isAvailable) return 1;
        return 0;
    });

    const featured = FEATURED_IDS.map((id) => sorted.find((w) => w.id === id)).filter(
        (w): w is WalletInfo => !!w
    );

    const remaining = sorted.filter((w) => !FEATURED_IDS.includes(w.id));

    return (
        <div className="relative flex min-h-screen flex-col items-center justify-center overflow-hidden px-4 bg-background">
            <div className="pointer-events-none absolute inset-0">
                <div className="absolute -left-40 -top-40 h-[600px] w-[600px] rounded-full bg-brand-purple/[0.04] blur-[120px]" />
                <div className="absolute -bottom-40 -right-40 h-[500px] w-[500px] rounded-full bg-brand-cyan/[0.04] blur-[120px]" />
                <div
                    className="absolute inset-0 opacity-[0.025]"
                    style={{
                        backgroundImage:
                            "linear-gradient(hsl(var(--foreground)) 1px, transparent 1px), linear-gradient(90deg, hsl(var(--foreground)) 1px, transparent 1px)",
                        backgroundSize: "60px 60px",
                    }}
                />
            </div>

            <motion.div
                initial={{ opacity: 0, y: 30 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.8, ease: [0.25, 1, 0.5, 1] }}
                className="relative z-10 flex w-full max-w-sm sm:max-w-md flex-col items-center"
            >
                {/* Logo */}
                <motion.div
                    initial={{ opacity: 0, scale: 0.8 }}
                    animate={{ opacity: 1, scale: 1 }}
                    transition={{ duration: 0.6, delay: 0.1 }}
                    className="mb-6 sm:mb-8 animate-float"
                >
                    <Image
                        src="/logo.png"
                        alt="Nester"
                        width={56}
                        height={56}
                        className="rounded-2xl shadow-lg sm:w-16 sm:h-16"
                    />
                </motion.div>

                <motion.h1
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.6, delay: 0.2 }}
                    className="mb-3 text-center font-heading text-2xl font-light tracking-tight text-foreground sm:text-3xl md:text-4xl"
                >
                    Welcome to{" "}
                    <span className="font-display italic font-medium">Nester</span>
                </motion.h1>

                <motion.p
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.6, delay: 0.3 }}
                    className="mb-6 sm:mb-8 max-w-sm text-center text-sm sm:text-base text-muted-foreground leading-relaxed px-2"
                >
                    Connect your Stellar wallet to start earning optimized yield
                    and settle to fiat instantly.
                </motion.p>

                {/* Wallet Card */}
                <motion.div
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{ duration: 0.6, delay: 0.4 }}
                    className="w-full"
                >
                    <div className="rounded-2xl sm:rounded-3xl border border-border bg-white px-4 sm:px-6 py-5 shadow-xl shadow-black/[0.03]">
                        <p className="mb-4 text-xs font-medium text-muted-foreground tracking-wider uppercase text-center">
                            Choose a wallet
                        </p>

                        {!walletsLoaded ? (
                            <div className="flex items-center justify-center py-12">
                                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                            </div>
                        ) : (
                            <>

                                <div className="grid w-full grid-cols-3 gap-2 sm:gap-3 mb-3">
                                    {featured.map((wallet, i) => (
                                        <motion.div
                                            key={wallet.id}
                                            initial={{ opacity: 0, y: 8 }}
                                            animate={{ opacity: 1, y: 0 }}
                                            transition={{
                                                duration: 0.3,
                                                delay: 0.5 + i * 0.06,
                                            }}
                                        >
                                            <WalletGridCard
                                                wallet={wallet}
                                                onSelect={handleSelect}
                                                isConnecting={isConnecting}
                                                connectingId={connectingId}
                                            />
                                        </motion.div>
                                    ))}
                                </div>

                                {remaining.length > 0 && (
                                    <motion.button
                                        initial={{ opacity: 0 }}
                                        animate={{ opacity: 1 }}
                                        transition={{ delay: 0.7 }}
                                        onClick={() => setShowAll(!showAll)}
                                        className="flex w-full items-center justify-center gap-2 rounded-xl border border-dashed border-border py-3 text-xs font-medium text-muted-foreground transition-all hover:border-black/20 hover:text-foreground min-h-[44px]"
                                    >
                                        {showAll
                                            ? "Show less"
                                            : `More wallets (${remaining.length})`}
                                        <ChevronDown
                                            className={`h-3.5 w-3.5 transition-transform ${showAll ? "rotate-180" : ""
                                                }`}
                                        />
                                    </motion.button>
                                )}

                                {/* Expanded list */}
                                <AnimatePresence>
                                    {showAll && (
                                        <motion.div
                                            initial={{ opacity: 0, height: 0 }}
                                            animate={{ opacity: 1, height: "auto" }}
                                            exit={{ opacity: 0, height: 0 }}
                                            transition={{ duration: 0.25 }}
                                            className="overflow-hidden"
                                        >
                                            <div className="mt-3 space-y-2">
                                                {remaining.map((wallet, i) => (
                                                    <motion.div
                                                        key={wallet.id}
                                                        initial={{ opacity: 0, y: 6 }}
                                                        animate={{ opacity: 1, y: 0 }}
                                                        transition={{
                                                            duration: 0.2,
                                                            delay: i * 0.03,
                                                        }}
                                                    >
                                                        <WalletListItem
                                                            wallet={wallet}
                                                            onSelect={handleSelect}
                                                            isConnecting={isConnecting}
                                                            connectingId={connectingId}
                                                        />
                                                    </motion.div>
                                                ))}
                                            </div>
                                        </motion.div>
                                    )}
                                </AnimatePresence>
                            </>
                        )}

                        {/* Error */}
                        <AnimatePresence>
                            {error && (
                                <motion.div
                                    initial={{ opacity: 0, height: 0 }}
                                    animate={{ opacity: 1, height: "auto" }}
                                    exit={{ opacity: 0, height: 0 }}
                                    className="overflow-hidden"
                                >
                                    <div className="mt-3 flex items-start gap-2 rounded-xl bg-destructive/10 border border-destructive/20 px-4 py-3">
                                        <AlertCircle className="h-4 w-4 shrink-0 text-destructive mt-0.5" />
                                        <p className="text-sm text-destructive">{error}</p>
                                    </div>
                                </motion.div>
                            )}
                        </AnimatePresence>
                    </div>
                </motion.div>

                {/* Footer */}
                <motion.p
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    transition={{ duration: 0.6, delay: 0.9 }}
                    className="mt-6 text-center text-xs text-muted-foreground leading-relaxed px-4"
                >
                    By connecting, you agree to Nester&apos;s{" "}
                    <span className="text-foreground/70 hover:text-foreground cursor-pointer transition-colors">
                        Terms of Service
                    </span>
                </motion.p>
            </motion.div>
        </div>
    );
}