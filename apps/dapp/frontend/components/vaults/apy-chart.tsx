"use client";

import { useState } from "react";
import { cn } from "@/lib/utils";
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  type TooltipContentProps,
  type TooltipValueType,
} from "recharts";
import type { ApyDataPoint } from "@/lib/types/vault";

type ApyTab = "7d" | "30d" | "90d";
type TooltipNameType = number | string;

const TAB_DAYS: Record<ApyTab, number> = { "7d": 7, "30d": 30, "90d": 90 };

function ApyTooltip({
  active,
  payload,
  label,
}: TooltipContentProps<TooltipValueType, TooltipNameType>) {
  if (!active || !payload?.length) return null;
  return (
    <div className="rounded-xl border border-border bg-white p-2.5 shadow-sm text-xs">
      <p className="text-muted-foreground mb-0.5">{label as string}</p>
      <p className="font-medium text-foreground">
        {(payload[0].value as number)?.toFixed(2)}% APY
      </p>
    </div>
  );
}

export function APYChart({ data }: { data: ApyDataPoint[] }) {
  const [tab, setTab] = useState<ApyTab>("30d");
  const slice = data.slice(-TAB_DAYS[tab]);

  return (
    <div className="rounded-2xl border border-border bg-white p-6">
      <div className="flex items-center justify-between mb-6">
        <h2 className="font-heading text-lg font-light text-foreground">
          APY History
        </h2>
        <div className="flex gap-1">
          {(["7d", "30d", "90d"] as ApyTab[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={cn(
                "px-3 py-1 rounded-full text-xs font-medium transition-colors",
                tab === t
                  ? "bg-foreground text-background"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t}
            </button>
          ))}
        </div>
      </div>
      <ResponsiveContainer width="100%" height={220}>
        <LineChart data={slice} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <CartesianGrid
            strokeDasharray="3 3"
            stroke="#e5e7eb"
            vertical={false}
          />
          <XAxis dataKey="date" tick={false} axisLine={false} tickLine={false} />
          <YAxis
            domain={["auto", "auto"]}
            tick={{ fontSize: 11, fill: "#9ca3af" }}
            axisLine={false}
            tickLine={false}
            tickFormatter={(v) => `${v}%`}
            width={40}
          />
          <Tooltip
            content={ApyTooltip}
            cursor={{ stroke: "#e5e7eb", strokeWidth: 1 }}
          />
          <Line
            type="monotone"
            dataKey="apy"
            stroke="#2EBAC6"
            strokeWidth={2}
            dot={false}
            activeDot={{ r: 4, fill: "#2EBAC6", strokeWidth: 0 }}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
