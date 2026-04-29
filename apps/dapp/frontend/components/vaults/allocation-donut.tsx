"use client";

import {
  ResponsiveContainer,
  PieChart,
  Pie,
  Cell,
  Tooltip,
  type TooltipContentProps,
  type TooltipValueType,
} from "recharts";
import type { VaultAllocation } from "@/lib/types/vault";

type TooltipNameType = number | string;

function AllocationTooltip({
  active,
  payload,
}: TooltipContentProps<TooltipValueType, TooltipNameType>) {
  if (!active || !payload?.length) return null;
  const d = payload[0].payload as VaultAllocation;
  return (
    <div className="rounded-xl border border-border bg-white p-2.5 shadow-sm text-xs">
      <p className="font-medium text-foreground">{d.protocol}</p>
      <p className="text-muted-foreground">{d.percentage}% allocation</p>
      <p className="text-muted-foreground">{d.apy.toFixed(1)}% APY</p>
    </div>
  );
}

export function AllocationDonut({
  allocations,
}: {
  allocations: VaultAllocation[];
}) {
  return (
    <div className="rounded-2xl border border-border bg-white p-6">
      <h2 className="font-heading text-lg font-light text-foreground mb-6">
        Allocation Breakdown
      </h2>
      <ResponsiveContainer width="100%" height={200}>
        <PieChart>
          <Pie
            data={allocations}
            dataKey="percentage"
            nameKey="protocol"
            cx="50%"
            cy="50%"
            innerRadius={55}
            outerRadius={85}
            paddingAngle={allocations.length > 1 ? 3 : 0}
            strokeWidth={0}
          >
            {allocations.map((entry, index) => (
              <Cell key={index} fill={entry.color} />
            ))}
          </Pie>
          <Tooltip content={AllocationTooltip} />
        </PieChart>
      </ResponsiveContainer>
      <div className="mt-4 divide-y divide-border">
        {allocations.map((a) => (
          <div
            key={a.protocol}
            className="flex items-center justify-between py-3"
          >
            <div className="flex items-center gap-2.5">
              <span
                className="h-2.5 w-2.5 rounded-full shrink-0"
                style={{ background: a.color }}
              />
              <span className="text-sm text-foreground">{a.protocol}</span>
            </div>
            <div className="flex items-center gap-3">
              <span className="text-sm font-medium text-foreground">
                {a.percentage}%
              </span>
              <span className="text-xs text-muted-foreground w-16 text-right">
                {a.apy.toFixed(1)}% APY
              </span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
