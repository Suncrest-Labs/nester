"use client";

import { motion, AnimatePresence } from "framer-motion";
import { useOnboarding } from "@/hooks/useOnboarding";
import { useWallet } from "@/components/wallet-provider";

export function WelcomeModal() {
    const { hasSeenWelcome, completeStep, skip } = useOnboarding();
    const { isConnected } = useWallet();

    const showModal = !hasSeenWelcome || (!isConnected && !hasSeenWelcome); // Usually just !hasSeenWelcome is enough

    if (!showModal) return null;

    return (
        <AnimatePresence>
            {showModal && (
                <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-background/80 backdrop-blur-sm">
                    <motion.div
                        initial={{ opacity: 0, scale: 0.95 }}
                        animate={{ opacity: 1, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.95 }}
                        transition={{ duration: 0.3 }}
                        className="relative w-full max-w-md overflow-hidden rounded-3xl border border-border bg-white shadow-2xl shadow-black/5"
                    >
                        <div className="p-8 text-center">
                            <h2 className="mb-2 font-heading text-2xl font-light tracking-tight text-foreground sm:text-3xl">
                                Welcome to <span className="font-display italic font-medium">Nester</span>
                            </h2>
                            <p className="mb-8 text-sm text-muted-foreground leading-relaxed">
                                Automated yield farming for your stablecoins on Stellar
                            </p>

                            <div className="mb-8 flex items-center justify-center gap-3 text-sm font-medium text-foreground/80">
                                <div className="flex flex-col items-center gap-2">
                                    <div className="flex h-10 w-10 items-center justify-center rounded-full bg-brand-purple/10 text-brand-purple">
                                        1
                                    </div>
                                    <span>Deposit</span>
                                </div>
                                <div className="h-0.5 w-8 bg-border" />
                                <div className="flex flex-col items-center gap-2">
                                    <div className="flex h-10 w-10 items-center justify-center rounded-full bg-brand-cyan/10 text-brand-cyan">
                                        2
                                    </div>
                                    <span>Earn</span>
                                </div>
                                <div className="h-0.5 w-8 bg-border" />
                                <div className="flex flex-col items-center gap-2">
                                    <div className="flex h-10 w-10 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-500">
                                        3
                                    </div>
                                    <span>Withdraw</span>
                                </div>
                            </div>

                            <div className="space-y-3">
                                <button
                                    onClick={() => completeStep("hasSeenWelcome")}
                                    className="w-full rounded-xl bg-foreground px-4 py-3.5 text-sm font-medium text-background transition-transform hover:scale-[1.02] active:scale-[0.98]"
                                >
                                    Connect Wallet
                                </button>
                                <div className="flex items-center justify-between gap-3">
                                    <button
                                        onClick={() => window.open("https://nester.com", "_blank")}
                                        className="flex-1 rounded-xl border border-border bg-white px-4 py-3.5 text-sm font-medium text-foreground transition-colors hover:bg-secondary"
                                    >
                                        Learn More
                                    </button>
                                    <button
                                        onClick={() => skip()}
                                        className="flex-1 rounded-xl border border-transparent px-4 py-3.5 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground"
                                    >
                                        Skip
                                    </button>
                                </div>
                            </div>
                        </div>
                    </motion.div>
                </div>
            )}
        </AnimatePresence>
    );
}
