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
import type { ProjectionData, ProjectionBandPoint } from "@/lib/types/progress";

type TooltipNameType = number | string;

const TICK = { fontSize: 11, fill: "currentColor", fillOpacity: 0.5 };

function ProjectionTooltip({
  active,
  payload,
}: TooltipContentProps<TooltipValueType, TooltipNameType>) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload as ProjectionBandPoint & { currency?: string };
  return (
    <div className="rounded-lg border bg-card px-3 py-2 text-xs tabular-nums">
      <p className="mb-1 text-foreground">{d.date}</p>
      <p className="text-foreground">
        Likely: {d.median.toLocaleString()} {d.currency ?? ""}
      </p>
      <p className="text-muted-foreground">
        High: {d.upper_bound.toLocaleString()} {d.currency ?? ""}
      </p>
      <p className="text-muted-foreground">
        Low: {d.lower_bound.toLocaleString()} {d.currency ?? ""}
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
        <h3 className="text-sm font-medium text-muted-foreground">Projection</h3>
        <span
          className={cn(
            "text-[13px] tabular-nums",
            projection.success_probability >= 70
              ? "text-foreground"
              : projection.success_probability >= 40
              ? "text-amber-700 dark:text-amber-400"
              : "text-red-600 dark:text-red-400"
          )}
        >
          {projection.success_probability}% likely
        </span>
      </div>

      <div className="h-48 w-full text-foreground">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
            <CartesianGrid
              strokeDasharray="3 3"
              vertical={false}
              stroke="currentColor"
              strokeOpacity={0.08}
            />
            <XAxis dataKey="date" axisLine={false} tickLine={false} tick={TICK} minTickGap={30} />
            <YAxis
              axisLine={false}
              tickLine={false}
              tick={TICK}
              domain={["auto", "auto"]}
              width={48}
              tickFormatter={(val) => val >= 1000 ? `${(val / 1000).toFixed(0)}k` : String(val)}
            />
            <Tooltip content={ProjectionTooltip} cursor={{ stroke: "currentColor", strokeOpacity: 0.2 }} />
            <Area
              type="monotone"
              dataKey="upper_bound"
              stroke="transparent"
              fill="currentColor"
              fillOpacity={0.06}
            />
            <Area
              type="monotone"
              dataKey="lower_bound"
              stroke="transparent"
              fill="currentColor"
              fillOpacity={0.06}
            />
            <Area
              type="monotone"
              dataKey="median"
              stroke="currentColor"
              strokeWidth={1.5}
              fill="transparent"
              dot={false}
              activeDot={{ r: 3, fill: "currentColor", stroke: "currentColor" }}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>

      {!projection.on_track && projection.monthly_gap && projection.monthly_gap > 0 && (
        <p className="text-sm text-amber-700 dark:text-amber-400">
          Add {projection.monthly_gap.toLocaleString()} {currency}/month to get back on track.
        </p>
      )}
    </div>
  );
}
