"use client";

import { useState, useMemo } from "react";
import { 
    ResponsiveContainer, 
    PieChart, 
    Pie, 
    Cell, 
    AreaChart, 
    Area, 
    XAxis, 
    YAxis, 
    CartesianGrid, 
    Tooltip,
    Legend
} from "recharts";
import { 
    VaultPosition, 
    mockPerformanceHistory 
} from "@/lib/mock-data";
import { subDays, parseISO, isAfter } from "date-fns";
import { cn } from "@/lib/utils";
import { useSettings } from "@/context/settings-context";

interface PortfolioChartsProps {
    positions: VaultPosition[];
}

const CHART_COLORS = [
    "#000000",
    "#10b981",
    "#3b82f6",
    "#fbbf24",
    "#f43f5e",
];

export function PortfolioCharts({ positions }: PortfolioChartsProps) {
    const { formatValue, exchangeRate, currency } = useSettings();
    const [timeframe, setTimeframe] = useState<"7D" | "1M" | "3M" | "ALL">("1M");

    // Process Pie Data
    const pieData = useMemo(() => {
        return positions.map((p, i) => ({
            name: p.vaultName,
            value: p.balance * exchangeRate,
            color: CHART_COLORS[i % CHART_COLORS.length]
        }));
    }, [positions, exchangeRate]);

    // Filter Performance Data based on timeframe
    const filteredHistory = useMemo(() => {
        const history = mockPerformanceHistory.map(d => ({
            ...d,
            balance: d.balance * exchangeRate,
            yield: d.yield * exchangeRate,
            benchmark: d.benchmark * exchangeRate
        }));

        if (timeframe === "ALL") return history;
        
        const days = timeframe === "7D" ? 7 : timeframe === "1M" ? 30 : 90;
        const startDate = subDays(new Date(), days);
        
        return history.filter(d => isAfter(parseISO(d.date), startDate));
    }, [timeframe, exchangeRate]);

    const totalBalanceValue = useMemo(() => {
        return positions.reduce((acc, p) => acc + p.balance, 0) * exchangeRate;
    }, [positions, exchangeRate]);

    return (
        <div className="grid gap-6 lg:grid-cols-3">
            {/* Allocation Donut */}
            <div className="rounded-2xl border border-border bg-white p-6 shadow-sm flex flex-col items-center justify-between min-h-[400px]">
                <div className="w-full flex items-center justify-between mb-2">
                   <h3 className="font-heading text-lg font-light text-foreground text-left">Allocation</h3>
                   <span className="text-[10px] text-muted-foreground uppercase tracking-widest font-medium">By Vault</span>
                </div>
                
                <div className="h-64 w-full relative">
                    <ResponsiveContainer width="100%" height="100%">
                        <PieChart>
                            <Pie
                                data={pieData}
                                cx="50%"
                                cy="50%"
                                innerRadius={70}
                                outerRadius={85}
                                paddingAngle={5}
                                dataKey="value"
                            >
                                {pieData.map((entry, index) => (
                                    <Cell key={`cell-${index}`} fill={entry.color} />
                                ))}
                            </Pie>
                            <Tooltip 
                                contentStyle={{ borderRadius: "12px", border: "none", boxShadow: "0 4px 12px rgba(0,0,0,0.08)", fontSize: "11px" }}
                                formatter={(value) => formatValue(Number(value) / exchangeRate)}
                            />
                        </PieChart>
                    </ResponsiveContainer>
                    {/* Center Text */}
                    <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                        <span className="text-[10px] uppercase tracking-widest text-muted-foreground font-bold">Total</span>
                        <span className="text-xl font-heading font-light">
                            {currency === "USD" ? `$${(totalBalanceValue / 1000).toFixed(1)}k` : formatValue(totalBalanceValue / exchangeRate)}
                        </span>
                    </div>
                </div>

                <div className="w-full flex flex-wrap justify-center gap-x-4 gap-y-2 mt-4">
                    {pieData.map((d) => (
                        <div key={d.name} className="flex items-center gap-1.5">
                            <div className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: d.color }} />
                            <span className="text-[10px] font-medium text-foreground">{d.name}</span>
                        </div>
                    ))}
                </div>
            </div>

            {/* Performance Chart */}
            <div className="lg:col-span-2 rounded-2xl border border-border bg-white p-6 shadow-sm flex flex-col min-h-[400px]">
                <div className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4 mb-8">
                    <div className="flex flex-col">
                        <h3 className="font-heading text-lg font-light text-foreground">Yield Performance</h3>
                        <p className="text-[10px] text-muted-foreground uppercase tracking-widest font-medium">Portfolio Growth vs Holding</p>
                    </div>
                    
                    <div className="inline-flex p-1 rounded-xl bg-secondary/30 border border-border/50">
                        {(["7D", "1M", "3M", "ALL"] as const).map((t) => (
                            <button
                                key={t}
                                onClick={() => setTimeframe(t)}
                                className={cn(
                                    "px-3 py-1 rounded-lg text-[10px] font-medium uppercase tracking-wider transition-all",
                                    timeframe === t 
                                        ? "bg-white text-primary shadow-sm border border-border/50" 
                                        : "text-muted-foreground hover:text-foreground"
                                )}
                            >
                                {t}
                            </button>
                        ))}
                    </div>
                </div>

                <div className="flex-1 w-full min-h-[250px]">
                    <ResponsiveContainer width="100%" height="100%">
                        <AreaChart data={filteredHistory} margin={{ top: 0, right: 0, left: -20, bottom: 0 }}>
                            <defs>
                                <linearGradient id="colorYield" x1="0" y1="0" x2="0" y2="1">
                                    <stop offset="5%" stopColor="#10b981" stopOpacity={0.1}/>
                                    <stop offset="95%" stopColor="#10b981" stopOpacity={0}/>
                                </linearGradient>
                            </defs>
                            <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#f1f1f1" />
                            <XAxis 
                                dataKey="date" 
                                axisLine={false} 
                                tickLine={false} 
                                tick={{ fontSize: 9, fill: '#94a3b8' }}
                                tickFormatter={(val) => new Date(val).toLocaleDateString([], { month: 'short', day: 'numeric' })}
                                minTickGap={30}
                            />
                            <YAxis 
                                hide={false}
                                axisLine={false} 
                                tickLine={false} 
                                tick={{ fontSize: 9, fill: '#94a3b8' }}
                                tickFormatter={(val) => currency === "USD" ? `$${(val / 1000).toFixed(0)}k` : `${(val / 1000).toFixed(0)}k`}
                            />
                            <Tooltip 
                                labelStyle={{ fontSize: '11px', fontWeight: 'bold', marginBottom: '4px' }}
                                itemStyle={{ fontSize: '11px' }}
                                contentStyle={{ borderRadius: '12px', border: 'none', boxShadow: '0 4px 12px rgba(0,0,0,0.08)' }}
                                formatter={(value) => formatValue(Number(value) / exchangeRate)}
                            />
                            <Legend 
                                verticalAlign="top" 
                                align="right" 
                                iconType="circle" 
                                iconSize={6}
                                wrapperStyle={{ fontSize: '10px', paddingTop: '0', top: -35 }}
                            />
                            <Area 
                                name="Portfolio"
                                type="monotone" 
                                dataKey="balance" 
                                stroke="#10b981" 
                                strokeWidth={2}
                                fillOpacity={1} 
                                fill="url(#colorYield)" 
                            />
                            <Area 
                                name="Benchmark (Idle)"
                                type="monotone" 
                                dataKey="benchmark" 
                                stroke="#94a3b8" 
                                strokeWidth={1}
                                strokeDasharray="5 5"
                                fillOpacity={0} 
                            />
                        </AreaChart>
                    </ResponsiveContainer>
                </div>
            </div>
        </div>
    );
}
