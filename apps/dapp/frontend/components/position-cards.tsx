"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { type PortfolioPosition } from "@/components/portfolio-provider";
import { WithdrawModal } from "@/components/vault-action-modals";

/**
 * Renders a list of position cards with withdraw buttons.
 * Filter positions externally before passing them in.
 */
export function PositionCards({
    positions,
    emptyLabel = "No positions yet",
    emptyHint = "Deposit to see your positions here.",
}: {
    positions: PortfolioPosition[];
    emptyLabel?: string;
    emptyHint?: string;
}) {
    const [withdrawPos, setWithdrawPos] = useState<PortfolioPosition | null>(null);

    if (positions.length === 0) return null;

    return (
        <>
            <div className="space-y-2">
                {positions.map((pos, i) => (
                    <motion.div
                        key={pos.id}
                        initial={{ opacity: 0, y: 6 }}
                        animate={{ opacity: 1, y: 0 }}
                        transition={{ delay: i * 0.04 }}
                        className="flex items-center justify-between gap-4 rounded-2xl border border-black/8 bg-white px-5 py-4"
                    >
                        <div className="min-w-0">
                            <div className="flex items-center gap-2">
                                <p className="text-sm text-black truncate">{pos.vaultName}</p>
                                <span className="text-[11px] text-black/35">{pos.asset}</span>
                                {pos.isMatured ? (
                                    <span className="text-[10px] bg-black text-white rounded-full px-2 py-0.5">Matured</span>
                                ) : (
                                    <span className="text-[10px] bg-black/[0.04] text-black/50 rounded-full px-2 py-0.5">{pos.daysRemaining}d left</span>
                                )}
                            </div>
                            <div className="mt-1 flex items-center gap-3 text-xs text-black/35">
                                <span>APY {(pos.apy * 100).toFixed(1)}%</span>
                                <span>Yield +{pos.yieldEarned.toFixed(4)}</span>
                            </div>
                        </div>
                        <div className="flex items-center gap-3 shrink-0">
                            <div className="text-right">
                                <p className="text-base text-black">
                                    {pos.currentValue.toFixed(2)}
                                </p>
                                <p className="text-[11px] text-black/30 mt-0.5">
                                    Principal: {pos.principal.toFixed(2)}
                                </p>
                            </div>
                            <button
                                onClick={() => setWithdrawPos(pos)}
                                className="rounded-lg bg-black px-3 py-1.5 text-[11px] text-white transition-opacity hover:opacity-75"
                            >
                                Withdraw
                            </button>
                        </div>
                    </motion.div>
                ))}
            </div>

            <WithdrawModal
                open={!!withdrawPos}
                onClose={() => setWithdrawPos(null)}
                position={withdrawPos}
            />
        </>
    );
}
