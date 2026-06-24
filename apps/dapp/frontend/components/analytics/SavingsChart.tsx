"use client";

import {
    LineChart,
    Line,
    XAxis,
    YAxis,
    CartesianGrid,
    Tooltip,
    ResponsiveContainer,
    type TooltipContentProps,
    type TooltipValueType,
} from "recharts";

/**
 * A single point on the APY history chart.
 *
 * Produced by `useSavingsChartData`, which maps the raw `/apy-history`
 * response into display-ready values:
 *   - `date`  short label for the axis/tooltip, e.g. "Jun 15"
 *   - `apy`   formatted percentage string, e.g. "8.23"
 *   - `value` numeric percentage used to plot the line, e.g. 8.23
 */
export interface SavingsChartPoint {
    date: string;
    apy: string;
    value: number;
}

interface SavingsChartProps {
    data: SavingsChartPoint[];
    isLoading?: boolean;
}

function ApyTooltip({
    active,
    payload,
}: TooltipContentProps<TooltipValueType, number | string>) {
    if (!active || !payload?.length) return null;
    const point = payload[0].payload as SavingsChartPoint;
    return (
        <div className="rounded-xl border border-black/8 bg-white px-3.5 py-2.5 shadow-lg shadow-black/10">
            <p className="text-xs font-medium text-black">
                APY: {point.apy}% <span className="text-black/40">on {point.date}</span>
            </p>
        </div>
    );
}

export default function SavingsChart({ data, isLoading = false }: SavingsChartProps) {
    if (isLoading) {
        return (
            <div
                className="h-[320px] w-full animate-pulse rounded-2xl bg-black/[0.03]"
                aria-busy="true"
                aria-label="Loading chart data"
            />
        );
    }

    if (!data || data.length === 0) {
        return (
            <div className="h-[320px] flex flex-col items-center justify-center text-center text-black/40 bg-black/[0.02] rounded-2xl border border-dashed border-black/10 px-6">
                <svg
                    width="40"
                    height="40"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    className="mb-3 text-black/25"
                    aria-hidden="true"
                >
                    <path d="M3 3v18h18" />
                    <path d="m19 9-5 5-4-4-3 3" />
                </svg>
                <p className="text-sm max-w-xs">
                    Chart data will appear after your vault has been active for a few days.
                </p>
            </div>
        );
    }

    return (
        <div
            className="w-full h-[320px]"
            role="img"
            aria-label="Vault APY performance over time"
        >
            <ResponsiveContainer width="100%" height="100%">
                <LineChart
                    data={data}
                    margin={{ top: 10, right: 10, left: -20, bottom: 0 }}
                >
                    <CartesianGrid
                        strokeDasharray="3 3"
                        vertical={false}
                        stroke="#000000"
                        strokeOpacity={0.05}
                    />
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
                        tickFormatter={(val) => `${val}%`}
                        domain={["auto", "auto"]}
                        width={48}
                    />
                    <Tooltip content={ApyTooltip} />
                    <Line
                        type="monotone"
                        dataKey="value"
                        stroke="#000000"
                        strokeWidth={2}
                        dot={false}
                        activeDot={{ r: 4 }}
                        animationDuration={1200}
                        name="APY"
                    />
                </LineChart>
            </ResponsiveContainer>
        </div>
    );
}
