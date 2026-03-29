"use client";

import { motion } from "framer-motion";
import { 
    TrendingUp, 
    Vault, 
    ArrowUpRight, 
    ArrowDownToLine, 
    Sparkles 
} from "lucide-react";
import { PortfolioStats } from "@/lib/mock-data";
import { useSettings } from "@/context/settings-context";

interface DashboardStatsProps {
    stats: PortfolioStats;
    loading?: boolean;
}

export function DashboardStats({ stats, loading }: DashboardStatsProps) {
    const { formatValue } = useSettings();

    const items = [
        {
            label: "Total Balance",
            value: formatValue(stats.totalBalance),
            change: null,
            icon: Vault,
        },
        {
            label: "Total Yield Earned",
            value: formatValue(stats.totalYieldEarned),
            change: "+12.5%", // Mock change
            icon: TrendingUp,
        },
        {
            label: "Active Vaults",
            value: stats.activeVaults.toString(),
            change: null,
            icon: ArrowDownToLine,
        },
        {
            label: "Prometheus Insights",
            value: `${stats.prometheusInsights} Opportunities`,
            change: null,
            icon: Sparkles,
            highlight: true
        },
    ];

    return (
        <div className="mb-10 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            {items.map((stat, i) => (
                <motion.div
                    key={stat.label}
                    initial={{ opacity: 0, y: 20 }}
                    animate={{ opacity: 1, y: 0 }}
                    transition={{
                        duration: 0.5,
                        delay: 0.1 + i * 0.08,
                    }}
                    className="group rounded-2xl border border-border bg-white p-5 transition-all hover:border-black/15 hover:shadow-sm"
                >
                    {loading ? (
                        <div className="animate-pulse">
                            <div className="mb-4 h-9 w-9 rounded-xl bg-secondary" />
                            <div className="h-8 w-24 rounded bg-secondary mb-2" />
                            <div className="h-4 w-16 rounded bg-secondary" />
                        </div>
                    ) : (
                        <>
                            <div className="mb-4 flex items-center justify-between">
                                <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-secondary">
                                    <stat.icon className={`h-4 w-4 ${stat.highlight ? 'text-emerald-500' : 'text-foreground/50'}`} />
                                </div>
                                {stat.change && (
                                    <span className="flex items-center gap-0.5 text-xs font-medium text-emerald-600">
                                        <ArrowUpRight className="h-3 w-3" />
                                        {stat.change}
                                    </span>
                                )}
                            </div>
                            <p className="text-2xl font-heading font-light text-foreground">
                                {stat.value}
                            </p>
                            <p className="mt-1 text-xs text-muted-foreground uppercase tracking-wider font-medium text-[10px]">
                                {stat.label}
                            </p>
                        </>
                    )}
                </motion.div>
            ))}
        </div>
    );
}
