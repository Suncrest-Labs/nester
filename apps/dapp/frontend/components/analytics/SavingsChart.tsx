"use client";

import {
    AreaChart,
    Area,
    XAxis,
    YAxis,
    CartesianGrid,
    Tooltip,
    ResponsiveContainer,
} from "recharts";

export interface ChartDataPoint {
    date: string;
    actualBalance?: number;
    projectedBalance?: number;
}

interface SavingsChartProps {
    data: ChartDataPoint[];
}

export default function SavingsChart({ data }: SavingsChartProps) {
    if (!data || data.length === 0) {
        return (
            <div className="h-[300px] flex flex-col items-center justify-center text-black/40 bg-black/[0.02] rounded-2xl border border-dashed border-black/10">
                <p className="text-sm">No savings history yet</p>
                <p className="text-xs mt-1">Deposits will appear here over time</p>
            </div>
        );
    }

    const fmtUsd = (val: number) => 
        new Intl.NumberFormat("en-US", {
            style: "currency",
            currency: "USD",
            minimumFractionDigits: 0,
            maximumFractionDigits: 0,
        }).format(val);

    return (
        <div className="w-full h-[320px]" role="img" aria-label="Savings balance growth chart showing actual and projected balance over time">
            <ResponsiveContainer width="100%" height="100%">
                <AreaChart
                    data={data}
                    margin={{ top: 10, right: 10, left: -20, bottom: 0 }}
                >
                    <defs>
                        <linearGradient id="colorBalance" x1="0" y1="0" x2="0" y2="1">
                            <stop offset="5%" stopColor="#000000" stopOpacity={0.08} />
                            <stop offset="95%" stopColor="#000000" stopOpacity={0} />
                        </linearGradient>
                    </defs>
                    <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#000000" strokeOpacity={0.05} />
                    <XAxis 
                        dataKey="date" 
                        axisLine={false}
                        tickLine={false}
                        tick={{ fontSize: 10, fill: "rgba(0,0,0,0.4)" }}
                        minTickGap={30}
                    />
                    <YAxis 
                        axisLine={false}
                        tickLine={false}
                        tick={{ fontSize: 10, fill: "rgba(0,0,0,0.4)" }}
                        tickFormatter={(val) => `$${val}`}
                    />
                    <Tooltip 
                        contentStyle={{ 
                            borderRadius: "12px", 
                            border: "none", 
                            boxShadow: "0 10px 15px -3px rgba(0,0,0,0.1)",
                            fontSize: "12px"
                        }}
                        formatter={(value) => [
                            typeof value === "number" ? fmtUsd(value) : "",
                            "",
                        ]}
                        labelStyle={{ color: "rgba(0,0,0,0.4)", marginBottom: "4px" }}
                    />
                    <Area
                        type="monotone"
                        dataKey="actualBalance"
                        stroke="#000000"
                        strokeWidth={2}
                        fillOpacity={1}
                        fill="url(#colorBalance)"
                        animationDuration={1200}
                        name="Actual Balance"
                    />
                    <Area
                        type="monotone"
                        dataKey="projectedBalance"
                        stroke="#000000"
                        strokeWidth={1.5}
                        strokeDasharray="5 5"
                        fill="transparent"
                        animationDuration={1200}
                        name="Projected Balance"
                    />
                </AreaChart>
            </ResponsiveContainer>
        </div>
    );
}
