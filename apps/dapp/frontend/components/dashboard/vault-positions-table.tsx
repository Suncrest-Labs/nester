"use client";

import { motion } from "framer-motion";
import { VaultPosition, RiskTier } from "@/lib/mock-data";
import { cn } from "@/lib/utils";
import Link from 'next/link';
import { ExternalLink, Plus, Minus } from "lucide-react";
import { useSettings } from "@/context/settings-context";

interface VaultPositionsTableProps {
    positions: VaultPosition[];
}

const RISK_COLORS: Record<RiskTier, string> = {
    Safe: "bg-emerald-50 text-emerald-700 border-emerald-100",
    Balanced: "bg-amber-50 text-amber-700 border-amber-100",
    Aggressive: "bg-rose-50 text-rose-700 border-rose-100",
};

export function VaultPositionsTable({ positions }: VaultPositionsTableProps) {
    const { formatValue } = useSettings();

    return (
        <div className="rounded-2xl border border-border bg-white overflow-hidden shadow-sm hover:border-black/15 transition-all">
            <div className="px-6 py-5 border-b border-border bg-secondary/10 flex items-center justify-between">
                <h2 className="font-heading text-lg font-light text-foreground">
                    Your Vaults
                </h2>
                <Link href="/dashboard/vaults">
                    <button className="text-xs font-semibold text-primary hover:underline transition-all">
                        View All
                    </button>
                </Link>
            </div>
            
            {positions.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-16 text-center">
                    <p className="text-sm font-medium text-foreground/80">
                        No vaults yet
                    </p>
                    <p className="mt-1 max-w-xs text-xs text-muted-foreground leading-relaxed px-4">
                        Create your first vault to start earning optimized yield.
                    </p>
                    <Link href="/dashboard/vaults" className="mt-6">
                        <button className="rounded-full bg-primary px-6 py-2.5 text-xs font-semibold text-white transition-all shadow-md shadow-primary/20 hover:scale-[1.02] active:scale-[0.98]">
                            Browse Vaults
                        </button>
                    </Link>
                </div>
            ) : (
                <div className="overflow-x-auto">
                    <table className="w-full text-left border-collapse min-w-[600px]">
                        <thead>
                            <tr className="border-b border-border bg-secondary/5">
                                <th className="px-6 py-3.5 text-[10px] font-bold text-muted-foreground uppercase tracking-wider">Vault</th>
                                <th className="px-6 py-3.5 text-[10px] font-bold text-muted-foreground uppercase tracking-wider">Balance</th>
                                <th className="px-6 py-3.5 text-[10px] font-bold text-muted-foreground uppercase tracking-wider text-right">APY</th>
                                <th className="px-6 py-3.5 text-[10px] font-bold text-muted-foreground uppercase tracking-wider">Risk</th>
                                <th className="px-6 py-3.5 text-[10px] font-bold text-muted-foreground uppercase tracking-wider text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-border">
                            {positions.map((pos) => (
                                <tr key={pos.id} className="group hover:bg-secondary/20 transition-colors">
                                    <td className="px-6 py-5">
                                        <Link 
                                            href={`/dashboard/vaults/${pos.id}`}
                                            className="flex flex-col hover:opacity-70 transition-opacity"
                                        >
                                            <span className="text-sm font-semibold text-foreground flex items-center gap-1.5">
                                                {pos.vaultName}
                                                <ExternalLink className="h-3 w-3 opacity-0 group-hover:opacity-30" />
                                            </span>
                                            <span className="text-[10px] text-muted-foreground">{pos.asset}</span>
                                        </Link>
                                    </td>
                                    <td className="px-6 py-5">
                                        <div className="flex flex-col">
                                            <span className="text-sm font-medium text-foreground">{formatValue(pos.balance)}</span>
                                            <span className="text-[10px] text-emerald-600 font-mono">+{formatValue(pos.yieldEarned)} earned</span>
                                        </div>
                                    </td>
                                    <td className="px-6 py-5 text-right font-mono text-sm text-emerald-600 font-bold">
                                        {pos.apy}
                                    </td>
                                    <td className="px-6 py-5">
                                        <span className={cn(
                                            "inline-flex items-center px-2 py-0.5 rounded-full text-[9px] font-bold border",
                                            RISK_COLORS[pos.riskTier]
                                        )}>
                                            {pos.riskTier}
                                        </span>
                                    </td>
                                    <td className="px-6 py-5 text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <button className="h-8 w-8 rounded-lg border border-border bg-white flex items-center justify-center hover:bg-secondary transition-all" title="Deposit More">
                                                <Plus className="h-3.5 w-3.5" />
                                            </button>
                                            <button className="h-8 w-8 rounded-lg border border-border bg-white flex items-center justify-center hover:bg-secondary transition-all" title="Withdraw">
                                                <Minus className="h-3.5 w-3.5" />
                                            </button>
                                        </div>
                                    </td>
                                </tr>
                            ))}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}
