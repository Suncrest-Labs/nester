"use client";

import { useState, useMemo } from "react";
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { ArrowUpRight, ArrowDownRight } from "lucide-react";
import { cn } from "@/lib/utils";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface ProtocolApyPoint {
  date: string;
  [protocol: string]: number | string;
}

export interface ProtocolSnapshot {
  protocol: string;
  currentApy: number;
  avg30d: number;
  trend7d: number;
  allocationPct?: number;
}

interface YieldComparisonChartProps {
  history: ProtocolApyPoint[];
  snapshots: ProtocolSnapshot[];
  loading?: boolean;
}

// ── Period options ─────────────────────────────────────────────────────────────

const PERIODS = ["30d", "90d", "1y"] as const;
type Period = (typeof PERIODS)[number];

const PROTOCOL_COLORS: Record<string, string> = {
  Blend: "#6366f1",
  Aave: "#8b5cf6",
  Compound: "#ec4899",
  Nester: "#000000",
};

function pickColor(protocol: string, index: number): string {
  if (PROTOCOL_COLORS[protocol]) return PROTOCOL_COLORS[protocol];
  const fallbacks = ["#06b6d4", "#f59e0b", "#10b981", "#ef4444", "#84cc16"];
  return fallbacks[index % fallbacks.length];
}

// ── Skeleton ──────────────────────────────────────────────────────────────────

function ChartSkeleton() {
  return (
    <div className="animate-pulse space-y-4">
      <div className="h-[260px] rounded-2xl bg-black/[0.04]" />
      <div className="space-y-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <div key={i} className="h-10 rounded-xl bg-black/[0.04]" />
        ))}
      </div>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function YieldComparisonChart({
  history,
  snapshots,
  loading = false,
}: YieldComparisonChartProps) {
  const [period, setPeriod] = useState<Period>("30d");

  const periodDays: Record<Period, number> = { "30d": 30, "90d": 90, "1y": 365 };

  const filteredHistory = useMemo(() => {
    const cutoff = new Date();
    cutoff.setDate(cutoff.getDate() - periodDays[period]);
    return history.filter((d) => new Date(d.date) >= cutoff);
  }, [history, period, periodDays]);

  const protocols = useMemo(() => {
    if (history.length === 0) return [];
    const keys = Object.keys(history[0]).filter((k) => k !== "date");
    // Put Nester last so its line renders on top
    return [...keys.filter((k) => k !== "Nester"), ...keys.filter((k) => k === "Nester")];
  }, [history]);

  if (loading) return <ChartSkeleton />;

  const isEmpty = filteredHistory.length === 0 || protocols.length === 0;

  return (
    <div>
      {/* Period toggle */}
      <div className="mb-5 flex items-center gap-1">
        {PERIODS.map((p) => (
          <button
            key={p}
            onClick={() => setPeriod(p)}
            className={cn(
              "rounded-md px-3 py-1 text-[12px] font-medium transition-colors",
              period === p
                ? "bg-black/[0.06] text-black"
                : "text-black/35 hover:text-black/60"
            )}
          >
            {p}
          </button>
        ))}
      </div>

      {/* Line chart */}
      {isEmpty ? (
        <div className="flex h-[260px] items-center justify-center rounded-2xl border border-black/8 bg-white">
          <p className="text-sm text-black/35">No APY history available for this period.</p>
        </div>
      ) : (
        <div className="rounded-2xl border border-black/8 bg-white p-4">
          <ResponsiveContainer
            width="100%"
            height={260}
            aria-label="APY history comparison chart showing protocol performance over time"
          >
            <LineChart
              data={filteredHistory}
              margin={{ top: 8, right: 12, left: 0, bottom: 4 }}
            >
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(0,0,0,0.04)" />
              <XAxis
                dataKey="date"
                tick={{ fontSize: 11, fill: "rgba(0,0,0,0.35)" }}
                tickFormatter={(v: string) => {
                  const d = new Date(v);
                  return `${d.getMonth() + 1}/${d.getDate()}`;
                }}
              />
              <YAxis
                tick={{ fontSize: 11, fill: "rgba(0,0,0,0.35)" }}
                tickFormatter={(v: number) => `${v.toFixed(1)}%`}
                width={42}
              />
              <Tooltip
                formatter={(value, name) => [
                  `${Number(value ?? 0).toFixed(2)}%`,
                  String(name ?? ""),
                ]}
                labelFormatter={(label) =>
                  new Date(String(label ?? "")).toLocaleDateString()
                }
                contentStyle={{
                  borderRadius: "12px",
                  border: "1px solid rgba(0,0,0,0.08)",
                  fontSize: "12px",
                }}
              />
              <Legend
                iconType="plainline"
                wrapperStyle={{ fontSize: "12px", paddingTop: "12px" }}
              />
              {protocols.map((protocol, i) => (
                <Line
                  key={protocol}
                  type="monotone"
                  dataKey={protocol}
                  stroke={pickColor(protocol, i)}
                  strokeWidth={protocol === "Nester" ? 2.5 : 1.5}
                  dot={false}
                  activeDot={{ r: 3 }}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        </div>
      )}

      {/* Snapshot table */}
      {snapshots.length > 0 && (
        <div className="mt-5 overflow-x-auto">
          <table className="w-full text-left">
            <thead>
              <tr className="border-b border-black/[0.05] text-[11px] text-black/35">
                <th className="pb-3 pr-4 font-medium">Protocol</th>
                <th className="pb-3 pr-4 font-medium text-right">Current APY</th>
                <th className="pb-3 pr-4 font-medium text-right">30d Avg</th>
                <th className="pb-3 pr-4 font-medium text-right">7d Trend</th>
                <th className="pb-3 font-medium text-right">Your Allocation</th>
              </tr>
            </thead>
            <tbody>
              {snapshots.map((row, i) => (
                <tr key={row.protocol} className="border-b border-black/[0.04] last:border-0">
                  <td className="py-3 pr-4">
                    <div className="flex items-center gap-2">
                      <span
                        className="h-2 w-2 rounded-full shrink-0"
                        style={{ background: pickColor(row.protocol, i) }}
                      />
                      <span
                        className={cn(
                          "text-sm",
                          row.protocol === "Nester" ? "font-medium text-black" : "text-black"
                        )}
                      >
                        {row.protocol}
                      </span>
                    </div>
                  </td>
                  <td className="py-3 pr-4 text-right font-mono text-sm text-black">
                    {row.currentApy.toFixed(1)}%
                  </td>
                  <td className="py-3 pr-4 text-right font-mono text-sm text-black/55">
                    {row.avg30d.toFixed(1)}%
                  </td>
                  <td className="py-3 pr-4 text-right">
                    <span
                      className={cn(
                        "inline-flex items-center gap-0.5 font-mono text-sm",
                        row.trend7d >= 0 ? "text-emerald-600" : "text-red-500"
                      )}
                    >
                      {row.trend7d >= 0 ? (
                        <ArrowUpRight className="h-3 w-3" />
                      ) : (
                        <ArrowDownRight className="h-3 w-3" />
                      )}
                      {Math.abs(row.trend7d).toFixed(1)}%
                    </span>
                  </td>
                  <td className="py-3 text-right text-sm text-black/55">
                    {row.allocationPct != null ? `${row.allocationPct.toFixed(0)}%` : "—"}
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
