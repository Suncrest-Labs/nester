"use client";

import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
  type TooltipContentProps,
  type TooltipValueType,
} from "recharts";
import { cn } from "@/lib/utils";
import { AlertTriangle } from "lucide-react";
import type { ProjectionData, ProjectionBandPoint } from "@/lib/types/progress";

type TooltipNameType = number | string;

function ProjectionTooltip({
  active,
  payload,
}: TooltipContentProps<TooltipValueType, TooltipNameType>) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload as ProjectionBandPoint & { currency?: string };
  return (
    <div className="rounded-xl border border-black/8 dark:border-white/8 bg-white dark:bg-[#100F0F] px-3.5 py-2.5 shadow-lg text-xs">
      <p className="font-medium text-black dark:text-white mb-1">{d.date}</p>
      <p className="text-black/70 dark:text-white/70">
        Median: {d.median.toLocaleString()} {d.currency ?? ""}
      </p>
      <p className="text-emerald-600">
        Upper: {d.upper_bound.toLocaleString()} {d.currency ?? ""}
      </p>
      <p className="text-red-500">
        Lower: {d.lower_bound.toLocaleString()} {d.currency ?? ""}
      </p>
    </div>
  );
}

export function ProjectionBand({
  projection,
}: {
  projection: ProjectionData;
}) {
  if (!projection.timeline?.length) return null;

  const currency = projection.currency;

  const data = projection.timeline.map((p) => ({ ...p, currency }));

  return (
    <div className="space-y-3" data-testid="projection-band">
      <div className="flex items-center justify-between">
        <h3 className="text-xs font-semibold text-black/60 dark:text-white/60 uppercase tracking-wider">
          Projection
        </h3>
        <div className="flex items-center gap-2">
          <span className={cn(
            "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-bold",
            projection.success_probability >= 70
              ? "bg-emerald-100 text-emerald-700"
              : projection.success_probability >= 40
              ? "bg-amber-100 text-amber-700"
              : "bg-red-100 text-red-700"
          )}>
            {projection.success_probability}% likely
          </span>
        </div>
      </div>

      <div className="h-48 w-full">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
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
              domain={["auto", "auto"]}
              width={48}
              tickFormatter={(val) => val >= 1000 ? `${(val / 1000).toFixed(0)}k` : String(val)}
            />
            <Tooltip content={ProjectionTooltip} />
            <Area
              type="monotone"
              dataKey="upper_bound"
              stroke="transparent"
              fill="#000000"
              fillOpacity={0.06}
            />
            <Area
              type="monotone"
              dataKey="lower_bound"
              stroke="transparent"
              fill="#000000"
              fillOpacity={0.06}
            />
            <Area
              type="monotone"
              dataKey="median"
              stroke="#000000"
              strokeWidth={2}
              fill="transparent"
              dot={false}
              activeDot={{ r: 4 }}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>

      {!projection.on_track && projection.monthly_gap && projection.monthly_gap > 0 && (
        <div className="flex items-start gap-2 rounded-xl border border-amber-200 bg-amber-50/50 dark:border-amber-800/30 dark:bg-amber-900/10 px-3 py-2.5">
          <AlertTriangle className="h-4 w-4 shrink-0 text-amber-600 mt-0.5" aria-hidden="true" />
          <p className="text-xs text-amber-800 dark:text-amber-300 font-medium">
            Add {projection.monthly_gap.toLocaleString()} {currency}/month to get back on track
          </p>
        </div>
      )}
    </div>
  );
}
