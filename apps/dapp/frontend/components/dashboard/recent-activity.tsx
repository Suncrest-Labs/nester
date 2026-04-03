"use client";

import { motion } from "framer-motion";
import { Transaction } from "@/lib/mock-data";
import { cn, truncateAddress } from "@/lib/utils";
import { 
    ArrowUpRight, 
    ArrowDownLeft, 
    RefreshCcw, 
    LineChart,
    ExternalLink 
} from "lucide-react";
import Link from 'next/link';
import { useSettings } from "@/context/settings-context";

interface RecentActivityProps {
    transactions: Transaction[];
}

const TYPE_ICONS = {
    "Deposit": ArrowDownLeft,
    "Withdrawal": ArrowUpRight,
    "Yield Accrual": LineChart,
    "Rebalance": RefreshCcw,
};

export function RecentActivity({ transactions }: RecentActivityProps) {
    const { formatValue } = useSettings();
    const latestItems = transactions.slice(0, 5);

    return (
        <div className="mt-8 rounded-2xl border border-border bg-white overflow-hidden shadow-sm hover:border-black/15 transition-all">
            <div className="px-6 py-5 border-b border-border bg-secondary/10 flex items-center justify-between">
                <h2 className="font-heading text-lg font-light text-foreground">
                    Recent Activity
                </h2>
                <Link href="/dashboard/portfolio">
                    <button className="text-xs font-semibold text-primary hover:underline transition-all">
                        View All History
                    </button>
                </Link>
            </div>

            {latestItems.length === 0 ? (
                <div className="py-12 flex flex-col items-center justify-center text-center">
                    <p className="text-sm text-muted-foreground">No recent transactions</p>
                </div>
            ) : (
                <div className="overflow-x-auto">
                    <table className="w-full text-left border-collapse min-w-[600px]">
                        <tbody className="divide-y divide-border">
                            {latestItems.map((tx) => {
                                const Icon = TYPE_ICONS[tx.type];
                                return (
                                    <tr key={tx.id} className="group hover:bg-secondary/20 transition-colors">
                                        <td className="px-6 py-4">
                                            <div className="flex items-center gap-3">
                                                <div className="h-8 w-8 rounded-lg bg-secondary flex items-center justify-center text-foreground/50">
                                                    <Icon className="h-4 w-4" />
                                                </div>
                                                <div className="flex flex-col">
                                                    <span className="text-sm font-medium text-foreground">{tx.type}</span>
                                                    <span className="text-[10px] text-muted-foreground">{tx.vaultName}</span>
                                                </div>
                                            </div>
                                        </td>
                                        <td className="px-6 py-4">
                                            <div className="text-sm font-mono text-muted-foreground">
                                                {new Date(tx.timestamp).toLocaleDateString([], { month: "short", day: "numeric" })}
                                            </div>
                                        </td>
                                        <td className="px-6 py-4">
                                            <div className={cn(
                                                "text-sm font-semibold",
                                                tx.amount.startsWith("+") ? "text-emerald-600" : 
                                                tx.amount.startsWith("-") ? "text-rose-600" : "text-foreground"
                                            )}>
                                                {tx.amount.startsWith("+") || tx.amount.startsWith("-") 
                                                    ? `${tx.amount[0]}${formatValue(Math.abs(parseFloat(tx.amount)))}` 
                                                    : formatValue(parseFloat(tx.amount))}
                                                <span className="text-[10px] opacity-70 ml-0.5">{tx.asset}</span>
                                            </div>
                                        </td>
                                        <td className="px-6 py-4 text-right">
                                            <a 
                                                href={`https://stellar.expert/explorer/public/tx/${tx.txHash}`} 
                                                target="_blank" 
                                                rel="noopener noreferrer"
                                                className="inline-flex items-center gap-1.5 text-xs text-muted-foreground hover:text-primary transition-all group/link font-mono"
                                            >
                                                {truncateAddress(tx.txHash, 4)}
                                                <ExternalLink className="h-3 w-3 opacity-50 group-hover/link:opacity-100" />
                                            </a>
                                        </td>
                                    </tr>
                                );
                            })}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    );
}
